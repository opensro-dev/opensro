/*
===========================================================================

jobhonor_test.go - the Job Honor feed's wire shape (#3530)

A snapshot carries both periods' rows and the viewer's own standing,
caps each period at JobHonorMaxRows, and bounds an alias to 64 bytes.

===========================================================================
*/
package wire

import (
	"encoding/binary"
	"strings"
	"testing"
)

// snapshotReader walks EncodeJobHonorSnapshot's body in the tests.
type snapshotReader struct {
	buf []byte
}

func (r *snapshotReader) u8() int {
	v := r.buf[0]
	r.buf = r.buf[1:]
	return int(v)
}

func (r *snapshotReader) u16() int {
	v := binary.LittleEndian.Uint16(r.buf)
	r.buf = r.buf[2:]
	return int(v)
}

func (r *snapshotReader) u32() int64 {
	v := binary.LittleEndian.Uint32(r.buf)
	r.buf = r.buf[4:]
	return int64(v)
}

func (r *snapshotReader) u64() int64 {
	v := binary.LittleEndian.Uint64(r.buf)
	r.buf = r.buf[8:]
	return int64(v)
}

func (r *snapshotReader) row() JobHonorRow {
	job := r.u8()
	n := r.u8()
	alias := string(r.buf[:n])
	r.buf = r.buf[n:]
	return JobHonorRow{Job: job, Alias: alias, Points: r.u32(),
		Tier: r.u8(), Tonight: r.u8()}
}

func TestJobHonorSnapshotRoundTrips(t *testing.T) {
	current := []JobHonorRow{{Job: 1, Alias: "Alpha", Points: 300, Tier: 2, Tonight: 3}}
	previous := []JobHonorRow{{Job: 2, Alias: "Beta", Points: 50, Tier: 1, Tonight: 1}}
	own := JobHonorOwn{Present: true, Job: 1, Place: 1, Tier: 2, Tonight: 3,
		CapLeft: 750, Today: [3]int64{40, 0, 5}, Month: [3]int64{90, 10, 5}}
	r := &snapshotReader{buf: EncodeJobHonorSnapshot(111, 222, 333, current, previous, own)}
	if got := r.u64(); got != 111 {
		t.Fatalf("serverNowMs = %d, want 111", got)
	}
	if got := r.u64(); got != 222 {
		t.Fatalf("nextChangeMs = %d, want 222", got)
	}
	if got := r.u64(); got != 333 {
		t.Fatalf("previousPeriodEndMs = %d, want 333", got)
	}
	if got := r.u16(); got != 1 {
		t.Fatalf("currentCount = %d, want 1", got)
	}
	if got := r.row(); got != current[0] {
		t.Fatalf("current row = %+v, want %+v", got, current[0])
	}
	if got := r.u16(); got != 1 {
		t.Fatalf("previousCount = %d, want 1", got)
	}
	if got := r.row(); got != previous[0] {
		t.Fatalf("previous row = %+v, want %+v", got, previous[0])
	}
	if got := r.u8(); got != 1 {
		t.Fatalf("own present = %d, want 1", got)
	}
	if got := r.u8(); got != own.Job {
		t.Fatalf("own job = %d, want %d", got, own.Job)
	}
	if got := r.u16(); got != own.Place {
		t.Fatalf("own place = %d, want %d", got, own.Place)
	}
	if got := r.u8(); got != own.Tier {
		t.Fatalf("own tier = %d, want %d", got, own.Tier)
	}
	if got := r.u8(); got != own.Tonight {
		t.Fatalf("own tonight = %d, want %d", got, own.Tonight)
	}
	if got := r.u32(); got != int64(own.CapLeft) {
		t.Fatalf("own capLeft = %d, want %d", got, own.CapLeft)
	}
	for i := 0; i < 3; i++ {
		if got := r.u32(); got != own.Today[i] {
			t.Fatalf("own today[%d] = %d, want %d", i, got, own.Today[i])
		}
	}
	for i := 0; i < 3; i++ {
		if got := r.u32(); got != own.Month[i] {
			t.Fatalf("own month[%d] = %d, want %d", i, got, own.Month[i])
		}
	}
	if len(r.buf) != 0 {
		t.Fatalf("%d undecoded bytes left", len(r.buf))
	}
}

func TestJobHonorSnapshotCapsRowsAndAliases(t *testing.T) {
	many := make([]JobHonorRow, JobHonorMaxRows+10)
	for i := range many {
		many[i] = JobHonorRow{Job: 1, Alias: strings.Repeat("a", 70), Points: 1}
	}
	r := &snapshotReader{buf: EncodeJobHonorSnapshot(0, 0, 0, many, nil, JobHonorOwn{})}
	r.u64()
	r.u64()
	r.u64()
	if got := r.u16(); got != JobHonorMaxRows {
		t.Fatalf("currentCount = %d, want the %d cap", got, JobHonorMaxRows)
	}
	first := r.row()
	if len(first.Alias) != jobHonorMaxAlias {
		t.Fatalf("alias is %d bytes, want the %d bound", len(first.Alias), jobHonorMaxAlias)
	}
	// Drain the rest of the rows and the empty previous period.
	for i := 1; i < JobHonorMaxRows; i++ {
		r.row()
	}
	if got := r.u16(); got != 0 {
		t.Fatalf("previousCount = %d, want 0", got)
	}
	if got := r.u8(); got != 0 {
		t.Fatalf("own present = %d, want 0", got)
	}
	if len(r.buf) != 0 {
		t.Fatalf("%d undecoded bytes left", len(r.buf))
	}
}
