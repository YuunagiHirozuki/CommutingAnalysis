// 网络连通性诊断工具(AGENTS.md Phase 3 验证项)。
// 检查:连通分量、地标站点间最短时间、跨河(指定纬度带)连接边数量。
// 用法: go run ./cmd/netcheck -config config/config.json [-city guangzhou]
package main

import (
	"flag"
	"fmt"
	"math"
	"sort"

	"backend/internal/config"
	"backend/internal/service"
)

func main() {
	cfgPath := flag.String("config", "config/config.json", "配置文件路径")
	city := flag.String("city", "", "城市代码(默认取第一个)")
	bandLat := flag.Float64("band", 23.108, "跨河检查纬度带(带±0.004 内视为河道)")
	bandLonMin := flag.Float64("lonmin", 113.24, "河道检查经度窗口")
	bandLonMax := flag.Float64("lonmax", 113.36, "")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		panic(err)
	}
	code := *city
	if code == "" {
		code = cfg.Cities[0].Code
	}
	var cc config.CityConfig
	for _, c := range cfg.Cities {
		if c.Code == code {
			cc = c
		}
	}
	eng, err := service.LoadCity(cfg, cc)
	if err != nil {
		panic(err)
	}
	g := eng.Graph
	fmt.Printf("图: %d 节点 / %d 弧\n\n", g.NodeCount(), g.ArcCount())

	// 1. 连通分量(无向 BFS)
	comp := map[int64]int{}
	var sizes []int
	id := 0
	for start := range g.Nodes {
		if comp[start] != 0 {
			continue
		}
		id++
		queue := []int64{start}
		comp[start] = id
		n := 0
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			n++
			for _, a := range g.Neighbors(cur) {
				if comp[a.To] == 0 {
					comp[a.To] = id
					queue = append(queue, a.To)
				}
			}
		}
		sizes = append(sizes, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
	fmt.Printf("连通分量: %d 个,最大 10 个分量规模: %v\n", id, firstN(sizes, 10))
	if id > 1 {
		fmt.Println("  ⚠ 存在孤立子图(规模 <100 的分量数量):", countLess(sizes, 100))
	}

	// 1b. 非主分量样本站点 + 与主分量最近的站点对(定位断链位置)
	if id > 1 {
		compSizes := map[int]int{}
		for _, c := range comp {
			compSizes[c]++
		}
		main := 0
		for c, n := range compSizes {
			if n == sizes[0] {
				main = c
			}
		}
		type pair struct {
			d float64
			a string
			b string
		}
		var nearest []pair
		shown := map[int]int{}
		for _, n := range g.Nodes {
			if comp[n.ID] == main || shown[comp[n.ID]] >= 3 {
				continue
			}
			for _, m := range g.Nodes {
				if comp[m.ID] != main {
					continue
				}
				d := hav(n.Lat, n.Lon, m.Lat, m.Lon)
				nearest = append(nearest, pair{d, fmt.Sprintf("[%d]%s", comp[n.ID], n.Name), fmt.Sprintf("[主]%s", m.Name)})
			}
		}
		sort.Slice(nearest, func(i, j int) bool { return nearest[i].d < nearest[j].d })
		fmt.Println("\n非主分量与主分量最近的站点对(前 12):")
		for _, p := range nearest[:min(12, len(nearest))] {
			fmt.Printf("  %6.0fm  %s ↔ %s\n", p.d, p.a, p.b)
		}
	}

	// 2. 地标站点间最短时间(检验跨河可达性)
	landmarks := []string{"广州塔", "财厅", "海珠广场", "江南西", "珠江新城", "客村"}
	var lmIDs []int64
	var lmNames []string
	for _, n := range g.Nodes {
		for i, key := range landmarks {
			if n.Name == key {
				lmIDs = append(lmIDs, n.ID)
				lmNames = append(lmNames, key)
				_ = i
			}
		}
	}
	fmt.Println("\n地标间理论可达时间(分钟, 含默认速度参数):")
	fmt.Printf("  %-10s", "")
	for _, n := range lmNames {
		fmt.Printf("%-12s", n)
	}
	fmt.Println()
	for i, src := range lmIDs {
		fmt.Printf("  %-10s", lmNames[i])
		dist := g.MultiSourceDijkstra(map[int64]float64{src: 0}, 1e9)
		for j, dst := range lmIDs {
			if i == j {
				fmt.Printf("%-12s", "-")
				continue
			}
			if d, ok := dist[dst]; ok {
				fmt.Printf("%-12.1f", d)
			} else {
				fmt.Printf("%-12s", "不可达!")
			}
		}
		fmt.Println()
	}
	if len(lmIDs) < 2 {
		fmt.Println("  (地标未全部命中,实际命中:", lmNames, ")")
	}

	// 3. 跨河连接边:两端分别位于纬度带两侧、且位于经度窗口内(珠江核心段近似)
	cross := 0
	seen := map[[2]int64]bool{}
	for _, n := range g.Nodes {
		for _, a := range g.Neighbors(n.ID) {
			m := g.Nodes[a.To]
			k := [2]int64{n.ID, a.To}
			if k[0] > k[1] {
				k = [2]int64{a.To, n.ID}
			}
			if seen[k] {
				continue
			}
			north := n.Lat > *bandLat && m.Lat <= *bandLat
			south := n.Lat <= *bandLat && m.Lat > *bandLat
			inWin := midpoint(n.Lon, m.Lon) >= *bandLonMin && midpoint(n.Lon, m.Lon) <= *bandLonMax
			if (north || south) && inWin {
				seen[k] = true
				cross++
			}
		}
	}
	fmt.Printf("\n跨河检查(纬度带 %.3f,经度窗口 %.2f~%.2f): 连接两侧的无向边 %d 条\n",
		*bandLat, *bandLonMin, *bandLonMax, cross)
}

func midpoint(a, b float64) float64 { return (a + b) / 2 }
func hav(lat1, lon1, lat2, lon2 float64) float64 {
	rad := 3.141592653589793 / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 6371000 * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func firstN(s []int, n int) []int {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func countLess(s []int, v int) int {
	c := 0
	for _, x := range s {
		if x < v {
			c++
		}
	}
	return c
}
