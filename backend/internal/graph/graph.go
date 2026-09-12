// Package graph 实现公共交通网络图模型。
// 节点为公交/地铁站点,边为站点间的连接(bus/metro/walk),
// 边权为按平均速度折算的估计运行时间(分钟)。
package graph

import (
	"fmt"
	"math"
)

// SpeedParams 时间权重模型参数(全部可配置,不写死在算法内)。
type SpeedParams struct {
	WalkingSpeedKmh    float64 // 步行速度 km/h
	BusSpeedKmh        float64 // 公交平均速度 km/h
	MetroSpeedKmh      float64 // 地铁平均速度 km/h
	TransferPenaltyMin float64 // 换乘/上车额外时间(分钟)
	WalkLinkMaxM       float64 // 站点步行接驳距离上限(米);0 = 关闭
}

// Node 交通站点节点。
type Node struct {
	ID   int64   `json:"id"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Type string  `json:"type"` // bus | metro
	City string  `json:"city"`
}

// Edge 站点间连接。数据格式与磁盘 JSON 一致,保持简单可读。
type Edge struct {
	From      int64   `json:"from"`
	To        int64   `json:"to"`
	DistanceM float64 `json:"distance_m"` // 米
	Mode      string  `json:"mode"`       // bus | metro | walk
}

// Arc 加权邻接项。
type Arc struct {
	To      int64
	TimeMin float64 // 估计时间(分钟)
	Mode    string
	DistM   float64
}

// Graph 邻接表图。
type Graph struct {
	Nodes map[int64]*Node
	adj   map[int64][]Arc
	arcs  int // 去重后的有向弧数
}

// EdgeTime 按交通方式把距离折算为估计时间(分钟)。
// walk 边额外计入换乘惩罚:步行边只出现在不同站点接驳处,
// 经过它意味着一次换乘/上车,等待时间在此建模(简化模型,见 docs/algorithm.md)。
func EdgeTime(e Edge, p SpeedParams) (float64, error) {
	switch e.Mode {
	case "bus":
		return e.DistanceM / (p.BusSpeedKmh * 1000 / 60), nil
	case "metro":
		return e.DistanceM / (p.MetroSpeedKmh * 1000 / 60), nil
	case "walk":
		return e.DistanceM/(p.WalkingSpeedKmh*1000/60) + p.TransferPenaltyMin, nil
	default:
		return 0, fmt.Errorf("未知边类型 %q (from=%d to=%d)", e.Mode, e.From, e.To)
	}
}

// Build 由节点和边构建无向图。
// CPTOND 的 segment 按线路方向存储;可达性分析中同一站间连接视为双向。
// 平行边(多线路共用同一站对)保留时间最短的一条。
func Build(nodes []Node, edges []Edge, p SpeedParams) (*Graph, error) {
	g := &Graph{
		Nodes: make(map[int64]*Node, len(nodes)),
		adj:   make(map[int64][]Arc, len(nodes)),
	}
	for i := range nodes {
		n := nodes[i]
		if _, dup := g.Nodes[n.ID]; dup {
			return nil, fmt.Errorf("重复站点 ID %d (%s)", n.ID, n.Name)
		}
		g.Nodes[n.ID] = &n
	}

	type key struct{ from, to int64 }
	best := make(map[key]Arc)
	addEdge := func(e Edge) error {
		if _, ok := g.Nodes[e.From]; !ok {
			return fmt.Errorf("边引用了不存在的节点 %d", e.From)
		}
		if _, ok := g.Nodes[e.To]; !ok {
			return fmt.Errorf("边引用了不存在的节点 %d", e.To)
		}
		if e.From == e.To {
			return nil
		}
		t, err := EdgeTime(e, p)
		if err != nil {
			return err
		}
		for _, dir := range [2]Edge{{e.From, e.To, e.DistanceM, e.Mode}, {e.To, e.From, e.DistanceM, e.Mode}} {
			k := key{dir.From, dir.To}
			a := Arc{To: dir.To, TimeMin: t, Mode: dir.Mode, DistM: dir.DistanceM}
			if old, ok := best[k]; !ok || a.TimeMin < old.TimeMin {
				best[k] = a
			}
		}
		return nil
	}
	for _, e := range edges {
		if err := addEdge(e); err != nil {
			return nil, err
		}
	}
	// 站点步行接驳边:近距离(≤ WalkLinkMaxM)站点对补 walk 边。
	// 目的:CPTOND 同一物理站点可能存在多个 ID(如 "小北(停东行)"/"小北1"、
	// 公交与地铁的同名站),且公交网与地铁网间没有数据边;无接驳边会导致
	// 图碎片化、换乘不可达。换乘惩罚(EdgeTime walk +5min)保证接驳边
	// 永远不比乘车快,只在换乘/同站接驳时被使用。
	if p.WalkLinkMaxM > 0 {
		for _, e := range WalkLinkEdges(nodes, p.WalkLinkMaxM) {
			if err := addEdge(e); err != nil {
				return nil, err
			}
		}
	}
	for k, a := range best {
		g.adj[k.from] = append(g.adj[k.from], a)
		g.arcs++
	}
	return g, nil
}

// havDeg 球面距离(米),供步行接驳边计算;graph 包内自包含,不依赖 geo 包。
func havDeg(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 6371000 * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// WalkLinkEdges 生成近距离站点对的 walk 接驳边(每对一条,无向边即两个方向)。
// 空间哈希分桶查找近邻,桶边长 = maxDist。
func WalkLinkEdges(nodes []Node, maxDist float64) []Edge {
	type cellKey struct{ r, c int64 }
	latDeg := maxDist / 111320
	// 经度按保守收缩取桶(只会多查桶,不会漏)
	lonDeg := maxDist / (111320 * 0.7)
	cells := make(map[cellKey][]int64)
	for i, n := range nodes {
		r := int64(math.Floor((n.Lat + 90) / latDeg))
		c := int64(math.Floor((n.Lon + 180) / lonDeg))
		cells[cellKey{r, c}] = append(cells[cellKey{r, c}], int64(i))
	}
	var out []Edge
	seen := map[[2]int64]bool{}
	for i, n := range nodes {
		r := int64(math.Floor((n.Lat + 90) / latDeg))
		c := int64(math.Floor((n.Lon + 180) / lonDeg))
		for dr := int64(-1); dr <= 1; dr++ {
			for dc := int64(-1); dc <= 1; dc++ {
				for _, j := range cells[cellKey{r + dr, c + dc}] {
					if int64(i) >= j {
						continue // 只取每对一次
					}
					m := nodes[j]
					d := havDeg(n.Lat, n.Lon, m.Lat, m.Lon)
					if d > maxDist {
						continue
					}
					pair := [2]int64{n.ID, m.ID}
					if seen[pair] {
						continue
					}
					seen[pair] = true
					out = append(out, Edge{From: n.ID, To: m.ID, DistanceM: d, Mode: "walk"})
				}
			}
		}
	}
	return out
}

// Neighbors 返回节点的邻接弧。
func (g *Graph) Neighbors(id int64) []Arc { return g.adj[id] }

// NodeCount / ArcCount 统计信息。
func (g *Graph) NodeCount() int { return len(g.Nodes) }
func (g *Graph) ArcCount() int  { return g.arcs }
