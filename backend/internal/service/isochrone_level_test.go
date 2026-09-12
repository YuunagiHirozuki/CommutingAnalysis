package service

import (
	"path/filepath"
	"testing"

	"backend/internal/graph"
)

// TestIsochronePurpleBandRendered:90 分钟档应完整生成 60~90 分钟的第 4 级
// (紫色)网格——超出最后一级阈值的部分按 maxTime 正常计算与返回,
// 渲染性能由前端 Canvas 渲染器保障,而非在数据端截断。
func TestIsochronePurpleBandRendered(t *testing.T) {
	root := t.TempDir()
	// A→B 20.4km bus ≈ 61.2 分钟:90 分钟档可达 B,形成 >60 分钟的紫色网格
	nodes := []graph.Node{
		{ID: 0, Name: "A站", Lat: 23.10, Lon: 113.40, Type: "bus", City: "TEST"},
		{ID: 1, Name: "B站", Lat: 23.10, Lon: 113.60, Type: "bus", City: "TEST"},
	}
	edges := []graph.Edge{{From: 0, To: 1, DistanceM: 20460, Mode: "bus"}}
	cc := writeCity(t, filepath.Join(root, "gz"), nodes, edges)

	cfg := testConfig("")
	cfg.Params.EntryStationsLimit = 1
	eng, err := LoadCity(cfg, cc)
	if err != nil {
		t.Fatal(err)
	}

	r60, err := eng.Isochrone(IsochroneRequest{Latitude: 23.10, Longitude: 113.40, MaxTime: 60})
	if err != nil {
		t.Fatal(err)
	}
	r90, err := eng.Isochrone(IsochroneRequest{Latitude: 23.10, Longitude: 113.40, MaxTime: 90})
	if err != nil {
		t.Fatal(err)
	}

	// 可达站点:60 分钟档只有 A,90 分钟档 A+B
	if r60.ReachableStations != 1 {
		t.Fatalf("60 分钟档可达站点应为 1,实际 %d", r60.ReachableStations)
	}
	if r90.ReachableStations != 2 {
		t.Fatalf("90 分钟档可达站点应为 2,实际 %d", r90.ReachableStations)
	}

	// 网格:90 分钟档应包含第 4 级(60~90 分钟)网格,且总数多于 60 分钟档
	if r90.GridCells <= r60.GridCells {
		t.Fatalf("90 分钟档网格数(%d)应多于 60 分钟档(%d)", r90.GridCells, r60.GridCells)
	}
	hasLevel4 := false
	for _, f := range r90.GeoJSON.Features {
		if lv := f.Properties["level"].(int); lv == 4 {
			hasLevel4 = true
		}
	}
	if !hasLevel4 {
		t.Fatal("90 分钟档应包含第 4 级(60~90 分钟)网格")
	}
	// 60 分钟档不应有第 4 级
	for _, f := range r60.GeoJSON.Features {
		if lv := f.Properties["level"].(int); lv > 3 {
			t.Fatalf("60 分钟档不应有第 4 级网格,发现 level=%d", lv)
		}
	}
}
