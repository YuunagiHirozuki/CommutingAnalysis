// Package config 加载系统配置(时间权重模型参数必须可配置,不写死在算法内)。
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config 顶层配置,与 backend/config/config.json 对应。
type Config struct {
	Server struct {
		Port int `json:"port"`
	} `json:"server"`

	// Network 外部服务访问配置(可选代理,用于地址搜索等出网请求;留空直连)。
	// 核心图算法不出网。
	Network struct {
		ProxyURL string `json:"proxyURL"`
	} `json:"network"`

	Data struct {
		Fallback string `json:"fallback"` // 所有城市数据缺失时的回退目录(TEST DATA)
	} `json:"data"`

	// Params 为全局时间权重模型;城市差异主要体现在数据本身。
	Params struct {
		WalkingSpeedKmh     float64 `json:"walkingSpeedKmh"`
		BusSpeedKmh         float64 `json:"busSpeedKmh"`
		MetroSpeedKmh       float64 `json:"metroSpeedKmh"`
		TransferPenaltyMin  float64 `json:"transferPenaltyMin"`
		MaxWalkingDistanceM float64 `json:"maxWalkingDistanceM"` // 附近站点查询默认半径
		EntryStationsLimit  int     `json:"entryStationsLimit"`  // 多源起点数量
		WalkLinkMaxM        float64 `json:"walkLinkMaxM"`        // 站点步行接驳距离上限(同站异ID/换乘),0=关闭
	} `json:"params"`

	Grid struct {
		CellSizeM   float64   `json:"cellSizeM"`
		WalkRadiusM float64   `json:"walkRadiusM"`
		Levels      []float64 `json:"levels"`
	} `json:"grid"`

	// Cities 城市即配置:新增城市只需一条配置 + 一份数据,无需改代码。
	Cities []CityConfig `json:"cities"`
}

// CityConfig 单个城市的接入配置。
type CityConfig struct {
	Code    string     `json:"code"`    // URL 参数标识,如 guangzhou
	Name    string     `json:"name"`    // 显示名,如 广州
	BBox    [4]float64 `json:"bbox"`    // [minLon, minLat, maxLon, maxLat]
	DataDir string     `json:"dataDir"` // nodes.json/edges.json 所在目录
}

// Load 读取配置文件并做基本校验。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	p := &c.Params
	if p.WalkingSpeedKmh <= 0 || p.BusSpeedKmh <= 0 || p.MetroSpeedKmh <= 0 {
		return fmt.Errorf("速度参数必须为正: walk=%v bus=%v metro=%v", p.WalkingSpeedKmh, p.BusSpeedKmh, p.MetroSpeedKmh)
	}
	if p.TransferPenaltyMin < 0 || p.MaxWalkingDistanceM <= 0 || p.EntryStationsLimit <= 0 {
		return fmt.Errorf("换乘惩罚/步行距离/入口站点数配置非法")
	}
	if p.WalkLinkMaxM < 0 || p.WalkLinkMaxM > 2000 {
		return fmt.Errorf("walkLinkMaxM 非法(应为 0~2000)")
	}
	if c.Grid.CellSizeM <= 0 || c.Grid.WalkRadiusM <= 0 || len(c.Grid.Levels) == 0 {
		return fmt.Errorf("网格配置非法")
	}
	if len(c.Cities) == 0 {
		return fmt.Errorf("cities 至少配置一个城市")
	}
	seen := map[string]bool{}
	for i, ct := range c.Cities {
		if ct.Code == "" || ct.Name == "" || ct.DataDir == "" {
			return fmt.Errorf("cities[%d] 缺少 code/name/dataDir", i)
		}
		if seen[ct.Code] {
			return fmt.Errorf("cities code 重复: %s", ct.Code)
		}
		seen[ct.Code] = true
		b := ct.BBox
		if !(b[0] < b[2] && b[1] < b[3] && inRange(b[0], b[2], -180, 180) && inRange(b[1], b[3], -90, 90)) {
			return fmt.Errorf("cities[%d] bbox 非法: %v", i, b)
		}
	}
	if c.Server.Port <= 0 {
		c.Server.Port = 8080
	}
	return nil
}

func inRange(min, max, lo, hi float64) bool {
	return lo <= min && min <= hi && lo <= max && max <= hi
}
