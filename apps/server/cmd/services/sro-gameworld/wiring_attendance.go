/*
===========================================================================

wiring_attendance.go - the attendance ledger and calendar feed (#3255)

Port-only, not native (SRO_ATTENDANCE, off by default). On, the ledger
(platform/attendance) opens beside the shard's authority state, the
calendar feed (game/attendancefeed) keeps every entered session told of
its character's month (0x3FD2), the action runtime answers the request
(0x7FD2) for check-in and tier claims, and the application closes the
ledger at shutdown. Off, nothing opens, no tick hook is installed,
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
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/attendancefeed"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
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
	gameplay.items.AttendanceRequest = attendanceRequest(gameplay.items, gameplay.deps, service)
	log.Infof("attendance: monthly calendar on (%s, port-only, not native); ledger in %s",
		action.EnvAttendance, filepath.Join(dir, attendance.FileName))
	return nil
}

/*
================
attendanceRequest

Answers 0x7FD2: check-in (empty or op 0) or a continuity/accumulation
claim (op 1/2 + tier index). A claim marks the tier, grants the reward
item, and rolls the mark back if the grant refuses. Every answer is the
month snapshot (claims also carry the inventory receipts).
================
*/
func attendanceRequest(items *action.Runtime, deps *enterworld.Deps, service *attendance.Service) action.AttendanceRequestFunc {
	return func(division string, c *enterworld.Character, payload []byte) []wire.Frame {
		if c == nil || service == nil {
			return nil
		}
		switch {
		case len(payload) == 0 || (len(payload) == 1 && payload[0] == wire.AttendanceOpCheckIn):
			month, _, err := service.CheckIn(c.ID)
			if err != nil {
				return nil
			}
			return attendanceSnapshotFrames(month, service.Now())
		case len(payload) == 2 && (payload[0] == wire.AttendanceOpClaimContinuity || payload[0] == wire.AttendanceOpClaimAccumulation):
			return attendanceClaim(items, deps, service, division, c, payload[0] == wire.AttendanceOpClaimContinuity, int(payload[1]))
		default:
			return nil
		}
	}
}

func attendanceClaim(items *action.Runtime, deps *enterworld.Deps, service *attendance.Service, division string, c *enterworld.Character, continuity bool, tierIndex int) []wire.Frame {
	if items == nil || deps == nil {
		return nil
	}
	if items.Stalls != nil && items.Stalls.Keeping(division, c.Name) ||
		items.Exchanges != nil && items.Exchanges.Trading(division, c.Name) {
		return nil
	}
	var month attendance.Month
	var err error
	if continuity {
		month, err = service.ClaimContinuity(c.ID, tierIndex)
	} else {
		month, err = service.ClaimAccumulation(c.ID, tierIndex)
	}
	if err != nil {
		return nil
	}
	reward := attendance.RewardFor(tierIndex)
	var frames []wire.Frame
	var planErr error
	if !deps.Update(c, domain.GrantAttendanceReward, func() bool {
		rows, receipts, err := items.PlanQuestInventory(c, nil,
			[]inventory.ItemAmount{{Codename: reward, Count: 1}})
		if err != nil {
			planErr = err
			return false
		}
		c.MissionInventory = rows
		frames = receipts
		return true
	}) {
		if continuity {
			_ = service.UnclaimContinuity(c.ID, tierIndex)
		} else {
			_ = service.UnclaimAccumulation(c.ID, tierIndex)
		}
		if planErr != nil {
			log.Infof("attendance: reward %s for %s refused: %v", reward, c.Name, planErr)
		}
		return nil
	}
	return append(frames, attendanceSnapshotFrames(month, service.Now())...)
}

func attendanceSnapshotFrames(month attendance.Month, now time.Time) []wire.Frame {
	return []wire.Frame{{Opcode: wire.OpAttendanceSnapshot,
		Payload: attendancefeed.Snapshot(month, now)}}
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
