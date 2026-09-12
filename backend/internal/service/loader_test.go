package service

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/config"
	"backend/internal/graph"
)

func testConfig(fallback string) *config.Config {
	c := &config.Config{}
	c.Params.WalkingSpeedKmh = 5
	c.Params.BusSpeedKmh = 20
	c.Params.MetroSpeedKmh = 35
	c.Params.TransferPenaltyMin = 5
	c.Params.MaxWalkingDistanceM = 1500
	c.Params.EntryStationsLimit = 6
	c.Grid.CellSizeM = 500
	c.Grid.WalkRadiusM = 800
	c.Grid.Levels = []float64{30, 45, 60}
	c.Data.Fallback = fallback
	return c
}

func writeCity(t *testing.T, dir string, nodes []graph.Node, edges []graph.Edge) config.CityConfig {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w := func(name string, v any) {
		raw, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	w("nodes.json", nodes)
	w("edges.json", edges)
	return config.CityConfig{Code: filepath.Base(dir), Name: filepath.Base(dir), BBox: [4]float64{113, 22, 114, 23.5}, DataDir: dir}
}

// TestLoadMergedCrossCity 验证多城合并:两城本地 ID 冲突时正确重映射,
// 且跨城市边(城市A站点 → 城市B站点)与城内边同等参与 Dijkstra。
// 场景:广州站 A --城内边-- B;佛山站 X --城内边-- Y;B --跨城边(10km bus)-- X。
// A 出发 45 分钟应能到达 Y:A→B(3min)+跨城(30min)+X→Y(3min)=36min。
func TestLoadMergedCrossCity(t *testing.T) {
	root := t.TempDir()
	// 两城节点本地 ID 都从 1 开始 —— 故意制造冲突
	gzNodes := []graph.Node{
		{ID: 1, Name: "广州A", Lat: 23.10, Lon: 113.26, Type: "bus", City: "guangzhou"},
		{ID: 2, Name: "广州B", Lat: 23.11, Lon: 113.26, Type: "bus", City: "guangzhou"},
	}
	gzEdges := []graph.Edge{{From: 1, To: 2, DistanceM: 1000, Mode: "bus"}} // 3min
	fsNodes := []graph.Node{
		{ID: 1, Name: "佛山X", Lat: 23.02, Lon: 113.10, Type: "bus", City: "foshan"},
		{ID: 2, Name: "佛山Y", Lat: 23.03, Lon: 113.10, Type: "bus", City: "foshan"},
	}
	fsEdges := []graph.Edge{{From: 1, To: 2, DistanceM: 1000, Mode: "bus"}} // 3min
	c1 := writeCity(t, filepath.Join(root, "guangzhou"), gzNodes, gzEdges)
	c2 := writeCity(t, filepath.Join(root, "foshan"), fsNodes, fsEdges)

	cfg := testConfig("")
	eng, err := LoadMerged(cfg, []config.CityConfig{c1, c2})
	if err != nil {
		t.Fatalf("合并加载失败: %v", err)
	}
	if got := eng.Graph.NodeCount(); got != 4 {
		t.Fatalf("合并后应有 4 节点,实际 %d", got)
	}

	// 含跨城市边的完整场景(全局 ID:广州A=0, 广州B=1, 佛山X=2, 佛山Y=3):
	// B(广州)→ X(佛山)10km bus = 30min,与城内边同等处理
	nodesAll := []graph.Node{
		{ID: 0, Name: "广州A", Lat: 23.10, Lon: 113.26, Type: "bus", City: "guangzhou"},
		{ID: 1, Name: "广州B", Lat: 23.11, Lon: 113.26, Type: "bus", City: "guangzhou"},
		{ID: 2, Name: "佛山X", Lat: 23.02, Lon: 113.10, Type: "bus", City: "foshan"},
		{ID: 3, Name: "佛山Y", Lat: 23.03, Lon: 113.10, Type: "bus", City: "foshan"},
	}
	edgesAll := []graph.Edge{
		{From: 0, To: 1, DistanceM: 1000, Mode: "bus"},
		{From: 2, To: 3, DistanceM: 1000, Mode: "bus"},
		{From: 1, To: 2, DistanceM: 10000, Mode: "bus"}, // 跨城边
	}
	p := graph.SpeedParams{WalkingSpeedKmh: 5, BusSpeedKmh: 20, MetroSpeedKmh: 35, TransferPenaltyMin: 5}
	g, err := graph.Build(nodesAll, edgesAll, p)
	if err != nil {
		t.Fatalf("含跨城边建图失败: %v", err)
	}
	dist := g.MultiSourceDijkstra(map[int64]float64{0: 0}, 45)
	if math.Abs(dist[3]-36) > 1e-6 {
		t.Fatalf("广州A→佛山Y 应为 3+30+3=36 分钟,实际 %v", dist[3])
	}
	if math.Abs(dist[2]-33) > 1e-6 {
		t.Fatalf("广州A→佛山X 应为 3+30=33 分钟,实际 %v", dist[2])
	}
}
