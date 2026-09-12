// Package service 组合图数据加载、可达性计算与附近站点查询。
package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"backend/internal/config"
	"backend/internal/geo"
	"backend/internal/graph"
)

// Engine 持有一个城市的已加载公共交通图与空间索引(启动时加载进内存)。
type Engine struct {
	Graph *graph.Graph
	Index *geo.StopIndex
	Stops []geo.StopRef // 全量站点(用于最近站查找)
	Cfg   *config.Config
	City  config.CityConfig

	IsTest bool  // 是否为 TEST DATA
	LoadMs int64 // 加载耗时
}

// LoadCity 加载单个城市的图数据。数据目录不存在时返回错误,由调用方决定回退策略。
func LoadCity(cfg *config.Config, city config.CityConfig) (*Engine, error) {
	dir := city.DataDir
	if _, err := os.Stat(filepath.Join(dir, "nodes.json")); err != nil {
		return nil, fmt.Errorf("城市 %s 图数据缺失: %s/nodes.json: %w", city.Name, dir, err)
	}
	start := time.Now()

	var nodes []graph.Node
	if err := readJSON(filepath.Join(dir, "nodes.json"), &nodes); err != nil {
		return nil, fmt.Errorf("读取 nodes.json 失败: %w", err)
	}
	var edges []graph.Edge
	if err := readJSON(filepath.Join(dir, "edges.json"), &edges); err != nil {
		return nil, fmt.Errorf("读取 edges.json 失败: %w", err)
	}

	eng, err := buildEngine(cfg, city, nodes, edges)
	if err != nil {
		return nil, err
	}
	eng.LoadMs = time.Since(start).Milliseconds()
	return eng, nil
}

// buildEngine 由节点/边构建图与空间索引(LoadCity 与 LoadMerged 共用)。
func buildEngine(cfg *config.Config, city config.CityConfig, nodes []graph.Node, edges []graph.Edge) (*Engine, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("节点数据为空: %s", city.DataDir)
	}
	p := graph.SpeedParams{
		WalkingSpeedKmh:    cfg.Params.WalkingSpeedKmh,
		BusSpeedKmh:        cfg.Params.BusSpeedKmh,
		MetroSpeedKmh:      cfg.Params.MetroSpeedKmh,
		TransferPenaltyMin: cfg.Params.TransferPenaltyMin,
		WalkLinkMaxM:       cfg.Params.WalkLinkMaxM,
	}
	g, err := graph.Build(nodes, edges, p)
	if err != nil {
		return nil, fmt.Errorf("构建图失败: %w", err)
	}
	stops := make([]geo.StopRef, 0, len(nodes))
	for i := range nodes {
		stops = append(stops, geo.StopRef{ID: nodes[i].ID, Lat: nodes[i].Lat, Lon: nodes[i].Lon})
	}
	return &Engine{
		Graph:  g,
		Index:  geo.BuildStopIndex(stops, cfg.Params.MaxWalkingDistanceM, (city.BBox[1]+city.BBox[3])/2),
		Stops:  stops,
		Cfg:    cfg,
		City:   city,
		LoadMs: 0,
	}, nil
}

// LoadFallback 将 TEST DATA 目录加载为回退引擎(保留城市范围用于坐标校验)。
func LoadFallback(cfg *config.Config, scope config.CityConfig) (*Engine, error) {
	eng, err := LoadCity(cfg, config.CityConfig{
		Code:    scope.Code,
		Name:    scope.Name + "(TEST DATA)",
		BBox:    scope.BBox,
		DataDir: cfg.Data.Fallback,
	})
	if err != nil {
		return nil, err
	}
	eng.IsTest = true
	return eng, nil
}

// LoadMerged 把多个城市的节点/边合并进同一张交通网络图(为跨城市通勤预留,
// 例如广州→佛山)。当前 MVP 不在 API 中启用,仅保证该路径可用且有测试覆盖。
//
// 关键设计:各城市数据文件使用各自的本地 int64 ID,直接合并必然冲突,
// 因此按城市做 ID 偏移重映射;Graph/Build 不做任何"同城"假设,
// 跨城市边(如佛山站→广州站的城际巴士/walk 边)与城内边完全同等处理。
func LoadMerged(cfg *config.Config, cities []config.CityConfig) (*Engine, error) {
	if len(cities) == 0 {
		return nil, fmt.Errorf("合并城市列表为空")
	}
	start := time.Now()

	var nodes []graph.Node
	var edges []graph.Edge
	for _, ct := range cities {
		var cn []graph.Node
		var ce []graph.Edge
		if err := readJSON(filepath.Join(ct.DataDir, "nodes.json"), &cn); err != nil {
			return nil, fmt.Errorf("城市 %s 读取 nodes.json 失败: %w", ct.Code, err)
		}
		if err := readJSON(filepath.Join(ct.DataDir, "edges.json"), &ce); err != nil {
			return nil, fmt.Errorf("城市 %s 读取 edges.json 失败: %w", ct.Code, err)
		}
		offset := int64(len(nodes))
		local := make(map[int64]int64, len(cn))
		for i := range cn {
			n := cn[i]
			gid := n.ID + offset
			local[n.ID] = gid
			if n.City == "" {
				n.City = ct.Code
			}
			n.ID = gid
			nodes = append(nodes, n)
		}
		for _, e := range ce {
			edges = append(edges, graph.Edge{
				From:      local[e.From],
				To:        local[e.To],
				DistanceM: e.DistanceM,
				Mode:      e.Mode,
			})
		}
	}

	// 合并后 bbox 取各城市包围盒的并集
	union := cities[0].BBox
	for _, ct := range cities[1:] {
		union[0] = min(union[0], ct.BBox[0])
		union[1] = min(union[1], ct.BBox[1])
		union[2] = max(union[2], ct.BBox[2])
		union[3] = max(union[3], ct.BBox[3])
	}
	scope := config.CityConfig{
		Code:    joinCodes(cities),
		Name:    joinNames(cities),
		BBox:    union,
		DataDir: cities[0].DataDir,
	}
	eng, err := buildEngine(cfg, scope, nodes, edges)
	if err != nil {
		return nil, err
	}
	eng.LoadMs = time.Since(start).Milliseconds()
	return eng, nil
}

func joinCodes(cities []config.CityConfig) string {
	s := cities[0].Code
	for _, ct := range cities[1:] {
		s += "+" + ct.Code
	}
	return s
}

func joinNames(cities []config.CityConfig) string {
	s := cities[0].Name
	for _, ct := range cities[1:] {
		s += "→" + ct.Name
	}
	return s
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// StationInfo 附近站点查询结果。
type StationInfo struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	DistanceM float64 `json:"distanceM"`
	WalkMin   float64 `json:"walkMin"`
}

// NearbyStations 查询(lat,lon)半径 radiusM 内站点,按距离排序,最多 limit 个。
func (e *Engine) NearbyStations(lat, lon, radiusM float64, limit int) []StationInfo {
	refs := e.Index.Nearby(lat, lon, radiusM)
	out := make([]StationInfo, 0, len(refs))
	walkMPerMin := e.Cfg.Params.WalkingSpeedKmh * 1000 / 60
	for _, r := range refs {
		d := geo.Haversine(lat, lon, r.Lat, r.Lon)
		n := e.Graph.Nodes[r.ID]
		if n == nil {
			continue
		}
		out = append(out, StationInfo{
			ID: n.ID, Name: n.Name, Type: n.Type, Lat: n.Lat, Lon: n.Lon,
			DistanceM: d, WalkMin: d / walkMPerMin,
		})
	}
	// 按距离排序(插入排序,limit 很小)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].DistanceM < out[j-1].DistanceM; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// NearestStations 距离最近的 limit 个站点,不设距离上限。
// 万级站点全量扫描 <1ms,无需空间索引。
func (e *Engine) NearestStations(lat, lon float64, limit int) []StationInfo {
	walkMPerMin := e.Cfg.Params.WalkingSpeedKmh * 1000 / 60
	out := make([]StationInfo, 0, limit)
	for _, r := range e.Stops {
		d := geo.Haversine(lat, lon, r.Lat, r.Lon)
		n := e.Graph.Nodes[r.ID]
		if n == nil {
			continue
		}
		s := StationInfo{
			ID: n.ID, Name: n.Name, Type: n.Type, Lat: n.Lat, Lon: n.Lon,
			DistanceM: d, WalkMin: d / walkMPerMin,
		}
		// 维护按距离升序的前 limit 个(插入排序)
		pos := len(out)
		for pos > 0 && out[pos-1].DistanceM > d {
			pos--
		}
		if pos == limit {
			continue
		}
		out = append(out, StationInfo{})
		copy(out[pos+1:], out[pos:])
		out[pos] = s
		if len(out) > limit {
			out = out[:limit]
		}
	}
	return out
}
