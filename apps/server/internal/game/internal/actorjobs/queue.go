// Package actorjobs projects the reference server's async actor job queue.
// This actor state queue is distinct from persistent database/timed skill jobs.
package actorjobs

import "container/list"

// Job callbacks retain native order. Release must dispose owned resources;
// the queue unlinks the node only after Release returns. Removing the current
// node or clearing this queue inside its callback is outside native's domain.
type Job interface {
	Advance(deltaSeconds float32) uint32
	Release()
	Flags() uint32
	Category() uint8
	Matches(kind, value uint32) uint32
}

// Queue is actor-owned. Its owner must serialize dispatch and mutation.
type Queue struct {
	jobs     list.List
	now      func() uint32
	lastTick uint32
}

func New(now func() uint32) *Queue {
	if now == nil {
		panic("missing actor job clock")
	}
	return &Queue{now: now, lastTick: now()}
}
func (q *Queue) Len() int    { return q.jobs.Len() }
func (q *Queue) ResetClock() { q.lastTick = q.now() }
func (q *Queue) Append(job Job) {
	if job == nil {
		panic("null actor job")
	}
	if q.jobs.Len() == 0 {
		q.ResetClock()
	}
	q.jobs.PushBack(job)
}
func (q *Queue) Advance() {
	if q.jobs.Len() == 0 {
		return
	}
	now := q.now()
	delta := float32(float64(now-q.lastTick) / 1000)
	q.lastTick = now
	for node := q.jobs.Front(); node != nil; {
		job := node.Value.(Job)
		keep := job.Advance(delta)
		if keep == 0 {
			job.Release()
		}
		// Read the live successor AFTER callbacks: a newly appended job can run now.
		next := node.Next()
		if keep == 0 {
			q.jobs.Remove(node)
		}
		node = next
	}
}
func (q *Queue) Clear() {
	for node := q.jobs.Front(); node != nil; node = node.Next() {
		node.Value.(Job).Release()
	}
	q.jobs.Init()
	q.lastTick = 0
}
func (q *Queue) RemoveFirstMatching(category uint8, kind, value uint32) bool {
	for node := q.jobs.Front(); node != nil; node = node.Next() {
		job := node.Value.(Job)
		if job.Flags() != 0 || job.Category() != category || job.Matches(kind, value) != 1 {
			continue
		}
		job.Release()
		q.jobs.Remove(node)
		return true
	}
	return false
}
