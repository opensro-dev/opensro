/*
===========================================================================

departure.go - the logout/restart countdown: request, cancel and completion

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/transport"
)

// v1.188 50EFF0 -> 50EF70 explicit request: ordinary five-second delay.
// Wire identifiers and byte widths are the v1.150 client's 700020/74B2E0.
const departureSeconds = 5

// Logout/restart countdown opcodes of the v1.150 client.
const (
	// opDepartureRequest is 0x70B7 [u8 mode] (1 logout, 2 restart).
	opDepartureRequest uint16 = 0x70B7
	// opDepartureResponse is 0xB0B7 (CPSMission_OnLogoutResponse0xB0B7):
	// [1][seconds][mode] arms the countdown, [2][code] refuses.
	opDepartureResponse uint16 = 0xB0B7
	// opDepartureCancelRequest is the empty 0x731F
	// (CGameApp_SendLogoutCancelRequest0x731F), sent by a ground click
	// during the countdown (CGInterface_MoveToWorldPoint, +0x39C set).
	opDepartureCancelRequest uint16 = 0x731F
	// opDepartureCancelResponse is 0xB31F
	// (CPSMission_OnLogoutCancelResponse0xB31F): [1] ends the countdown
	// (UIIT_MSG_LOGOUT_REMAIN_TIME_CANCLE), [2][code] is a notice.
	opDepartureCancelResponse uint16 = 0xB31F
	departureResultOK         uint8  = 1
	departureResultError      uint8  = 2
	// departureRefusedInBattle and departureRefusedTeleporting are the low
	// bytes of 50EFF0's 0x801 / 0x802 (UIIT_MSG_LOGOUT_ERR_CANT_LOGOUT_IN_
	// BATTLE_STATE / _WHILE_TELEPORT_WORKING, client category 0xF 1 / 2).
	departureRefusedInBattle    uint8 = 1
	departureRefusedTeleporting uint8 = 2
	// departureCancelNotPending is v1.188 CGObjPC_OnLogoutCancelRequest's
	// error code (0x50F13D push 0) when no countdown runs.
	departureCancelNotPending uint8 = 0
)

/*
================
pendingDeparture
================
*/
type pendingDeparture struct {
	session             *transport.Session
	division, character string
	due                 int64
}

/*
================
registerDeparture

Wires the logout/restart countdown: 0x70B7 arms it, 0x731F cancels it, and
advanceDepartures completes it.
================
*/
func (rt *Runtime) registerDeparture(hub *transport.Hub) {
	hub.Handle(opDepartureCancelRequest, rt.cancelDeparture)
	hub.Handle(opDepartureRequest, func(s *transport.Session, _ uint16, p []byte) {
		c, division, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound || len(p) != 1 || (p[0] != 1 && p[0] != 2) {
			_ = s.Send(opDepartureResponse, []byte{departureResultError, 1})
			return
		}
		unlock := rt.lockDivision(division)
		if s.Evicted() {
			unlock()
			return
		}
		if !s.WorldReady() {
			unlock()
			_ = s.Send(opDepartureResponse, []byte{departureResultError, 2})
			return
		}
		if code := rt.departureRefusal(division, c); code != 0 {
			unlock()
			_ = s.Send(opDepartureResponse, []byte{departureResultError, code})
			return
		}
		// 50EF70 first cancels the actor's current action through vtable +6A8.
		rt.ClearCombatIntent(division, c.Name)
		rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
		rt.departureMu.Lock()
		if rt.departures == nil {
			rt.departures = make(map[uint64]pendingDeparture)
		}
		rt.departures[s.ID] = pendingDeparture{session: s, division: division, character: c.Name, due: rt.Now().UnixMilli() + departureSeconds*1000}
		rt.departureMu.Unlock()
		unlock()
		// A queue refusal can synchronously invoke both close hooks below and
		// gameplay cleanup. Neither departure nor division locks may be held.
		_ = s.Send(opDepartureResponse, []byte{departureResultOK, departureSeconds, p[0]})
	})
	hub.OnSessionClose(func(s *transport.Session, _ error) {
		rt.departureMu.Lock()
		delete(rt.departures, s.ID)
		rt.departureMu.Unlock()
	})
}

/*
================
departureRefusal

CGObjPC_OnLogoutRequest (v1.188 50EFF0). Ordinary battle does not stop a
logout: in battle (state+0xD) it is refused only for a PvP state of 1 or
2 (state+0xC), job mode (job class, vtable +0x11C, not 4), a siege world
while its war runs, or a ride. Out of battle, or in battle with none of
those, a teleport in progress (vtable +0x120) refuses. 0 admits.

INFERENCE: 50EFF0 also refuses in battle while the actor's byte at +0x21EC
(arg1[0x87B]) is set; no port state carries it, so that arm never refuses.
================
*/
func (rt *Runtime) departureRefusal(division string, c *enterworld.Character) uint8 {
	code := uint8(0)
	now := rt.Now().UnixMilli()
	rt.deps.Read(division, func() {
		if inBattleState(c, now) {
			_, siegeWorld := rt.activeBattleFortress(division, c)
			ride := c.ActiveCOS
			riding := ride != nil && ride.Summoned && ride.Mounted
			if pvp := c.PVPState(); pvp == 1 || pvp == 2 || enterworld.PartyJobClass(c) != enterworld.NoJobClass || siegeWorld || riding {
				code = departureRefusedInBattle
				return
			}
		}
		if c.NativeTeleportMode != 0 {
			code = departureRefusedTeleporting
		}
	})
	return code
}

/*
================
cancelDeparture

v1.188 CGObjPC_OnLogoutCancelRequest (50F130, its 0x7006): a countdown in
progress ends and the client gets [1]; without one the answer is the error
[2][0], which the v1.150 client shows no notice for. The request has no
body; a non-empty one is treated as having nothing to cancel.
================
*/
func (rt *Runtime) cancelDeparture(s *transport.Session, _ uint16, p []byte) {
	_, division, bound := enterworld.SessionCharacter(rt.deps, s)
	if !bound {
		return
	}
	pending := false
	if len(p) == 0 {
		unlock := rt.lockDivision(division)
		rt.departureMu.Lock()
		_, pending = rt.departures[s.ID]
		delete(rt.departures, s.ID)
		rt.departureMu.Unlock()
		unlock()
	}
	if !pending {
		_ = s.Send(opDepartureCancelResponse, []byte{departureResultError, departureCancelNotPending})
		return
	}
	_ = s.Send(opDepartureCancelResponse, []byte{departureResultOK})
}

/*
================
advanceDepartures
================
*/
func (rt *Runtime) advanceDepartures(now int64) {
	rt.departureMu.Lock()
	var due []pendingDeparture
	for id, job := range rt.departures {
		select {
		case <-job.session.Done():
			delete(rt.departures, id)
			continue
		default:
		}
		if job.session.Evicted() {
			delete(rt.departures, id)
			continue
		}
		if now >= job.due {
			due = append(due, job)
		}
	}
	rt.departureMu.Unlock()
	for _, job := range due {
		unlock := rt.lockDivision(job.division)
		rt.departureMu.Lock()
		current, pending := rt.departures[job.session.ID]
		if !pending || current.due > now {
			rt.departureMu.Unlock()
			unlock()
			continue
		}
		delete(rt.departures, job.session.ID)
		rt.departureMu.Unlock()
		closeSession := false
		division, name, bound := job.session.CharacterBinding()
		if bound && !job.session.Evicted() && division == job.division && name == job.character {
			// Queue completion before orderly transport closure. The normal close hook
			// owns presence, companions, effects and character-world retirement.
			closeSession = true
		}
		unlock()
		// Detached closure AND send-overflow invoke teardown synchronously.
		// Publish outside the transaction; Send still rejects an evicted owner.
		if closeSession && job.session.Send(0x315a, nil) == nil {
			job.session.CloseWhenDrained(transport.ByeReasonNormal)
		}
	}
}
