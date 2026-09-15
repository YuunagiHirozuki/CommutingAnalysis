// Package config 加载系统配置(时间权重模型参数必须可配置,不写死在算法内)。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Config 顶层配置,与 backend/config/config.json 对应。
type Config struct {
	Server struct {
		Port int `json:"port"`
	} `json:"server"`

	// Network 外部服务访问配置(可选代理,用于地址搜索等出网请求)。
	Network struct {
		ProxyURL string `json:"proxyURL"`
	} `json:"network"`

	Data struct {
		Root     string `json:"root"`     // 城市数据根目录
		Fallback string `json:"fallback"` // 所有城市数据缺失时的回退目录(TEST DATA)
	} `json:"data"`

	// Params 为全局时间权重模型;城市差异主要体现在数据本身。
	Params struct {
		WalkingSpeedKmh     float64 `json:"walkingSpeedKmh"`
		BusSpeedKmh         float64 `json:"busSpeedKmh"`
		MetroSpeedKmh       float64 `json:"metroSpeedKmh"`
		TransferPenaltyMin  float64 `json:"transferPenaltyMin"`
		MaxWalkingDistanceM float64 `json:"maxWalkingDistanceM"`
		EntryStationsLimit  int     `json:"entryStationsLimit"`
		WalkLinkMaxM        float64 `json:"walkLinkMaxM"`
	} `json:"params"`

	Grid struct {
		CellSizeM   float64   `json:"cellSizeM"`
		WalkRadiusM float64   `json:"walkRadiusM"`
		Levels      []float64 `json:"levels"`
	} `json:"grid"`

	// Cities 启动时从 data.root 下的各城市 city.json 自动发现。
	Cities []CityConfig `json:"cities"`
}

// CityConfig 单个城市的接入配置。
type CityConfig struct {
	Code    string     `json:"code"`
	Name    string     `json:"name"`
	BBox    [4]float64 `json:"bbox"`
	DataDir string     `json:"dataDir"`
}

// cityMetadata 对应 Python 数据预处理阶段生成的 city.json。
type cityMetadata struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	BBox      []float64 `json:"bbox"`
	NodeCount int       `json:"nodeCount"`
	EdgeCount int       `json:"edgeCount"`
	Modes     []string  `json:"modes"`
}

// Load 读取配置文件，并自动发现城市数据。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	// 未手动配置 cities 时，自动扫描 data.root。
	if len(c.Cities) == 0 {
		if c.Data.Root == "" {
			return nil, fmt.Errorf("未配置 data.root,无法自动发现城市")
		}

		cities, err := discoverCities(c.Data.Root)
		if err != nil {
			return nil, err
		}

		c.Cities = cities
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	return &c, nil
}

// discoverCities 扫描城市数据根目录。
// 每个一级子目录需要包含 city.json。
func discoverCities(root string) ([]CityConfig, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("读取城市数据目录失败 %q: %w", root, err)
	}

	var cities []CityConfig

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(root, entry.Name())
		metaPath := filepath.Join(dir, "city.json")

		raw, err := os.ReadFile(metaPath)
		if err != nil {
			// 没有 city.json 的目录不是城市数据，跳过。
			continue
		}

		var meta cityMetadata
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf(
				"解析城市元数据失败 %q: %w",
				metaPath,
				err,
			)
		}

		if meta.Code == "" || meta.Name == "" {
			return nil, fmt.Errorf(
				"城市元数据缺少 code/name: %s",
				metaPath,
			)
		}

		if len(meta.BBox) != 4 {
			return nil, fmt.Errorf(
				"城市 %s bbox 必须包含 4 个值",
				meta.Code,
			)
		}

		cities = append(cities, CityConfig{
			Code:    meta.Code,
			Name:    meta.Name,
			BBox:    [4]float64{meta.BBox[0], meta.BBox[1], meta.BBox[2], meta.BBox[3]},
			DataDir: dir,
		})
	}

	// 保证启动时城市顺序稳定。
	sort.Slice(cities, func(i, j int) bool {
		return cities[i].Code < cities[j].Code
	})

	if len(cities) == 0 {
		return nil, fmt.Errorf(
			"未在 %q 中发现城市数据(city.json)",
			root,
		)
	}

	return cities, nil
}

func (c *Config) validate() error {
	p := &c.Params

	if p.WalkingSpeedKmh <= 0 ||
		p.BusSpeedKmh <= 0 ||
		p.MetroSpeedKmh <= 0 {
		return fmt.Errorf(
			"速度参数必须为正: walk=%v bus=%v metro=%v",
			p.WalkingSpeedKmh,
			p.BusSpeedKmh,
			p.MetroSpeedKmh,
		)
	}

	if p.TransferPenaltyMin < 0 ||
		p.MaxWalkingDistanceM <= 0 ||
		p.EntryStationsLimit <= 0 {
		return fmt.Errorf("换乘惩罚/步行距离/入口站点数配置非法")
	}

	if p.WalkLinkMaxM < 0 || p.WalkLinkMaxM > 2000 {
		return fmt.Errorf("walkLinkMaxM 非法(应为 0~2000)")
	}

	if c.Grid.CellSizeM <= 0 ||
		c.Grid.WalkRadiusM <= 0 ||
		len(c.Grid.Levels) == 0 {
		return fmt.Errorf("网格配置非法")
	}

	if len(c.Cities) == 0 {
		return fmt.Errorf("未发现任何城市")
	}

	seen := map[string]bool{}

	for i, ct := range c.Cities {
		if ct.Code == "" ||
			ct.Name == "" ||
			ct.DataDir == "" {
			return fmt.Errorf(
				"cities[%d] 缺少 code/name/dataDir",
				i,
			)
		}

		if seen[ct.Code] {
			return fmt.Errorf(
				"cities code 重复: %s",
				ct.Code,
			)
		}

		seen[ct.Code] = true

		b := ct.BBox
		if !(
			b[0] < b[2] &&
				b[1] < b[3] &&
				inRange(b[0], b[2], -180, 180) &&
				inRange(b[1], b[3], -90, 90)) {
			return fmt.Errorf(
				"cities[%d] bbox 非法: %v",
				i,
				b,
			)
		}
	}

	if c.Server.Port <= 0 {
		c.Server.Port = 8080
	}

	return nil
}

func inRange(min, max, lo, hi float64) bool {
	return lo <= min &&
		min <= hi &&
		lo <= max &&
		max <= hi
}