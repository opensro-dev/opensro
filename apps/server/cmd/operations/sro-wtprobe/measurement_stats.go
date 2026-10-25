/*
===========================================================================

measurement_stats.go - exact sample accounting for a probe measurement

The session records one sample before each write and retains the first
response time plus the total response count. This file owns only the pure
summary: it opens no connection and reads no clock. Offsets use time.Sub,
which preserves the caller's monotonic clock when present.

===========================================================================
*/

package main

import (
	"fmt"
	"sort"
	"time"
)

const receiptStallThreshold = 250 * time.Millisecond

/*
================
probeSample

Written records a successful write, not an attempted one. ReceivedAt is
the first response only; Responses also counts later duplicate replies.
An unwritten registered sample may have no send timestamp yet.
================
*/
type probeSample struct {
	Sequence   uint64
	SentAt     time.Time
	ReceivedAt time.Time
	Written    bool
	Responses  uint64
}

/*
================
sampleMeasurement

The raw evidence in registration order. Nil offsets mean the event never
happened, distinct from an event at offset zero. Durations encode as integer
nanoseconds; absolute wall-clock timestamps do not enter the statistics.
================
*/
type sampleMeasurement struct {
	Sequence       uint64         `json:"sequence"`
	Written        bool           `json:"written"`
	Responses      uint64         `json:"responses"`
	SentOffset     *time.Duration `json:"sent_offset_ns"`
	ReceivedOffset *time.Duration `json:"received_offset_ns"`
	RTT            *time.Duration `json:"rtt_ns"`
}

/*
================
sampleSummary

Missing counts successful writes without a response. NotSent includes
requested slots that were never registered. Received counts unique replies,
and is also the latency sample count: zero means no RTT percentile exists.
WorstReceiptGap includes gaps below the stall threshold. Jitter is a mean
in nanoseconds, kept fractional rather than truncating half-nanosecond pairs.
================
*/
type sampleSummary struct {
	Requested          int                 `json:"requested"`
	Registered         int                 `json:"registered"`
	Sent               int                 `json:"sent"`
	Received           int                 `json:"received"`
	Missing            int                 `json:"missing"`
	NotSent            int                 `json:"not_sent"`
	DuplicateReplies   uint64              `json:"duplicate_replies"`
	OutOfOrder         int                 `json:"out_of_order"`
	RTTP50             time.Duration       `json:"rtt_p50_ns"`
	RTTP95             time.Duration       `json:"rtt_p95_ns"`
	RTTP99             time.Duration       `json:"rtt_p99_ns"`
	RTTMax             time.Duration       `json:"rtt_max_ns"`
	JitterPairs        int                 `json:"jitter_pairs"`
	OneWayJitterMeanNS float64             `json:"one_way_jitter_mean_ns"`
	ReceiptStalls      int                 `json:"receipt_stalls"`
	WorstReceiptGap    time.Duration       `json:"worst_receipt_gap_ns"`
	Samples            []sampleMeasurement `json:"samples"`
}

/*
================
sampleArrival

Unique replies ordered on the shared measurement origin, separately from
the raw registration order and the sorted RTT values.
================
*/
type sampleArrival struct {
	sequence uint64
	offset   time.Duration
	rtt      time.Duration
}

/*
================
probeElapsed

time.Sub saturates an unrepresentable duration. Reject that result instead
of publishing a clamped offset as if it were measured.
================
*/
func probeElapsed(origin, event time.Time) (time.Duration, error) {
	elapsed := event.Sub(origin)
	if elapsed < 0 {
		return 0, fmt.Errorf("timestamp precedes its origin")
	}
	if !origin.Add(elapsed).Equal(event) {
		return 0, fmt.Errorf("timestamp exceeds the duration range")
	}
	return elapsed, nil
}

/*
================
nearestRankRTT

The nearest-rank percentile of an ascending nonempty sample set. Splitting
the rank calculation keeps the intermediate product bounded by the count.
================
*/
func nearestRankRTT(sorted []time.Duration, percent int) time.Duration {
	count := len(sorted)
	rank := count/100*percent + (count%100*percent+99)/100
	return sorted[rank-1]
}

/*
================
summarizeSamples

No partial summary escapes invalid recorder data. A valid partial run still
reports every unwritten or missing request. Replies tied at the same clock
reading sort by sequence: the timestamp proves no inversion within that tie.
OutOfOrder counts replies below the highest sequence already received.
================
*/
func summarizeSamples(start time.Time, requested int, samples []probeSample) (sampleSummary, error) {
	if requested < 0 || len(samples) > requested {
		return sampleSummary{}, fmt.Errorf("invalid requested/registered sample counts: %d/%d", requested, len(samples))
	}
	if start.IsZero() {
		return sampleSummary{}, fmt.Errorf("measurement start is unset")
	}
	summary := sampleSummary{Requested: requested, Registered: len(samples), Samples: make([]sampleMeasurement, 0, len(samples))}
	seen := make(map[uint64]bool, len(samples))
	rtts := make([]time.Duration, 0, len(samples))
	arrivals := make([]sampleArrival, 0, len(samples))
	for index, sample := range samples {
		if seen[sample.Sequence] {
			return sampleSummary{}, fmt.Errorf("sample %d repeats sequence %d", index, sample.Sequence)
		}
		seen[sample.Sequence] = true
		row := sampleMeasurement{Sequence: sample.Sequence, Written: sample.Written, Responses: sample.Responses}
		if sample.Written && sample.SentAt.IsZero() {
			return sampleSummary{}, fmt.Errorf("sample %d was written without a send timestamp", index)
		}
		if !sample.SentAt.IsZero() {
			offset, err := probeElapsed(start, sample.SentAt)
			if err != nil {
				return sampleSummary{}, fmt.Errorf("sample %d send: %w", index, err)
			}
			row.SentOffset = &offset
		}
		if sample.Written {
			summary.Sent++
		}
		hasResponse := !sample.ReceivedAt.IsZero()
		if hasResponse != (sample.Responses > 0) || hasResponse && !sample.Written {
			return sampleSummary{}, fmt.Errorf("sample %d has inconsistent response/write accounting", index)
		}
		if hasResponse {
			offset, err := probeElapsed(start, sample.ReceivedAt)
			if err != nil {
				return sampleSummary{}, fmt.Errorf("sample %d receipt: %w", index, err)
			}
			rtt, err := probeElapsed(sample.SentAt, sample.ReceivedAt)
			if err != nil {
				return sampleSummary{}, fmt.Errorf("sample %d RTT: %w", index, err)
			}
			if offset < *row.SentOffset || offset-*row.SentOffset != rtt {
				return sampleSummary{}, fmt.Errorf("sample %d timestamps do not share the measurement clock", index)
			}
			duplicates := sample.Responses - 1
			if duplicates > ^uint64(0)-summary.DuplicateReplies {
				return sampleSummary{}, fmt.Errorf("duplicate response count overflows")
			}
			summary.DuplicateReplies += duplicates
			summary.Received++
			row.ReceivedOffset, row.RTT = &offset, &rtt
			rtts = append(rtts, rtt)
			arrivals = append(arrivals, sampleArrival{sequence: sample.Sequence, offset: offset, rtt: rtt})
		}
		summary.Samples = append(summary.Samples, row)
	}
	summary.Missing = summary.Sent - summary.Received
	summary.NotSent = requested - summary.Sent
	if len(rtts) == 0 {
		return summary, nil
	}
	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	summary.RTTP50 = nearestRankRTT(rtts, 50)
	summary.RTTP95 = nearestRankRTT(rtts, 95)
	summary.RTTP99 = nearestRankRTT(rtts, 99)
	summary.RTTMax = rtts[len(rtts)-1]
	sort.Slice(arrivals, func(i, j int) bool {
		if arrivals[i].offset == arrivals[j].offset {
			return arrivals[i].sequence < arrivals[j].sequence
		}
		return arrivals[i].offset < arrivals[j].offset
	})
	var previousOffset time.Duration
	var highest uint64
	var jitterTotalNS float64
	for index, arrival := range arrivals {
		if index > 0 && arrival.sequence < highest {
			summary.OutOfOrder++
		}
		if arrival.sequence > highest {
			highest = arrival.sequence
		}
		gap := arrival.offset - previousOffset
		if gap > receiptStallThreshold {
			summary.ReceiptStalls++
		}
		if gap > summary.WorstReceiptGap {
			summary.WorstReceiptGap = gap
		}
		previousOffset = arrival.offset
		if index > 0 {
			difference := arrival.rtt - arrivals[index-1].rtt
			if difference < 0 {
				difference = -difference
			}
			jitterTotalNS += float64(difference) / 2
			summary.JitterPairs++
		}
	}
	if summary.JitterPairs > 0 {
		summary.OneWayJitterMeanNS = jitterTotalNS / float64(summary.JitterPairs)
	}
	return summary, nil
}
