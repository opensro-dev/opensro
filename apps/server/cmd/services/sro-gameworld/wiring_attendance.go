/*
===========================================================================

wiring_attendance.go - the attendance ledger and calendar feed (#3255)

Port-only, not native (SRO_ATTENDANCE, off by default). On, the ledger
(platform/attendance) opens beside the shard's authority state, the
calendar feed (game/attendancefeed) keeps every entered session told of
its character's month (0x3FD2), the action runtime answers the check-in
request (0x7FD2) with that month's snapshot, and the application closes
the ledger at shutdown. Off, nothing opens, no tick hook is installed,
0x7FD2 is ignored and no frame is sent.

===========================================================================
*/
package main

import (
	"fmt"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/attendancefeed"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/attendance"
)

/*
================
installAttendance

Opens the ledger and builds the calendar feed over it. The application
owns the service from here, as it owns the other ledgers.
================
*/
func installAttendance(application *gameWorldApplication, gameplay *gameplayPlane, shardID string) error {
	if !action.AttendanceFromEnv() {
		return nil
	}
	dir := store.DirForShardFromEnv(shardID)
	service, err := attendance.Open(dir, time.Now)
	if err != nil {
		return fmt.Errorf("attendance: %w", err)
	}
	application.ledgers.attendance = service
	gameplay.attendance = service
	gameplay.attendanceFeed = attendancefeed.New(attendancefeed.Sources{
		Month: service.Current,
		Now:   service.Now,
	})
	gameplay.items.AttendanceCheckIn = attendanceCheckIn(service)
	log.Infof("attendance: monthly calendar on (%s, port-only, not native); ledger in %s",
		action.EnvAttendance, filepath.Join(dir, attendance.FileName))
	return nil
}

/*
================
attendanceCheckIn

Marks today for the character and answers with the month snapshot.
Slice 2: only an empty body is accepted (later claim ops will share
0x7FD2). A ledger error refuses; an already-checked day still returns
the current month so the client sees today's bit set. The calendar
feed's next poll also sees the change.
================
*/
func attendanceCheckIn(service *attendance.Service) action.AttendanceCheckInFunc {
	return func(characterID int64, payload []byte) []wire.Frame {
		if len(payload) != 0 {
			return nil
		}
		month, _, err := service.CheckIn(characterID)
		if err != nil {
			return nil
		}
		return []wire.Frame{{Opcode: wire.OpAttendanceSnapshot,
			Payload: attendancefeed.Snapshot(month, service.Now())}}
	}
}

/*
================
attendanceTick

The calendar feed's tick hook: one paced pass over the division's
entered sessions; a snapshot goes to each session whose month changed.
================
*/
func (game *gameplayPlane) attendanceTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	views := sessionViews(ticker.Source)
	sessions := make([]attendancefeed.Session, 0, len(views))
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		sessions = append(sessions, attendancefeed.Session{ID: view.SessionID, CharacterID: view.CharacterID})
	}
	for sessionID, payload := range game.attendanceFeed.Tick(nowMs, sessions) {
		ticker.Push.PushToSession(sessionID, []simulation.Frame{{Opcode: wire.OpAttendanceSnapshot, Payload: payload}})
	}
	return nil
}
