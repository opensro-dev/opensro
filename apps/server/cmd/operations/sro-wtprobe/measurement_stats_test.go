/*
===========================================================================

measurement_stats_test.go - reproducible probe statistics without a session

All times are offsets from one fixed epoch. Losses, duplicate replies and
receipt order are independent of RTT sorting, and impossible recorder data
must fail instead of becoming a plausible latency result.

===========================================================================
*/

package main

import (
	"reflect"
	"testing"
	"time"
)

const measurementTestEpochSeconds = 1_700_000_000

/*
================
receivedProbeSample
================
*/
func receivedProbeSample(start time.Time, sequence uint64, sent, received time.Duration) probeSample {
	return probeSample{Sequence: sequence, SentAt: start.Add(sent), ReceivedAt: start.Add(received), Written: true, Responses: 1}
}

/*
================
TestSummarizeSamplesAccountsForAPartialRun

A registered failed write and an unregistered slot are both not sent.
Only a successfully written request can be missing; duplicate responses
never become extra RTT samples or extra receipt gaps.
================
*/
func TestSummarizeSamplesAccountsForAPartialRun(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	samples := []probeSample{
		receivedProbeSample(start, 1, 0, 100*time.Millisecond),
		{Sequence: 2, SentAt: start.Add(20 * time.Millisecond), Written: true},
		receivedProbeSample(start, 3, 40*time.Millisecond, 90*time.Millisecond),
		{Sequence: 4, SentAt: start.Add(60 * time.Millisecond)},
		receivedProbeSample(start, 5, 80*time.Millisecond, 500*time.Millisecond),
	}
	samples[2].Responses = 3
	before := append([]probeSample(nil), samples...)
	got, err := summarizeSamples(start, 6, samples)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requested != 6 || got.Registered != 5 || got.Sent != 4 || got.Received != 3 || got.Missing != 1 || got.NotSent != 2 ||
		got.DuplicateReplies != 2 || got.OutOfOrder != 1 {
		t.Fatalf("partial accounting: %+v", got)
	}
	if got.RTTP50 != 100*time.Millisecond || got.RTTP95 != 420*time.Millisecond || got.RTTP99 != 420*time.Millisecond ||
		got.RTTMax != 420*time.Millisecond {
		t.Fatalf("partial RTTs: p50=%v p95=%v p99=%v max=%v", got.RTTP50, got.RTTP95, got.RTTP99, got.RTTMax)
	}
	if got.JitterPairs != 2 || got.OneWayJitterMeanNS != float64(92500*time.Microsecond) ||
		got.ReceiptStalls != 1 || got.WorstReceiptGap != 400*time.Millisecond {
		t.Fatalf("partial jitter/gaps: %+v", got)
	}
	if len(got.Samples) != 5 || got.Samples[0].Sequence != 1 || got.Samples[2].Sequence != 3 ||
		got.Samples[0].SentOffset == nil || *got.Samples[0].SentOffset != 0 ||
		got.Samples[0].ReceivedOffset == nil || *got.Samples[0].ReceivedOffset != 100*time.Millisecond ||
		got.Samples[0].RTT == nil || *got.Samples[0].RTT != 100*time.Millisecond || got.Samples[2].Responses != 3 {
		t.Fatalf("raw evidence lost registration order or timing: %+v", got.Samples)
	}
	if got.Samples[1].ReceivedOffset != nil || got.Samples[1].RTT != nil || got.Samples[3].Written ||
		got.Samples[3].SentOffset == nil || *got.Samples[3].SentOffset != 60*time.Millisecond || got.Samples[3].RTT != nil {
		t.Fatal("a missing response or failed write acquired a synthetic RTT")
	}
	*got.Samples[0].SentOffset = time.Hour
	if !reflect.DeepEqual(samples, before) || *got.Samples[2].SentOffset != 40*time.Millisecond {
		t.Fatal("summarizing or changing raw output mutated the input or another sample")
	}
}

/*
================
TestSummarizeSamplesUsesNearestRankRTT

Nearest rank does not interpolate an even sample set. A larger set also
separates p95 and p99, so taking the maximum for both cannot pass.
================
*/
func TestSummarizeSamplesUsesNearestRankRTT(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	hundred := make([]time.Duration, 100)
	for index := range hundred {
		hundred[index] = time.Duration(100-index) * time.Millisecond
	}
	for _, tc := range []struct {
		name               string
		rtts               []time.Duration
		p50, p95, p99, max time.Duration
	}{
		{"single", []time.Duration{7 * time.Millisecond}, 7 * time.Millisecond, 7 * time.Millisecond, 7 * time.Millisecond, 7 * time.Millisecond},
		{"even", []time.Duration{30 * time.Millisecond, 10 * time.Millisecond}, 10 * time.Millisecond, 30 * time.Millisecond, 30 * time.Millisecond, 30 * time.Millisecond},
		{"odd", []time.Duration{9 * time.Millisecond, time.Millisecond, 5 * time.Millisecond}, 5 * time.Millisecond, 9 * time.Millisecond, 9 * time.Millisecond, 9 * time.Millisecond},
		{"ties", []time.Duration{5, 5, 5, 5}, 5, 5, 5, 5},
		{"hundred", hundred, 50 * time.Millisecond, 95 * time.Millisecond, 99 * time.Millisecond, 100 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([]probeSample, len(tc.rtts))
			for index, rtt := range tc.rtts {
				samples[index] = receivedProbeSample(start, uint64(index), 0, rtt)
			}
			before := append([]probeSample(nil), samples...)
			got, err := summarizeSamples(start, len(samples), samples)
			if err != nil {
				t.Fatal(err)
			}
			if got.RTTP50 != tc.p50 || got.RTTP95 != tc.p95 || got.RTTP99 != tc.p99 || got.RTTMax != tc.max || got.Received != len(samples) {
				t.Fatalf("RTTs p50=%v p95=%v p99=%v max=%v count=%d", got.RTTP50, got.RTTP95, got.RTTP99, got.RTTMax, got.Received)
			}
			if !reflect.DeepEqual(samples, before) {
				t.Fatal("RTT sorting mutated the recorder samples")
			}
		})
	}
}

/*
================
TestSummarizeSamplesOrdersUniqueReceipts

Three arrives before one before two. Both later replies are out of order,
including two even though its sequence exceeds the immediately prior one.
================
*/
func TestSummarizeSamplesOrdersUniqueReceipts(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	samples := []probeSample{
		receivedProbeSample(start, 2, 10*time.Millisecond, 300*time.Millisecond),
		receivedProbeSample(start, 1, 0, 200*time.Millisecond),
		receivedProbeSample(start, 3, 20*time.Millisecond, 100*time.Millisecond),
	}
	samples[0].Responses, samples[1].Responses = 4, 2
	got, err := summarizeSamples(start, 3, samples)
	if err != nil {
		t.Fatal(err)
	}
	if got.OutOfOrder != 2 || got.DuplicateReplies != 4 || got.Received != 3 || got.JitterPairs != 2 ||
		got.OneWayJitterMeanNS != float64(52500*time.Microsecond) || got.ReceiptStalls != 0 || got.WorstReceiptGap != 100*time.Millisecond {
		t.Fatalf("receipt-order statistics: %+v", got)
	}
}

/*
================
TestSummarizeSamplesRetainsFractionalOneWayJitter

The two successive RTT changes are 1 ns and 2 ns. Their one-way-equivalent
mean is 0.75 ns, not a truncated zero or an RTT mean of 1.5 ns.
================
*/
func TestSummarizeSamplesRetainsFractionalOneWayJitter(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	samples := []probeSample{
		receivedProbeSample(start, 1, 0, time.Nanosecond),
		receivedProbeSample(start, 2, 10*time.Nanosecond, 12*time.Nanosecond),
		receivedProbeSample(start, 3, 20*time.Nanosecond, 24*time.Nanosecond),
	}
	samples[1].Responses = 32
	got, err := summarizeSamples(start, 3, samples)
	if err != nil {
		t.Fatal(err)
	}
	if got.OneWayJitterMeanNS != 0.75 || got.JitterPairs != 2 || got.Received != 3 || got.DuplicateReplies != 31 {
		t.Fatalf("fractional jitter: %+v", got)
	}
}

/*
================
TestSummarizeSamplesLeavesEmptyAndMissingRunsHonest
================
*/
func TestSummarizeSamplesLeavesEmptyAndMissingRunsHonest(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	for _, requested := range []int{0, 5, int(^uint(0) >> 1)} {
		got, err := summarizeSamples(start, requested, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Requested != requested || got.NotSent != requested || got.Registered != 0 || got.Sent != 0 || got.Received != 0 ||
			got.Missing != 0 || got.RTTMax != 0 || got.ReceiptStalls != 0 || len(got.Samples) != 0 {
			t.Fatalf("empty run: %+v", got)
		}
	}
	got, err := summarizeSamples(start, 3, []probeSample{{Sequence: 1, SentAt: start, Written: true}, {Sequence: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Requested != 3 || got.Registered != 2 || got.Sent != 1 || got.Received != 0 || got.Missing != 1 || got.NotSent != 2 ||
		got.RTTP50 != 0 || got.RTTP95 != 0 || got.RTTP99 != 0 || got.RTTMax != 0 || got.JitterPairs != 0 ||
		got.OneWayJitterMeanNS != 0 || got.ReceiptStalls != 0 || got.WorstReceiptGap != 0 ||
		got.Samples[0].SentOffset == nil || *got.Samples[0].SentOffset != 0 || got.Samples[1].SentOffset != nil {
		t.Fatalf("no-receipt partial run: %+v", got)
	}
}

/*
================
TestSummarizeSamplesReceiptStallBoundary

The first gap starts at measurement start. Exactly 250 ms is not a stall;
one nanosecond more is. Tied receipts add neither an inversion nor a gap.
================
*/
func TestSummarizeSamplesReceiptStallBoundary(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	for _, tc := range []struct {
		name     string
		receipts []time.Duration
		stalls   int
		worst    time.Duration
	}{
		{"first-at-boundary", []time.Duration{250 * time.Millisecond}, 0, 250 * time.Millisecond},
		{"first-over-boundary", []time.Duration{250*time.Millisecond + time.Nanosecond}, 1, 250*time.Millisecond + time.Nanosecond},
		{"next-at-boundary", []time.Duration{10 * time.Millisecond, 260 * time.Millisecond}, 0, 250 * time.Millisecond},
		{"next-over-boundary", []time.Duration{10 * time.Millisecond, 260*time.Millisecond + time.Nanosecond}, 1, 250*time.Millisecond + time.Nanosecond},
		{"both-stall", []time.Duration{300 * time.Millisecond, 600 * time.Millisecond}, 2, 300 * time.Millisecond},
		{"receipt-tie", []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}, 0, 10 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([]probeSample, len(tc.receipts))
			for index, at := range tc.receipts {
				// Reverse registration order to exercise the receipt tie rule.
				samples[index] = receivedProbeSample(start, uint64(len(samples)-index), 0, at)
			}
			got, err := summarizeSamples(start, len(samples), samples)
			if err != nil {
				t.Fatal(err)
			}
			if got.ReceiptStalls != tc.stalls || got.WorstReceiptGap != tc.worst || tc.name == "receipt-tie" && got.OutOfOrder != 0 {
				t.Fatalf("receipt gaps: stalls=%d worst=%v reordered=%d", got.ReceiptStalls, got.WorstReceiptGap, got.OutOfOrder)
			}
		})
	}
}

/*
================
TestSummarizeSamplesRejectsMalformedRecorderData

A failure returns no partial numerical summary, and does not repair or
mutate the input. This includes saturated time.Sub and duplicate counters.
================
*/
func TestSummarizeSamplesRejectsMalformedRecorderData(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	good := receivedProbeSample(start, 1, 0, time.Millisecond)
	for _, tc := range []struct {
		name      string
		start     time.Time
		requested int
		samples   []probeSample
	}{
		{"negative-requested", start, -1, nil},
		{"more-registered-than-requested", start, 0, []probeSample{good}},
		{"unset-start", time.Time{}, 1, []probeSample{good}},
		{"duplicate-sequence", start, 2, []probeSample{good, good}},
		{"written-without-time", start, 1, []probeSample{{Sequence: 1, Written: true}}},
		{"send-before-start", start, 1, []probeSample{{Sequence: 1, SentAt: start.Add(-time.Nanosecond)}}},
		{"receipt-before-start", start, 1, []probeSample{receivedProbeSample(start, 1, 0, -time.Nanosecond)}},
		{"receipt-before-send", start, 1, []probeSample{receivedProbeSample(start, 1, time.Millisecond, 0)}},
		{"time-without-response", start, 1, []probeSample{{Sequence: 1, SentAt: start, Written: true, ReceivedAt: start}}},
		{"response-without-time", start, 1, []probeSample{{Sequence: 1, SentAt: start, Written: true, Responses: 1}}},
		{"response-without-write", start, 1, []probeSample{{Sequence: 1, SentAt: start, ReceivedAt: start, Responses: 1}}},
		{"send-duration-overflow", start, 1, []probeSample{{Sequence: 1, SentAt: start.AddDate(1000, 0, 0), Written: true}}},
		{"receipt-duration-overflow", start, 1, []probeSample{{Sequence: 1, SentAt: start, ReceivedAt: start.AddDate(1000, 0, 0), Written: true, Responses: 1}}},
		{"duplicate-count-overflow", start, 2, []probeSample{
			{Sequence: 1, SentAt: start, ReceivedAt: start, Written: true, Responses: ^uint64(0)},
			{Sequence: 2, SentAt: start, ReceivedAt: start, Written: true, Responses: 3},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]probeSample(nil), tc.samples...)
			got, err := summarizeSamples(tc.start, tc.requested, tc.samples)
			if err == nil || !reflect.DeepEqual(got, sampleSummary{}) {
				t.Fatalf("invalid recorder data yielded summary=%+v error=%v", got, err)
			}
			if !reflect.DeepEqual(tc.samples, before) {
				t.Fatal("invalid recorder input was repaired in place")
			}
		})
	}
}

/*
================
TestSummarizeSamplesAllowsZeroRTTAndLargeResponseCounts
================
*/
func TestSummarizeSamplesAllowsZeroRTTAndLargeResponseCounts(t *testing.T) {
	start := time.Unix(measurementTestEpochSeconds, 0)
	sample := receivedProbeSample(start, 0, 0, 0)
	sample.Responses = ^uint64(0)
	got, err := summarizeSamples(start, 1, []probeSample{sample})
	if err != nil {
		t.Fatal(err)
	}
	if got.Received != 1 || got.DuplicateReplies != ^uint64(0)-1 || got.RTTMax != 0 ||
		got.Samples[0].RTT == nil || *got.Samples[0].RTT != 0 || got.JitterPairs != 0 {
		t.Fatalf("zero RTT or duplicate accounting was clamped: %+v", got)
	}
}
