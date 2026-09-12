package service

import (
	"math"
	"testing"

	"backend/internal/geo"
	"backend/internal/graph"
)

// 跨河最小测试案例:
//
//	西岸 A 区                河流(约3km宽)               东岸 B 区
//	A1 ──bus 1km── A2 ══桥 bus 4.6km══ B1 ──bus 1km── B2
//
// 验证:
//  1. 有桥边时 Dijkstra 能沿公共交通网络跨河,A1→B2 = 三段乘车时间之和;
//  2. 无桥边时东岸不可达——不因直线距离相近而泄漏(河宽 >> 步行接入半径);
//  3. 等时圈网格:有桥时东岸出现可达网格;河道带(无站点区域)任何情况下
//     都不产生可达网格;无桥时东岸网格数为 0。
//
// 几何:纬度 23.10,1° 经度 ≈ 1023m。河道 lon[113.475, 113.505] 约 3.07km,
// 远大于网格步行接入半径(800m),步行接入不可能跨河"泄漏"。
func riverTestGraph(t *testing.T, withBridge bool) (*graph.Graph, map[string]graph.Node) {
	t.Helper()
	nodes := []graph.Node{
		{ID: 1, Name: "A1", Lat: 23.10, Lon: 113.455, Type: "bus", City: "TEST"},
		{ID: 2, Name: "A2", Lat: 23.10, Lon: 113.465, Type: "bus", City: "TEST"},
		{ID: 3, Name: "B1", Lat: 23.10, Lon: 113.510, Type: "bus", City: "TEST"},
		{ID: 4, Name: "B2", Lat: 23.10, Lon: 113.520, Type: "bus", City: "TEST"},
	}
	edges := []graph.Edge{
		{From: 1, To: 2, DistanceM: 1023, Mode: "bus"},
		{From: 3, To: 4, DistanceM: 1023, Mode: "bus"},
	}
	if withBridge {
		edges = append(edges, graph.Edge{From: 2, To: 3, DistanceM: 4604, Mode: "bus"})
	}
	p := graph.SpeedParams{WalkingSpeedKmh: 5, BusSpeedKmh: 20, MetroSpeedKmh: 35, TransferPenaltyMin: 5}
	g, err := graph.Build(nodes, edges, p)
	if err != nil {
		t.Fatalf("建图失败: %v", err)
	}
	return g, map[string]graph.Node{"A1": nodes[0], "A2": nodes[1], "B1": nodes[2], "B2": nodes[3]}
}

func riverGridConfig() geo.GridConfig {
	return geo.GridConfig{
		CellSizeM: 500, WalkRadiusM: 800, WalkSpeedKmh: 5,
		Levels: []float64{30, 45, 60}, MaxTime: 45,
	}
}

func TestIsochroneAcrossRiver(t *testing.T) {
	g, nm := riverTestGraph(t, true)

	// 1. Dijkstra 跨河:各段 bus 时间 = 距离/(20km/h),A1→B2 = 三段之和
	edgeTime := func(m float64) float64 { return m / (20000.0 / 60) }
	want := edgeTime(1023) + edgeTime(4604) + edgeTime(1023)
	dist := g.MultiSourceDijkstra(map[int64]float64{1: 0}, 60)
	if math.Abs(dist[4]-want) > 0.1 {
		t.Fatalf("有桥时 A1→B2 应为 %.1f 分钟(沿桥路径),实际 %v", want, dist[4])
	}

	// 2. 等时圈:东岸出现可达网格
	stops := []geo.StopRef{
		{ID: 1, Lat: nm["A1"].Lat, Lon: nm["A1"].Lon}, {ID: 2, Lat: nm["A2"].Lat, Lon: nm["A2"].Lon},
		{ID: 3, Lat: nm["B1"].Lat, Lon: nm["B1"].Lon}, {ID: 4, Lat: nm["B2"].Lat, Lon: nm["B2"].Lon},
	}
	cells := geo.ComputeCells(dist, stops, riverGridConfig())
	east, riverBand := 0, 0
	for _, c := range cells {
		if c.CenterLon > 113.505 {
			east++
		}
		if c.CenterLon > 113.477 && c.CenterLon < 113.503 {
			riverBand++
		}
	}
	if east == 0 {
		t.Fatal("有桥时东岸应出现可达网格")
	}
	if riverBand != 0 {
		t.Fatalf("河道带(无站点)不应有可达网格,实际 %d 个", riverBand)
	}
}

func TestIsochroneNoBridge(t *testing.T) {
	g, nm := riverTestGraph(t, false)

	// 1. 无桥:Dijkstra 不可达东岸(不因直线距离泄漏)
	dist := g.MultiSourceDijkstra(map[int64]float64{1: 0}, 60)
	if _, ok := dist[3]; ok {
		t.Fatal("无桥时 B1 不应可达")
	}
	if _, ok := dist[4]; ok {
		t.Fatal("无桥时 B2 不应可达")
	}

	// 2. 等时圈:只传入可达站点(与真实流程一致),东岸不应产生网格
	stops := []geo.StopRef{{ID: 1, Lat: nm["A1"].Lat, Lon: nm["A1"].Lon}, {ID: 2, Lat: nm["A2"].Lat, Lon: nm["A2"].Lon}}
	cells := geo.ComputeCells(dist, stops, riverGridConfig())
	for _, c := range cells {
		if c.CenterLon > 113.505 {
			t.Fatalf("无桥时东岸不应有可达网格,发现 (%.4f, %.4f)", c.CenterLat, c.CenterLon)
		}
	}
}

// TestWalkLinkConnectsSameStop 站点接驳:同物理站点的两个异 ID 停靠点
// (相距 8m,如 CPTOND 的 "客村"/"珠影(地铁客村站)")应通过 walk 边连通,
// 且接驳代价包含换乘惩罚,永远不会比乘车边快。
func TestWalkLinkConnectsSameStop(t *testing.T) {
	nodes := []graph.Node{
		{ID: 1, Name: "客村", Lat: 23.0930, Lon: 113.2860, Type: "bus", City: "TEST"},
		{ID: 2, Name: "珠影(地铁客村站)", Lat: 23.0931, Lon: 113.2861, Type: "metro", City: "TEST"},
		{ID: 3, Name: "地铁下一站", Lat: 23.1000, Lon: 113.2861, Type: "metro", City: "TEST"},
	}
	edges := []graph.Edge{{From: 2, To: 3, DistanceM: 780, Mode: "metro"}}
	p := graph.SpeedParams{
		WalkingSpeedKmh: 5, BusSpeedKmh: 20, MetroSpeedKmh: 35,
		TransferPenaltyMin: 5, WalkLinkMaxM: 200,
	}
	g, err := graph.Build(nodes, edges, p)
	if err != nil {
		t.Fatal(err)
	}
	dist := g.MultiSourceDijkstra(map[int64]float64{1: 0}, 60)
	// 客村 --walk(8m≈0.1min+5min 惩罚)-- 珠影 --metro(780m=1.3min)-- 下一站
	want := 8/(5000.0/60) + 5 + 780/(35000.0/60)
	if math.Abs(dist[3]-want) > 0.1 {
		t.Fatalf("经接驳边换乘应为 %.2f 分钟,实际 %v", want, dist[3])
	}
	// walk 边时间必须高于同距离乘车(惩罚生效,不会被当作捷径)
	arcT := g.Neighbors(1)[0].TimeMin
	if arcT < p.TransferPenaltyMin {
		t.Fatalf("接驳 walk 边应包含 ≥%.0f 分钟惩罚,实际 %.2f", p.TransferPenaltyMin, arcT)
	}
}
