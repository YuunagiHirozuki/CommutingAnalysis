package geo

import (
	"math"
	"testing"
)

func TestHaversine(t *testing.T) {
	// 广州天河到珠江新城约 5km 量级;用一对已知距离的点:
	// 赤道上经度差 1 度 ≈ 111.32km
	d := Haversine(0, 0, 0, 1)
	if math.Abs(d-111320) > 500 {
		t.Fatalf("赤道 1 经度差应约 111320m,实际 %.1f", d)
	}
	// 同点距离为 0
	if d := Haversine(23.1291, 113.2644, 23.1291, 113.2644); d != 0 {
		t.Fatalf("同点距离应为 0,实际 %v", d)
	}
}

func TestStopIndexNearby(t *testing.T) {
	stops := []StopRef{
		{1, 23.1300, 113.2600},
		{2, 23.1310, 113.2700}, // 距 1 约 1.05km
		{3, 23.1301, 113.2602}, // 距 1 很近
	}
	ix := BuildStopIndex(stops, 800, 23.13)
	got := ix.Nearby(23.1300, 113.2600, 500)
	if len(got) != 2 || got[0].ID == 2 {
		t.Fatalf("500m 内应只有站点 1、3,实际 %v", got)
	}
	got = ix.Nearby(23.1300, 113.2600, 1500)
	if len(got) != 3 {
		t.Fatalf("1500m 内应有 3 个站点,实际 %d", len(got))
	}
}

func TestClassify(t *testing.T) {
	levels := []float64{30, 45, 60}
	cases := map[float64]int{25: 1, 30: 1, 30.1: 2, 45: 2, 50: 3, 60: 3, 61: 4, 90: 4}
	for in, want := range cases {
		if got := Classify(in, levels); got != want {
			t.Fatalf("Classify(%v) 应为 %d,实际 %d", in, want, got)
		}
	}
}

// TestComputeCellsBasic 三个沿线站点、时间递增,验证:
// 网格时间 = min(站点时间 + 中心步行时间);超出 MaxTime 的网格被剪掉。
func TestComputeCellsBasic(t *testing.T) {
	// 沿经度排开的三个站,间距约 0.011 度 ≈ 1.13km
	stops := []StopRef{
		{1, 23.10, 113.2600},
		{2, 23.10, 113.2711},
		{3, 23.10, 113.2822},
	}
	times := map[int64]float64{1: 0, 2: 3, 3: 30}
	cfg := GridConfig{
		CellSizeM:    500,
		WalkRadiusM:  600,
		WalkSpeedKmh: 5,
		Levels:       []float64{30, 45, 60},
		MaxTime:      40,
	}
	cells := ComputeCells(times, stops, cfg)
	if len(cells) == 0 {
		t.Fatal("应产生可达网格")
	}
	// 距站点 1 最近的网格,其中心距站不超过半格对角线(~353m),
	// 步行 353m@5km/h ≈ 4.2min,故时间应 ≤ 4.5
	best := math.Inf(1)
	bestDist := math.Inf(1)
	for _, c := range cells {
		d := Haversine(c.CenterLat, c.CenterLon, 23.10, 113.2600)
		if d < bestDist {
			bestDist = d
			best = c.TimeMin
		}
	}
	if best > 4.5 {
		t.Fatalf("站点 1 最近网格(中心距 %.0fm)时间应≤4.5min,实际 %.2f", bestDist, best)
	}
	// 站点 3 时间 30 + 步行,应存在 >30 的 level-2 网格
	hasLevel2 := false
	for _, c := range cells {
		if c.Level == 2 {
			hasLevel2 = true
		}
	}
	if !hasLevel2 {
		t.Fatal("站点 3 周围应存在 level=2(30~45min)网格")
	}
}

func TestCellsToGeoJSON(t *testing.T) {
	cells := []Cell{{
		Row: 0, Col: 0,
		MinLon: 113.26, MaxLon: 113.265,
		MinLat: 23.10, MaxLat: 23.1045,
		CenterLat: 23.1022, CenterLon: 113.2625,
		TimeMin: 12.34, Level: 1,
	}}
	fc := CellsToGeoJSON(cells)
	if fc.Type != "FeatureCollection" || len(fc.Features) != 1 {
		t.Fatalf("FeatureCollection 结构错误")
	}
	f := fc.Features[0]
	ring := f.Geometry.Coordinates.([][][]float64)[0]
	if len(ring) != 5 {
		t.Fatalf("多边形环应有 5 个点(含闭合),实际 %d", len(ring))
	}
	if ring[0][0] != ring[4][0] || ring[0][1] != ring[4][1] {
		t.Fatal("多边形环未闭合")
	}
	if f.Geometry.Type != "Polygon" {
		t.Fatalf("几何类型应为 Polygon")
	}
	if f.Properties["level"] != 1 {
		t.Fatalf("level 属性错误: %v", f.Properties["level"])
	}
}
