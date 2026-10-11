/*
===========================================================================

calendar.go - the world-server calendar and its native wire encoding

Package calendar owns the one world clock every reader shares: logins, the
world-ready push (0x31AD) and quest periods. SR_GameServer 427660
initializes it with scale=30 and zero seed seconds; 427720 divides elapsed
milliseconds by floor(30000/1440). 4e3eb0 serializes the existing calendar
at login. No login-local epoch, wall-clock date, or month modulo occurs in
these intervals. A GM's /SETTIME moves it (SetHour).

===========================================================================
*/
package calendar

import (
	"encoding/binary"
	"sync/atomic"
	"time"
)

// MillisecondsPerGameSecond is 427720's divisor: floor(30000/1440).
const MillisecondsPerGameSecond int64 = 20

// Seconds in a game day, hour and minute, and the hours in a day.
const (
	secondsPerDay    = 86400
	secondsPerHour   = 3600
	secondsPerMinute = 60
	hoursPerDay      = 24
	minutesPerHour   = 60
)

// One immutable process-lifetime epoch, shared by both clock packet producers.
// time.Since retains Go's monotonic component, matching GetTickCount's role.
var startedAt = time.Now()

// shiftMilli moves the world clock against that epoch: a GM's /SETTIME
// (SetHour). Every reader goes through elapsedMilli, so logins, the
// world-ready push and quest periods all follow one moved clock.
var shiftMilli atomic.Int64

/*
================
Value

One calendar reading: the game day, hour and minute.
================
*/
type Value struct {
	Day          uint16
	Hour, Minute uint8
}

/*
================
AtElapsedMilli

The calendar at a world-clock age in milliseconds (a negative age is the
epoch).
================
*/
func AtElapsedMilli(elapsed int64) Value {
	if elapsed < 0 {
		elapsed = 0
	}
	seconds := elapsed / MillisecondsPerGameSecond
	return Value{
		Day:    uint16(seconds / secondsPerDay),
		Hour:   uint8(seconds / secondsPerHour % hoursPerDay),
		Minute: uint8(seconds / secondsPerMinute % minutesPerHour),
	}
}

/*
================
Current

The world clock now.
================
*/
func Current() Value { return AtElapsedMilli(elapsedMilli()) }

/*
================
elapsedMilli

The world clock's age: process time plus any GM shift.
================
*/
func elapsedMilli() int64 { return time.Since(startedAt).Milliseconds() + shiftMilli.Load() }

/*
================
SetHour

Moves the world clock to minute 0 of hour on the current game day and
returns the new time. SR_GameServer relays /SETTIME to the shard manager
("SetGameTime to %d", 51F09B); INFERENCE: the day is kept and the minutes
restart, since only the hour is sent. Hours past 23 wrap to 0, as the
client composer sends none. The new shift is swapped in only if no other
SetHour moved the clock meanwhile, so two at once never both add theirs.
================
*/
func SetHour(hour uint8) Value {
	if hour >= hoursPerDay {
		hour = 0
	}
	for {
		shift := shiftMilli.Load()
		now := max(time.Since(startedAt).Milliseconds()+shift, 0)
		seconds := now / MillisecondsPerGameSecond
		target := seconds - seconds%secondsPerDay + int64(hour)*secondsPerHour
		if shiftMilli.CompareAndSwap(shift, shift+target*MillisecondsPerGameSecond-now) {
			return AtElapsedMilli(target * MillisecondsPerGameSecond)
		}
	}
}

/*
================
Payload

The 0x31AD body (4e3eb0): u16 day, u8 hour, u8 minute.
================
*/
func (v Value) Payload() []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload, v.Day)
	payload[2], payload[3] = v.Hour, v.Minute
	return payload
}
