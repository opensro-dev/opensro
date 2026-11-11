/*
===========================================================================

lock_timing_bench_test.go - what one timed store-lock acquisition costs (#3126)

Every store door runs under timedRWMutex, so its two clock reads per
uncontended acquisition (one before, one after) are paid per door on
the tick. The benchmarks price the read door's cycle (RLock/RUnlock)
and the write door's (Lock/Unlock) the way the tick pays them: alone,
and from every goroutine at once.
===========================================================================
*/

package store

import "testing"

var timedMutexSink int

/*
================
BenchmarkTimedRWMutexRead
================
*/
func BenchmarkTimedRWMutexRead(b *testing.B) {
	var m timedRWMutex
	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			m.RLock()
			timedMutexSink++
			m.RUnlock()
		}
	})
	b.Run("parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			sum := 0
			for pb.Next() {
				m.RLock()
				sum++
				m.RUnlock()
			}
			timedMutexSink = sum
		})
	})
}

/*
================
BenchmarkTimedRWMutexReadHeld

The parallel door held across a few dozen nanoseconds of local
work - the shape of a real store read, where the acquisition cost
is amortized instead of back-to-back CAS on an empty hold.
================
*/
func BenchmarkTimedRWMutexReadHeld(b *testing.B) {
	m := &timedRWMutex{}
	b.RunParallel(func(pb *testing.PB) {
		sum := 0
		for pb.Next() {
			m.RLock()
			for i := 0; i < 24; i++ {
				sum += i * i
			}
			m.RUnlock()
		}
		timedMutexSink = sum
	})
}

/*
================
BenchmarkTimedRWMutexWrite
================
*/
func BenchmarkTimedRWMutexWrite(b *testing.B) {
	var m timedRWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		timedMutexSink++
		m.Unlock()
	}
}
