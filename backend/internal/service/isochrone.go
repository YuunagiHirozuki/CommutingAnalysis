package service

import (
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/geo"
)

// IsochroneRequest 可达区域计算请求。
type IsochroneRequest struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	MaxTime        float64 `json:"maxTime"`        // 分钟
	IncludeNetwork bool    `json:"includeNetwork"` // 是否返回可达子网络线路(前端开关控制)
}

// IsochroneResult 可达区域计算结果。
type IsochroneResult struct {
	MaxTime           float64                `json:"maxTime"`
	ReachableStations int                    `json:"reachableStations"`
	EntryStations     []StationInfo          `json:"entryStations"` // 接入图的多源起点
	ComputeMs         int64                  `json:"computeMs"`
	GridCells         int                    `json:"gridCells"`
	GeoJSON           *geo.FeatureCollection `json:"geojson"`
	TestData          bool                   `json:"testData"`         // 结果是否基于 TEST DATA
	Notice            string                 `json:"notice,omitempty"` // 数据覆盖范围外的提示
	Network           *geo.FeatureCollection `json:"network,omitempty"`
}

// Isochrone 完整流程:附近站点 → 多源接入 → 多源 Dijkstra → 空间网格 → GeoJSON。
func (e *Engine) Isochrone(req IsochroneRequest) (*IsochroneResult, error) {
	p := &e.Cfg.Params
	start := time.Now()

	// 1. 最近的若干站点作为多源起点(不设距离上限,远站步行成本高自然被
	//    Dijkstra 竞争淘汰;步行接入含上车等待的换乘惩罚,与 walk 边建模一致)
	entries := e.NearestStations(req.Latitude, req.Longitude, p.EntryStationsLimit)
	if len(entries) == 0 {
		return nil, fmt.Errorf("公共交通网络为空,无法计算可达区域")
	}
	sources := make(map[int64]float64, len(entries))
	for _, s := range entries {
		sources[s.ID] = s.WalkMin + p.TransferPenaltyMin
	}

	// 2. 多源 Dijkstra(按 maxTime 剪枝;可达站点数用于统计与论文实验)
	times := e.Graph.MultiSourceDijkstra(sources, req.MaxTime)

	// 3. 空间网格 + GeoJSON:按完整 maxTime 计算(含 90 分钟档的 60~90 环带)。
	//    渲染性能由前端 Canvas 渲染器保障(App.vue preferCanvas)。
	stops := make([]geo.StopRef, 0, len(times))
	for id := range times {
		if n := e.Graph.Nodes[id]; n != nil {
			stops = append(stops, geo.StopRef{ID: id, Lat: n.Lat, Lon: n.Lon})
		}
	}
	cfg := geo.GridConfig{
		CellSizeM:    e.Cfg.Grid.CellSizeM,
		WalkRadiusM:  e.Cfg.Grid.WalkRadiusM,
		WalkSpeedKmh: p.WalkingSpeedKmh,
		Levels:       e.Cfg.Grid.Levels,
		MaxTime:      req.MaxTime,
	}
	cells := geo.ComputeCells(times, stops, cfg)

	// 起点在当前城市数据覆盖范围外时照常计算(按最近站点接入),
	// 但返回明确提示,由前端展示
	b := e.City.BBox
	var notice string
	if req.Longitude < b[0] || req.Latitude < b[1] || req.Longitude > b[2] || req.Latitude > b[3] {
		notice = fmt.Sprintf("起点位于「%s」数据覆盖范围之外,结果基于最近站点估算,仅供参考", e.City.Name)
	}

	// 4. 可达子网络线路(仅在前端开关开启时计算):
	//    两端均可达的 bus/metro 边,按无向对去重
	var network *geo.FeatureCollection
	if req.IncludeNetwork {
		network = e.ReachableNetwork(times)
	}

	return &IsochroneResult{
		MaxTime:           req.MaxTime,
		ReachableStations: len(times),
		EntryStations:     entries,
		ComputeMs:         time.Since(start).Milliseconds(),
		GridCells:         len(cells),
		GeoJSON:           geo.CellsToGeoJSON(cells),
		TestData:          e.IsTest,
		Notice:            notice,
		Network:           network,
	}, nil
}

// ReachableNetwork 返回两端站点均可达的原始公交/地铁线路几何。
// 线路几何来自预处理阶段生成的 network.geojson，
// 不再根据站点坐标自行绘制直线。
func (e *Engine) ReachableNetwork(times map[int64]float64) *geo.FeatureCollection {
	if e.Network == nil {
		return &geo.FeatureCollection{
			Type:     "FeatureCollection",
			Features: []geo.Feature{},
		}
	}

	features := make([]geo.Feature, 0, len(e.Network.Features))

	for _, feature := range e.Network.Features {
		fromValue, ok := feature.Properties["from"]
		if !ok {
			continue
		}

		toValue, ok := feature.Properties["to"]
		if !ok {
			continue
		}

		fromID, ok := propertyInt64(fromValue)
		if !ok {
			continue
		}

		toID, ok := propertyInt64(toValue)
		if !ok {
			continue
		}

		// 起点和终点都可达，才显示该线路。
		if _, ok := times[fromID]; !ok {
			continue
		}

		if _, ok := times[toID]; !ok {
			continue
		}

		features = append(features, feature)
	}

	return &geo.FeatureCollection{
		Type:     "FeatureCollection",
		Features: features,
	}
}

// propertyInt64 将 GeoJSON properties 中的数字转换为 int64。
// JSON 反序列化到 map[string]any 后，数字通常会变成 float64。
func propertyInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}