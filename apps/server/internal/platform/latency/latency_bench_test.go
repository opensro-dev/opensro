/*
===========================================================================

latency_bench_test.go - the recording cost of the shared histograms

Recording sits on hot paths (every inbound frame, every store read) and
must stay allocation-free; bucket lookup is a linear scan, so a bound
added for slow samples must not cost the fast ones. The sub-benchmarks
pin both ends: a 2 ms frame-time record and a 180 ms record that walks
the full scan into the 100 to 250 ms band.

===========================================================================
*/

package latency

import "testing"

/*
================
BenchmarkLiveAdd
================
*/
func BenchmarkLiveAdd(b *testing.B) {
	b.Run("frame2ms", func(b *testing.B) {
		var h Live
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			h.Add(2_000_000)
		}
	})
	b.Run("band180ms", func(b *testing.B) {
		var h Live
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			h.Add(180_000_000)
		}
	})
}
