package geo

import (
	"fmt"
	"math"
)

// StopRef 参与空间索引的站点最小信息。
type StopRef struct {
	ID  int64
	Lat float64
	Lon float64
}

// StopIndex 均匀网格哈希空间索引(按 refLat 处的每度米数划分)。
// 站点总量在万级,均匀网格 + 3×3 邻域查询已足够,无需 R/KD 树。
type StopIndex struct {
	cellM   float64
	refLat  float64
	dLat    float64
	dLon    float64
	cells   map[string][]StopRef
}

// BuildStopIndex cellM 为索引格边长(米),建议等于查询半径。
func BuildStopIndex(stops []StopRef, cellM, refLat float64) *StopIndex {
	ix := &StopIndex{
		cellM:  cellM,
		refLat: refLat,
		dLat:   cellM / MetersPerDegLat(),
		dLon:   cellM / MetersPerDegLon(refLat),
		cells:  make(map[string][]StopRef, len(stops)),
	}
	for _, s := range stops {
		k := ix.key(s.Lat, s.Lon)
		ix.cells[k] = append(ix.cells[k], s)
	}
	return ix
}

func (ix *StopIndex) key(lat, lon float64) string {
	r := math.Floor((lat + 90) / ix.dLat)
	c := math.Floor((lon + 180) / ix.dLon)
	return fmt.Sprintf("%d:%d", int64(r), int64(c))
}

// Nearby 返回 radiusM 范围内的站点(未排序,调用方按需排序)。
func (ix *StopIndex) Nearby(lat, lon, radiusM float64) []StopRef {
	if radiusM > ix.cellM {
		// 索引按 cellM 构建,更大半径时逐环扫描
		rings := int(math.Ceil(radiusM / ix.cellM))
		var out []StopRef
		for dr := -rings; dr <= rings; dr++ {
			for dc := -rings; dc <= rings; dc++ {
				out = append(out, ix.nearbyCell(lat, lon, radiusM, dr, dc)...)
			}
		}
		return out
	}
	var out []StopRef
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			out = append(out, ix.nearbyCell(lat, lon, radiusM, dr, dc)...)
		}
	}
	return out
}

func (ix *StopIndex) nearbyCell(lat, lon, radiusM float64, dr, dc int) []StopRef {
	r := math.Floor((lat+90)/ix.dLat) + float64(dr)
	c := math.Floor((lon+180)/ix.dLon) + float64(dc)
	var out []StopRef
	for _, s := range ix.cells[fmt.Sprintf("%d:%d", int64(r), int64(c))] {
		if Haversine(lat, lon, s.Lat, s.Lon) <= radiusM {
			out = append(out, s)
		}
	}
	return out
}

// GridConfig 网格计算参数。
type GridConfig struct {
	CellSizeM    float64   // 网格边长,默认 500m
	WalkRadiusM  float64   // 网格中心→站点 的最大出网步行距离
	WalkSpeedKmh float64   // 步行速度
	Levels       []float64 // 分级断点,如 [30,45,60]
	MaxTime      float64   // 时间预算(分钟)
}

// Cell 一个可达网格。
type Cell struct {
	Row, Col                  int
	MinLon, MaxLon, MinLat, MaxLat float64
	CenterLat, CenterLon      float64
	TimeMin                   float64 // 理论可达时间
	Level                     int     // 1..len(Levels)+1
}

// Classify 时间 → 等级(1=最快档)。
func Classify(t float64, levels []float64) int {
	for i, lv := range levels {
		if t <= lv {
			return i + 1
		}
	}
	return len(levels) + 1
}

// ComputeCells 空间网格法:对可达站点包围盒划分网格,
// 每个网格中心在步行半径内找站点,取 min(站点时间 + 步行时间) 为网格理论可达时间。
func ComputeCells(times map[int64]float64, stops []StopRef, cfg GridConfig) []Cell {
	if len(stops) == 0 {
		return nil
	}
	// 包围盒 + 步行半径外扩,保证边缘网格不被截断
	minLat, maxLat := stops[0].Lat, stops[0].Lat
	minLon, maxLon := stops[0].Lon, stops[0].Lon
	for _, s := range stops {
		minLat, maxLat = math.Min(minLat, s.Lat), math.Max(maxLat, s.Lat)
		minLon, maxLon = math.Min(minLon, s.Lon), math.Max(maxLon, s.Lon)
	}
	centerLat := (minLat + maxLat) / 2
	padLat := (cfg.WalkRadiusM + cfg.CellSizeM) / MetersPerDegLat()
	padLon := (cfg.WalkRadiusM + cfg.CellSizeM) / MetersPerDegLon(centerLat)
	minLat, maxLat = minLat-padLat, maxLat+padLat
	minLon, maxLon = minLon-padLon, maxLon+padLon

	dLat := cfg.CellSizeM / MetersPerDegLat()
	dLon := cfg.CellSizeM / MetersPerDegLon(centerLat)
	rows := int(math.Ceil((maxLat - minLat) / dLat))
	cols := int(math.Ceil((maxLon - minLon) / dLon))

	ix := BuildStopIndex(stops, cfg.WalkRadiusM, centerLat)
	walkMPerMin := cfg.WalkSpeedKmh * 1000 / 60

	cells := make([]Cell, 0)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			clat := minLat + (float64(r)+0.5)*dLat
			clon := minLon + (float64(c)+0.5)*dLon
			best := math.Inf(1)
			for _, s := range ix.Nearby(clat, clon, cfg.WalkRadiusM) {
				t := times[s.ID] + Haversine(clat, clon, s.Lat, s.Lon)/walkMPerMin
				if t < best {
					best = t
				}
			}
			if math.IsInf(best, 1) || best > cfg.MaxTime {
				continue
			}
			cells = append(cells, Cell{
				Row: r, Col: c,
				MinLon: minLon + float64(c)*dLon, MaxLon: minLon + float64(c+1)*dLon,
				MinLat: minLat + float64(r)*dLat, MaxLat: minLat + float64(r+1)*dLat,
				CenterLat: clat, CenterLon: clon,
				TimeMin: best,
				Level:   Classify(best, cfg.Levels),
			})
		}
	}
	return cells
}

// ---- GeoJSON 输出 ----

// Geometry GeoJSON 几何(Polygon)。
type Geometry struct {
	Type        string          `json:"type"`
	Coordinates [][][]float64   `json:"coordinates"`
}

// Feature GeoJSON 要素。
type Feature struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Geometry   *Geometry      `json:"geometry"`
}

// FeatureCollection GeoJSON 要素集合。
type FeatureCollection struct {
	Type     string     `json:"type"`
	Features []Feature  `json:"features"`
}

// CellsToGeoJSON 把可达网格转为 GeoJSON FeatureCollection(多边形,含时间与等级属性)。
func CellsToGeoJSON(cells []Cell) *FeatureCollection {
	fc := &FeatureCollection{Type: "FeatureCollection", Features: make([]Feature, 0, len(cells))}
	for i := range cells {
		c := &cells[i]
		ring := [][]float64{
			{c.MinLon, c.MinLat},
			{c.MaxLon, c.MinLat},
			{c.MaxLon, c.MaxLat},
			{c.MinLon, c.MaxLat},
			{c.MinLon, c.MinLat}, // 闭合
		}
		fc.Features = append(fc.Features, Feature{
			Type: "Feature",
			Properties: map[string]any{
				"time_min": math.Round(c.TimeMin*10) / 10,
				"level":    c.Level,
			},
			Geometry: &Geometry{Type: "Polygon", Coordinates: [][][]float64{ring}},
		})
	}
	return fc
}
