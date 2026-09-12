package service

import (
	"fmt"
	"time"

	"backend/internal/geo"
)

// IsochroneRequest 可达区域计算请求。
type IsochroneRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	MaxTime   float64 `json:"maxTime"` // 分钟
}

// IsochroneResult 可达区域计算结果。
type IsochroneResult struct {
	MaxTime           float64                `json:"maxTime"`
	ReachableStations int                    `json:"reachableStations"`
	EntryStations     []StationInfo          `json:"entryStations"` // 接入图的多源起点
	ComputeMs         int64                  `json:"computeMs"`
	GridCells         int                    `json:"gridCells"`
	GeoJSON           *geo.FeatureCollection `json:"geojson"`
	TestData          bool                   `json:"testData"` // 结果是否基于 TEST DATA
	Notice            string                 `json:"notice,omitempty"` // 数据覆盖范围外的提示
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

	return &IsochroneResult{
		MaxTime:           req.MaxTime,
		ReachableStations: len(times),
		EntryStations:     entries,
		ComputeMs:         time.Since(start).Milliseconds(),
		GridCells:         len(cells),
		GeoJSON:           geo.CellsToGeoJSON(cells),
		TestData:          e.IsTest,
		Notice:            notice,
	}, nil
}
