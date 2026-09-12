package graph

import (
	"math"
	"testing"
)

var testParams = SpeedParams{
	WalkingSpeedKmh:    5,
	BusSpeedKmh:        20,
	MetroSpeedKmh:      35,
	TransferPenaltyMin: 5,
}

// buildSmall 构造测试图:nodes 为 (id,name) 序列,edges 的距离按 mode 折算时间。
func buildSmall(t *testing.T, nodes []Node, edges []Edge) *Graph {
	t.Helper()
	g, err := Build(nodes, edges, testParams)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	return g
}

// TestGraphBuildBasic 验证 A→B→C 图的节点/弧统计与邻接关系。
// bus 1000m @20km/h = 3 分钟。
func TestGraphBuildBasic(t *testing.T) {
	g := buildSmall(t,
		[]Node{{1, "A", 23.10, 113.26, "bus", "test"}, {2, "B", 23.11, 113.26, "bus", "test"}, {3, "C", 23.12, 113.26, "bus", "test"}},
		[]Edge{{1, 2, 1000, "bus"}, {2, 3, 1000, "bus"}},
	)
	if g.NodeCount() != 3 || g.ArcCount() != 4 { // 无向:每条边两个方向
		t.Fatalf("节点/弧数错误: nodes=%d arcs=%d", g.NodeCount(), g.ArcCount())
	}
	if n := g.Neighbors(1); len(n) != 1 || n[0].To != 2 {
		t.Fatalf("A 的邻接应为 [B],实际 %v", n)
	}
	if got := g.Neighbors(1)[0].TimeMin; math.Abs(got-3) > 1e-9 {
		t.Fatalf("1000m@20km/h 应为 3 分钟,实际 %v", got)
	}
}

// TestDijkstraShortestPathPerAGENTS:AGENTS.md §24 要求的标准用例。
//
//	A --5--> B
//	A --20--> C
//	B --5--> C     ⇒ A→C = 10 而非 20
//
// 用 metro 速度构造整数分钟:1750m@35km/h=3min,则 2916.667m=5.0000...min 不整。
// 直接用 walk 不行(有换乘惩罚),用 bus:5min = 5000/3 m,非整但时间仍精确可算。
// 为可读性按 20km/h: 5min→5000/3 m,20min→20000/3 m。
func TestDijkstraShortestPathPerAGENTS(t *testing.T) {
	const m1min = 20000.0 / 60 // 20km/h 下 1 分钟的米数(≈333.33m)
	nodes := []Node{
		{1, "A", 23.10, 113.26, "bus", "test"},
		{2, "B", 23.14, 113.26, "bus", "test"},
		{3, "C", 23.18, 113.26, "bus", "test"},
	}
	edges := []Edge{
		{1, 2, 5 * m1min, "bus"},  // 5 分钟
		{1, 3, 20 * m1min, "bus"}, // 20 分钟(劣路)
		{2, 3, 5 * m1min, "bus"},  // 5 分钟
	}
	g := buildSmall(t, nodes, edges)
	dist := g.MultiSourceDijkstra(map[int64]float64{1: 0}, 1e9)
	if math.Abs(dist[3]-10) > 1e-6 {
		t.Fatalf("A→C 应为 10 分钟(经 B),实际 %v", dist[3])
	}
	if math.Abs(dist[2]-5) > 1e-6 {
		t.Fatalf("A→B 应为 5 分钟,实际 %v", dist[2])
	}
}

// TestMultiSourceDijkstra 多源起点:AGENTS.md §24 要求。
// A 初始 5、B 初始 10;C 距 A 3 分钟、距 B 12 分钟 ⇒ C 应取 A 路径 = 8。
func TestMultiSourceDijkstra(t *testing.T) {
	const m1min = 20000.0 / 60
	nodes := []Node{
		{1, "A", 23.10, 113.26, "bus", "test"},
		{2, "B", 23.14, 113.26, "bus", "test"},
		{3, "C", 23.12, 113.26, "bus", "test"},
	}
	edges := []Edge{
		{1, 3, 3 * m1min, "bus"},
		{2, 3, 12 * m1min, "bus"},
	}
	g := buildSmall(t, nodes, edges)
	dist := g.MultiSourceDijkstra(map[int64]float64{1: 5, 2: 10}, 1e9)
	if math.Abs(dist[3]-8) > 1e-6 {
		t.Fatalf("C 应取 A 路径(5+3=8),实际 %v", dist[3])
	}
}

// TestMaxTimeCutoff 验证超过 maxTime 的节点被剪枝。
func TestMaxTimeCutoff(t *testing.T) {
	const m1min = 20000.0 / 60
	nodes := []Node{{1, "A", 23.10, 113.26, "bus", "test"}, {2, "B", 23.20, 113.26, "bus", "test"}}
	g := buildSmall(t, nodes, []Edge{{1, 2, 50 * m1min, "bus"}})
	dist := g.MultiSourceDijkstra(map[int64]float64{1: 0}, 30)
	if _, ok := dist[2]; ok {
		t.Fatalf("50 分钟外的 B 不应出现在结果中")
	}
	if len(dist) != 1 {
		t.Fatalf("应只有 A 可达,实际 %v", dist)
	}
}

// TestWalkEdgePenalty walk 边应包含换乘惩罚:100m@5km/h=1.2min,+5=6.2min。
func TestWalkEdgePenalty(t *testing.T) {
	nodes := []Node{{1, "A", 23.10, 113.26, "bus", "test"}, {2, "M", 23.1001, 113.26, "metro", "test"}}
	g := buildSmall(t, nodes, []Edge{{1, 2, 100, "walk"}})
	if got := g.Neighbors(1)[0].TimeMin; math.Abs(got-6.2) > 1e-9 {
		t.Fatalf("walk 边应为 1.2+5=6.2 分钟,实际 %v", got)
	}
}

// TestParallelEdgeDedup 平行边保留最短:公交 1000m(3min) 与地铁 1750m(3min) 同对时取更短者。
func TestParallelEdgeDedup(t *testing.T) {
	nodes := []Node{
		{1, "A", 23.10, 113.26, "bus", "test"},
		{2, "B", 23.11, 113.26, "metro", "test"},
	}
	edges := []Edge{{1, 2, 1000, "bus"}, {1, 2, 1400, "metro"}} // bus 3min, metro 2.4min
	g := buildSmall(t, nodes, edges)
	ns := g.Neighbors(1)
	if len(ns) != 1 {
		t.Fatalf("平行边应去重为 1 条,实际 %d", len(ns))
	}
	if ns[0].Mode != "metro" || math.Abs(ns[0].TimeMin-2.4) > 1e-9 {
		t.Fatalf("应保留更快的 metro 边(2.4min),实际 mode=%s t=%v", ns[0].Mode, ns[0].TimeMin)
	}
}
