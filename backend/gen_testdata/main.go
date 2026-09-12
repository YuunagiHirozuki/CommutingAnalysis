//gen:run go run ./gen_testdata.go
// 生成 TEST DATA(合成小型测试网络,绝非真实广州数据)。
// 以广州中心为原点铺设 10×10 公交网格 + 2 条十字地铁线,
// 用于在后端接入真实数据前端到端验证 API 与算法链路。
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"backend/internal/graph"
)

func main() {
	out := "data/test"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	_ = os.MkdirAll(out, 0o755)

	const lat0, lon0 = 23.1291, 113.2644 // 广州中心(北京路一带)
	const step = 0.006                   // ≈666m 站距
	var nodes []graph.Node
	var edges []graph.Edge

	id := int64(0)
	next := func() int64 { id++; return id }

	// 公交网格:每格 2 站(左右交错),横纵各 12 行/列
	gridID := make([][]int64, 12)
	for r := 0; r < 12; r++ {
		gridID[r] = make([]int64, 12)
		for c := 0; c < 12; c++ {
			gridID[r][c] = next()
			nodes = append(nodes, graph.Node{
				ID: gridID[r][c],
				Name: fmt.Sprintf("测试公交站 r%dc%d", r, c),
				Lat: lat0 + float64(r)*step,
				Lon: lon0 + float64(c)*step,
				Type: "bus", City: "TEST",
			})
		}
	}
	edgeM := step * 111320 * 0.94 // 经度方向站距(米,广州纬度)
	edgeLatM := step * 111320
	for r := 0; r < 12; r++ {
		for c := 0; c < 12; c++ {
			if c+1 < 12 {
				edges = append(edges, graph.Edge{From: gridID[r][c], To: gridID[r][c+1], DistanceM: edgeM, Mode: "bus"})
			}
			if r+1 < 12 {
				edges = append(edges, graph.Edge{From: gridID[r][c], To: gridID[r+1][c], DistanceM: edgeLatM, Mode: "bus"})
			}
		}
	}

	// 地铁 1 号线:东西向贯穿第 6 行;地铁 2 号线:南北向贯穿第 6 列;站距 2 格
	metro1 := make([]int64, 6)
	for i := range metro1 {
		metro1[i] = next()
		nodes = append(nodes, graph.Node{
			ID: metro1[i], Name: fmt.Sprintf("测试地铁站1-%d", i+1),
			Lat: lat0 + 6*step, Lon: lon0 + float64(2*i)*step,
			Type: "metro", City: "TEST",
		})
		if i > 0 {
			edges = append(edges, graph.Edge{From: metro1[i-1], To: metro1[i], DistanceM: edgeM * 2, Mode: "metro"})
		}
	}
	metro2 := make([]int64, 6)
	for i := range metro2 {
		metro2[i] = next()
		nodes = append(nodes, graph.Node{
			ID: metro2[i], Name: fmt.Sprintf("测试地铁站2-%d", i+1),
			Lat: lat0 + float64(2*i)*step, Lon: lon0 + 6*step,
			Type: "metro", City: "TEST",
		})
		if i > 0 {
			edges = append(edges, graph.Edge{From: metro2[i-1], To: metro2[i], DistanceM: edgeLatM * 2, Mode: "metro"})
		}
	}
	// 换乘站:地铁与网格公交的步行接驳(两线交点 + 端点)
	connect := func(mID, bID int64) {
		edges = append(edges, graph.Edge{From: mID, To: bID, DistanceM: 150, Mode: "walk"})
	}
	connect(metro1[0], gridID[6][0]); connect(metro1[1], gridID[6][2]); connect(metro1[2], gridID[6][4])
	connect(metro1[3], gridID[6][6]); connect(metro1[4], gridID[6][8]); connect(metro1[5], gridID[6][10])
	connect(metro2[0], gridID[0][6]); connect(metro2[1], gridID[2][6]); connect(metro2[2], gridID[4][6])
	connect(metro2[3], gridID[6][6]); connect(metro2[4], gridID[8][6]); connect(metro2[5], gridID[10][6])

	write(out+"/nodes.json", nodes)
	write(out+"/edges.json", edges)
	fmt.Printf("TEST DATA 已生成: %s (%d 节点, %d 有向边)\n", out, len(nodes), len(edges))
}

func write(path string, v any) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", " ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
}
