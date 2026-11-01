/*
===========================================================================

departure_test.go - the logout/restart countdown on the production transport

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
	"testing"
	"time"
)

/*
================
TestDepartureUsesNativeCountdownAndCompletionOnProductionTransport
================
*/
func TestDepartureUsesNativeCountdownAndCompletionOnProductionTransport(t *testing.T) {
	for _, kind := range []byte{1, 2} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testItems())
			rt.Now = func() time.Time { return time.UnixMilli(1000) }
			srv := wireStartServer(t, rt)
			client := wireConnect(t, srv, testDivision, c.Name)
			session, _ := srv.Hub.Session(client.sessionID)
			session.TryMarkWorldReady()
			client.send(t, 0x70b7, []byte{kind})
			client.expectFrame(t, 0xb0b7, []byte{1, 5, kind})
			rt.TickHook()(5999)
			select {
			case <-session.Done():
				t.Fatal("departure completed early")
			default:
			}
			rt.departureMu.Lock()
			count := len(rt.departures)
			rt.departureMu.Unlock()
			if count != 1 {
				t.Fatal("missing pending countdown")
			}
			rt.TickHook()(6000)
			client.expectFrame(t, 0x315a, nil)
			client.expectFrame(t, transport.OpBye, []byte{transport.ByeReasonNormal})
			select {
			case <-session.Done():
			case <-time.After(time.Second):
				t.Fatal("completion did not close session")
			}
			rt.TickHook()(6001)
			rt.departureMu.Lock()
			defer rt.departureMu.Unlock()
			if len(rt.departures) != 0 {
				t.Fatal("completed job retained")
			}
		})
	}
}

/*
================
TestDepartureRefusesUnreadyAndMalformedRequestsWithoutClosing
================
*/
func TestDepartureRefusesUnreadyAndMalformedRequestsWithoutClosing(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	srv := wireStartServer(t, rt)
	client := wireConnect(t, srv, testDivision, c.Name)
	client.send(t, 0x70b7, []byte{2})
	client.expectFrame(t, 0xb0b7, []byte{2, 2})
	for _, p := range [][]byte{nil, {0}, {3}, {2, 1}} {
		client.send(t, 0x70b7, p)
		client.expectFrame(t, 0xb0b7, []byte{2, 1})
	}
	rt.departureMu.Lock()
	defer rt.departureMu.Unlock()
	if len(rt.departures) != 0 {
		t.Fatal("refusal scheduled logout")
	}
}

/*
================
TestDepartureCannotCloseReplacementCharacterSession
================
*/
func TestDepartureCannotCloseReplacementCharacterSession(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	srv := wireStartServer(t, rt)
	first := wireConnect(t, srv, testDivision, c.Name)
	old, _ := srv.Hub.Session(first.sessionID)
	srv.Hub.BindExclusive("departure-fixture", old)
	old.TryMarkWorldReady()
	first.send(t, 0x70b7, []byte{2})
	first.expectFrame(t, 0xb0b7, []byte{1, 5, 2})
	second := wireConnect(t, srv, testDivision, c.Name)
	current, _ := srv.Hub.Session(second.sessionID)
	srv.Hub.BindExclusive("departure-fixture", current)
	current.TryMarkWorldReady()
	rt.TickHook()(6000)
	select {
	case <-current.Done():
		t.Fatal("stale logout closed replacement")
	default:
	}
	second.send(t, 0x70b7, []byte{1})
	second.expectFrame(t, 0xb0b7, []byte{1, 5, 1})
}

/*
================
TestDetachedDepartureAllowsSynchronousGameplayCleanup
================
*/
func TestDetachedDepartureAllowsSynchronousGameplayCleanup(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	srv := wireStartServer(t, rt)
	client := wireConnect(t, srv, testDivision, c.Name)
	session, _ := srv.Hub.Session(client.sessionID)
	session.TryMarkWorldReady()
	cleaned := make(chan struct{})
	srv.Hub.OnSessionClose(func(s *transport.Session, _ error) {
		if s.ID != session.ID {
			return
		}
		rt.EndCommerceSession(testDivision, c, s.ID)
		rt.ForgetCharacterSession(testDivision, c.Name, s.ID)
		close(cleaned)
	})
	client.send(t, 0x70b7, []byte{2})
	client.expectFrame(t, 0xb0b7, []byte{1, 5, 2})
	client.conn.Close()
	wait.Eventually(t, time.Second, "the socket to detach", func() bool {
		return session.Kind() == "detached"
	})
	rt.advanceDepartures(6000)
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not complete")
	}
	rt.TickHook()(6001)
}

/*
================
TestDepartureCancelEndsTheCountdown

A ground click during the countdown sends the empty 0x731F: the countdown
ends with [1] and the tick no longer closes the session. A cancel with no
countdown is the error [2][0].
================
*/
func TestDepartureCancelEndsTheCountdown(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.Now = func() time.Time { return time.UnixMilli(1000) }
	srv := wireStartServer(t, rt)
	client := wireConnect(t, srv, testDivision, c.Name)
	session, _ := srv.Hub.Session(client.sessionID)
	session.TryMarkWorldReady()

	client.send(t, opDepartureCancelRequest, nil)
	client.expectFrame(t, opDepartureCancelResponse, []byte{departureResultError, departureCancelNotPending})

	client.send(t, opDepartureRequest, []byte{1})
	client.expectFrame(t, opDepartureResponse, []byte{departureResultOK, departureSeconds, 1})
	client.send(t, opDepartureCancelRequest, nil)
	client.expectFrame(t, opDepartureCancelResponse, []byte{departureResultOK})
	rt.TickHook()(6000)
	select {
	case <-session.Done():
		t.Fatal("a cancelled countdown closed the session")
	default:
	}
	rt.departureMu.Lock()
	defer rt.departureMu.Unlock()
	if len(rt.departures) != 0 {
		t.Fatal("cancelled countdown retained")
	}
}

/*
================
TestDepartureFollowsTheNativeBattleRules

The reference server's logout request: ordinary battle still logs out; in battle
a PvP state or a ride refuses with code 1, and a teleport in progress
refuses with code 2. A PvP state outside battle does not refuse.
================
*/
func TestDepartureFollowsTheNativeBattleRules(t *testing.T) {
	const now = 1000
	cases := []struct {
		name  string
		setup func(c *enterworld.Character)
		want  []byte
	}{
		{"battle only", func(c *enterworld.Character) { c.BattleUntilMs = now + 5000 }, []byte{1, 5, 1}},
		{"battle and aggression", func(c *enterworld.Character) {
			c.BattleUntilMs = now + 5000
			c.Aggressions = map[uint32]uint32{100: 5}
		}, []byte{2, departureRefusedInBattle}},
		{"battle and murderer", func(c *enterworld.Character) {
			c.BattleUntilMs = now + 5000
			c.PK = &domain.PKRecord{Penalty: 10}
		}, []byte{2, departureRefusedInBattle}},
		{"battle and riding", func(c *enterworld.Character) {
			c.BattleUntilMs = now + 5000
			c.ActiveCOS = &enterworld.CharacterCOS{GID: 77, Summoned: true, Mounted: true}
		}, []byte{2, departureRefusedInBattle}},
		{"aggression out of battle", func(c *enterworld.Character) { c.Aggressions = map[uint32]uint32{100: 5} }, []byte{1, 5, 1}},
		{"teleporting", func(c *enterworld.Character) { c.NativeTeleportMode = 1 }, []byte{2, departureRefusedTeleporting}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testCharacter()
			tc.setup(c)
			rt, _ := newTestRuntime(c, testItems())
			rt.Now = func() time.Time { return time.UnixMilli(now) }
			srv := wireStartServer(t, rt)
			client := wireConnect(t, srv, testDivision, c.Name)
			session, _ := srv.Hub.Session(client.sessionID)
			session.TryMarkWorldReady()
			client.send(t, 0x70b7, []byte{1})
			client.expectFrame(t, 0xb0b7, tc.want)
			rt.departureMu.Lock()
			pending := len(rt.departures)
			rt.departureMu.Unlock()
			if (pending == 1) != (tc.want[0] == 1) {
				t.Fatalf("countdown scheduled = %v for answer %v", pending == 1, tc.want)
			}
		})
	}
}

/*
================
TestDepartureRefusesAFreeBattleCapeInBattle

The native logout request reads the cape group in the same in-battle arm as
the job suit: a worn cape refuses during battle and logs out once it ends.
================
*/
func TestDepartureRefusesAFreeBattleCapeInBattle(t *testing.T) {
	const now = 1000
	for _, tc := range []struct {
		name        string
		battleUntil int64
		want        []byte
	}{
		{"in battle", now + 5000, []byte{2, departureRefusedInBattle}},
		{"out of battle", 0, []byte{1, 5, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testCharacter()
			items := testItems()
			cape := &enterworld.ItemRef{Codename: "TEST_PVP_CAPE", RefObjID: 63000, TypeIDs: [4]int64{3, 1, 7, 5},
				NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: 1})}
			items[cape.Codename] = cape
			c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: domain.JobSuitSlot, RefObjID: cape.RefObjID,
				Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1})
			c.BattleUntilMs = tc.battleUntil
			rt, _ := newTestRuntime(c, items)
			rt.Now = func() time.Time { return time.UnixMilli(now) }
			srv := wireStartServer(t, rt)
			client := wireConnect(t, srv, testDivision, c.Name)
			session, _ := srv.Hub.Session(client.sessionID)
			session.TryMarkWorldReady()
			client.send(t, 0x70b7, []byte{1})
			client.expectFrame(t, 0xb0b7, tc.want)
		})
	}
}
