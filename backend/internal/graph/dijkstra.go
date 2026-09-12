package graph

// MultiSourceDijkstra 多源最短路径。
// sources 为各起点的初始时间(分钟)——即"用户地点 → 附近站点"的步行接入成本。
// 返回每个节点的最短估计到达时间;maxTime 以外的节点不再扩展(提前终止)。
// 实现为懒删除 Dijkstra:出堆时若已不是当前最优则跳过。
func (g *Graph) MultiSourceDijkstra(sources map[int64]float64, maxTime float64) map[int64]float64 {
	dist := make(map[int64]float64, len(g.Nodes))
	pq := &priorityQueue{}
	for id, d := range sources {
		if cur, ok := dist[id]; !ok || d < cur {
			dist[id] = d
			pq.push(&pqItem{node: id, dist: d})
		}
	}
	for pq.Len() > 0 {
		it := pq.pop()
		if it.dist > dist[it.node] {
			continue // 过期堆项
		}
		if it.dist > maxTime {
			continue // 超出时间预算,不再扩展
		}
		for _, a := range g.adj[it.node] {
			nd := it.dist + a.TimeMin
			if nd > maxTime {
				continue
			}
			if cur, ok := dist[a.To]; !ok || nd < cur {
				dist[a.To] = nd
				pq.push(&pqItem{node: a.To, dist: nd})
			}
		}
	}
	// 只保留 maxTime 内可达的节点
	for id, d := range dist {
		if d > maxTime {
			delete(dist, id)
		}
	}
	return dist
}
