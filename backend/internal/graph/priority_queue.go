package graph

import "container/heap"

// pqItem 优先队列元素。
type pqItem struct {
	node int64
	dist float64
	index int
}

// priorityQueue 基于 container/heap 的最小堆。
type priorityQueue []*pqItem

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].dist < pq[j].dist }
func (pq priorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i]; pq[i].index = i; pq[j].index = j }
func (pq *priorityQueue) Push(x any)         { it := x.(*pqItem); it.index = len(*pq); *pq = append(*pq, it) }
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return it
}

func (pq *priorityQueue) push(it *pqItem) { heap.Push(pq, it) }
func (pq *priorityQueue) pop() *pqItem    { return heap.Pop(pq).(*pqItem) }
