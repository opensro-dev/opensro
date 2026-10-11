// Package calendar owns the world-server calendar and native wire encoding.
package calendar

import (
	"encoding/binary"
	"sync/atomic"
	"time"
)

// SR_GameServer 427660 initializes one world clock with scale=30 and zero
// seed seconds; 427720 divides elapsed milliseconds by floor(30000/1440).
// 4e3eb0 serializes the existing calendar at login. No login-local epoch,
// wall-clock date, or month modulo occurs in these intervals.
const MillisecondsPerGameSecond int64 = 20

// One immutable process-lifetime epoch, shared by both clock packet producers.
// time.Since retains Go's monotonic component, matching GetTickCount's role.
var startedAt = time.Now()

// shiftMilli moves the world clock against that epoch: a GM's /SETTIME
// (SetHour). Every reader goes through elapsedMilli, so logins, the
// world-ready push and quest periods all follow one moved clock.
var shiftMilli atomic.Int64

type Value struct {
	Day          uint16
	Hour, Minute uint8
}

func AtElapsedMilli(elapsed int64) Value {
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := elapsed / MillisecondsPerGameSecond
	return Value{Day: uint16(seconds / 86400), Hour: uint8(seconds / 3600 % 24), Minute: uint8(seconds / 60 % 60)}
}
func Current() Value { return AtElapsedMilli(elapsedMilli()) }

// elapsedMilli is the world clock's age: process time plus any GM shift.
func elapsedMilli() int64 { return time.Since(startedAt).Milliseconds() + shiftMilli.Load() }

// SetHour moves the world clock to minute 0 of hour on the current game day
// and returns the new time. SR_GameServer relays /SETTIME to the shard
// manager ("SetGameTime to %d", 51F09B); INFERENCE: the day is kept and the
// minutes restart, since only the hour is sent. Hours past 23 wrap to 0, as
// the client composer sends none.
func SetHour(hour uint8) Value {
	if hour >= 24 {
		hour = 0
	}
	now := elapsedMilli()
	if now < 0 {
		now = 0
	}
	seconds := now / MillisecondsPerGameSecond
	target := seconds - seconds%86400 + int64(hour)*3600
	shiftMilli.Add(target*MillisecondsPerGameSecond - now)
	return AtElapsedMilli(target * MillisecondsPerGameSecond)
}
func (v Value) Payload() []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload, v.Day)
	payload[2], payload[3] = v.Hour, v.Minute
	return payload
}
