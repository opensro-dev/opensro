/*
===========================================================================

shape.go - one direction's impairment: delay, jitter and loss draws

One shaper owns the configured impairment of a single direction (up is
client to server, down is server to client) of one sro-netshape relay
leg. Each packet or TCP chunk (one read) draws once: the loss draw
first, then the delay - the configured one-way delay plus a uniform
jitter in [-jitter, +jitter], floored at zero, matching how netem's
delay/jitter pair reads. A seeded shaper reproduces its draw sequence
exactly, so a measurement run can be repeated draw for draw. Each leg
mints its own shapers (and each UDP session its own down shaper) from
the run's seed, so the legs never perturb each other's sequences.

The two legs spend a failed loss draw differently: UDP drops the
datagram, while a TCP chunk cannot be dropped without corrupting the
stream, so the TCP pump holds it for the configured retransmission
penalty instead (see pass).

===========================================================================
*/
package main

import (
	"container/heap"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

/*
================
shapeConfig

One direction's configured impairment. LossPct is a percentage in
[0, 100]. LossHold is the TCP-only spend of a failed loss draw; the UDP
leg ignores it and drops.
================
*/
type shapeConfig struct {
	Delay    time.Duration
	Jitter   time.Duration
	LossPct  float64
	LossHold time.Duration
}

/*
================
shaper

The draw state of one direction. Safe for concurrent use: a leg's down
direction is drawn from by every session's reader goroutine.
================
*/
type shaper struct {
	cfg shapeConfig
	mu  sync.Mutex
	rnd *rand.Rand
}

/*
================
newShaper
================
*/
func newShaper(cfg shapeConfig, seed uint64) *shaper {
	return &shaper{cfg: cfg, rnd: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

/*
================
pass

Draws once for one packet or chunk: admitted is the loss draw's verdict,
and delay is the one-way delay to apply. A caller that cannot drop (the
TCP pump) adds cfg.LossHold to the delay when admitted is false.
================
*/
func (s *shaper) pass() (admitted bool, delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.LossPct > 0 && s.rnd.Float64()*100 < s.cfg.LossPct {
		return false, 0
	}
	delay = s.cfg.Delay
	if s.cfg.Jitter > 0 {
		delay += time.Duration((s.rnd.Float64()*2 - 1) * float64(s.cfg.Jitter))
	}
	if delay < 0 {
		delay = 0
	}
	return true, delay
}

//============================================================================

/*
================
directionStats

One direction's counters, printed on exit and on the stats interval so a
run can prove the impairment it applied. Held counts TCP chunks whose
loss draw failed and which were held for LossHold instead of dropped.
Failed counts admitted chunks/datagrams that never shipped - a write to
a dead socket, a chunk queued behind one, a datagram still queued when
its queue closed - so received always adds up against the other four.
================
*/
type directionStats struct {
	received  atomic.Uint64
	forwarded atomic.Uint64
	dropped   atomic.Uint64
	held      atomic.Uint64
	failed    atomic.Uint64
	bytes     atomic.Uint64
}

//============================================================================

/*
================
queueItem

One scheduled emission: the payload leaves at at (submission order breaks
ties), delivered by emit. emit returns its error so the caller keeps its
own forwarded/bytes counters honest.
================
*/
type queueItem struct {
	at   time.Time
	seq  uint64
	data []byte
	emit func([]byte) error
}

// itemHeap is a container/heap of queueItem by (at, seq).
type itemHeap []*queueItem

func (h itemHeap) Len() int { return len(h) }

func (h itemHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].seq < h[j].seq
	}
	return h[i].at.Before(h[j].at)
}

func (h itemHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *itemHeap) Push(x any) { *h = append(*h, x.(*queueItem)) }

func (h *itemHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return item
}

/*
================
delayQueue

Emits payloads at their scheduled times, earliest first. One goroutine
(run) owns emission; submit is safe from any goroutine. Used by the UDP
leg, whose datagrams may pass each other when jitter makes a later
datagram due first - as on a real link. The TCP leg pipelines through
its own channel instead (pump in main.go): stream chunks must leave in
read order, so their due times chain rather than pass each other.
================
*/
type delayQueue struct {
	mu     sync.Mutex
	items  itemHeap
	seq    uint64
	closed bool
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once
}

/*
================
newDelayQueue
================
*/
func newDelayQueue() *delayQueue {
	return &delayQueue{wake: make(chan struct{}, 1), done: make(chan struct{})}
}

/*
================
submit

Schedules data for emission at at. The payload is copied nowhere: the
caller hands ownership to the queue. Returns false once the queue is
closed; the caller then counts the payload as failed itself, so nothing
submitted is ever lost from the counters.
================
*/
func (q *delayQueue) submit(at time.Time, data []byte, emit func([]byte) error) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	q.seq++
	heap.Push(&q.items, &queueItem{at: at, seq: q.seq, data: data, emit: emit})
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return true
}

/*
================
run

The emission loop: sleeps until the earliest item is due, emits every
due item in (at, seq) order outside the lock, repeats. Returns when the
queue is closed.
================
*/
func (q *delayQueue) run() {
	for {
		q.mu.Lock()
		if len(q.items) == 0 {
			q.mu.Unlock()
			select {
			case <-q.wake:
				continue
			case <-q.done:
				return
			}
		}
		wait := time.Until(q.items[0].at)
		if wait > 0 {
			q.mu.Unlock()
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-q.wake:
				timer.Stop()
			case <-q.done:
				timer.Stop()
				return
			}
			continue
		}
		var due []*queueItem
		now := time.Now()
		for len(q.items) > 0 && !q.items[0].at.After(now) {
			due = append(due, heap.Pop(&q.items).(*queueItem))
		}
		q.mu.Unlock()
		for _, item := range due {
			_ = item.emit(item.data)
		}
	}
}

/*
================
close

Stops the emission loop and reports how many items were still queued;
the caller counts them as failed, so shutdown does not silently spend
datagrams the counters never saw leave.
================
*/
func (q *delayQueue) close() (discarded int) {
	q.once.Do(func() { close(q.done) })
	q.mu.Lock()
	discarded = len(q.items)
	q.items = nil
	q.closed = true
	q.mu.Unlock()
	return discarded
}
