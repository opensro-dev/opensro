/*
===========================================================================

teleportprotection_test.go - the arrival grace starts at game-ready

Drives the real travel owners and the game-ready hook with a deterministic
clock.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
teleportTravel

Runs one travel owner to its arrival. The scroll paths cast and then tick
past their countdown.
================
*/
func teleportTravel(t *testing.T, rt *Runtime, c *enterworld.Character, clock *fakeClock, path string) {
	t.Helper()
	destination := simulation.SeedWorldState(c).Spawn
	destination.X += 10
	world := instance.ID(domain.CharacterWorldInstance(c))
	switch path {
	case "gate":
		rt.commitGateTravel(gateTravel{division: testDivision, character: c, destination: destination, world: world,
			reason: "test-teleport"}, func() (int64, OpResult, bool) { return 0, OpResult{}, true })
	case "relocation":
		rt.relocateCharacter(testDivision, c, "test-teleport", func() (travelPoint, bool) {
			return travelPoint{spawn: destination, world: world}, true
		})
	case "return", "reverse":
		if path == "return" {
			useReturn(rt, c)
		} else {
			c.World.LastDeathPoint = &domain.WorldPoint{WorldSpawn: *worldSpawnFromMission(destination)}
			reverseReturn(rt, c, reverseReturnLastDeath)
		}
		clock.Advance(30 * time.Second)
		rt.TickHook()(clock.NowMs())
	default:
		t.Fatalf("unknown travel %q", path)
	}
}

/*
================
gameReady

The scene going live: the wired game-ready hook.
================
*/
func gameReady(rt *Runtime, c *enterworld.Character) {
	rt.ReconcileScene(&transport.Session{ID: 1}, testDivision, c)
}

/*
================
TestTeleportProtectionStartsAtGameReady

Every travel owner marks a successful arrival only. The body mode waits for
game-ready however long the load takes, is published to the player and its
peers, and lasts exactly the revival's grace from game-ready.
================
*/
func TestTeleportProtectionStartsAtGameReady(t *testing.T) {
	for _, path := range []string{"gate", "relocation", "return", "reverse"} {
		for _, rejected := range []bool{false, true} {
			t.Run(path+map[bool]string{false: "/success", true: "/rollback"}[rejected], func(t *testing.T) {
				rt, c, clock := reverseReturnFixture(t)
				rt.TeleportGrace = true
				if rejected {
					rt.deps = portalRejectEntry{rt.deps}
				}
				var self, peers int
				rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { self += len(frames) }
				rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { peers += len(frames) }
				teleportTravel(t, rt, c, clock, path)
				if c.NativeBodyStatus != 0 {
					t.Fatalf("body status %d before game-ready, want 0", c.NativeBodyStatus)
				}
				// A load longer than the grace must not spend it.
				clock.Advance(20 * time.Second)
				rt.TickHook()(clock.NowMs())
				self, peers = 0, 0
				gameReady(rt, c)
				if rejected {
					if c.NativeBodyStatus != 0 {
						t.Fatal("a failed teleport granted the grace")
					}
					return
				}
				if c.NativeBodyStatus != untouchableBodyStatus || self == 0 || peers == 0 {
					t.Fatalf("after game-ready: body %d, self %d, peers %d frames", c.NativeBodyStatus, self, peers)
				}
				clock.Advance(reviveUntouchableMs*time.Millisecond - time.Millisecond)
				rt.TickHook()(clock.NowMs())
				if c.NativeBodyStatus != untouchableBodyStatus {
					t.Fatal("the grace ended early")
				}
				clock.Advance(time.Millisecond)
				rt.TickHook()(clock.NowMs())
				if c.NativeBodyStatus != 0 {
					t.Fatalf("body status %d after the grace, want 0", c.NativeBodyStatus)
				}
				gameReady(rt, c)
				if c.NativeBodyStatus != 0 {
					t.Fatal("a second game-ready repeated the grace")
				}
			})
		}
	}
}

/*
================
TestTeleportProtectionMarkEndsWithTheSession

A player who leaves while loading carries no mark into the next login.
================
*/
func TestTeleportProtectionMarkEndsWithTheSession(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	rt.TeleportGrace = true
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	teleportTravel(t, rt, c, clock, "gate")
	rt.ForgetCharacter(testDivision, c.Name)
	gameReady(rt, c)
	if c.NativeBodyStatus != 0 {
		t.Fatalf("body status %d after a new login, want 0", c.NativeBodyStatus)
	}
}

/*
================
TestTeleportProtectionKeepsGMStatus

A GM invincible or invisible mode is the stronger state and stays.
================
*/
func TestTeleportProtectionKeepsGMStatus(t *testing.T) {
	rt, c, clock := reverseReturnFixture(t)
	rt.TeleportGrace = true
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	teleportTravel(t, rt, c, clock, "gate")
	c.NativeBodyStatus = 3
	gameReady(rt, c)
	if c.NativeBodyStatus != 3 {
		t.Fatalf("body status %d, want the GM's 3", c.NativeBodyStatus)
	}
}

/*
================
TestTeleportProtectionIsOffByDefault

With SRO_TELEPORT_GRACE unset (native), no travel path marks an arrival
and game-ready grants nothing.
================
*/
func TestTeleportProtectionIsOffByDefault(t *testing.T) {
	t.Setenv(EnvTeleportGrace, "")
	if TeleportGraceFromEnv() {
		t.Fatal("the grace is on with the switch unset")
	}
	for _, path := range []string{"gate", "relocation", "return", "reverse"} {
		t.Run(path, func(t *testing.T) {
			rt, c, clock := reverseReturnFixture(t)
			rt.TeleportGrace = TeleportGraceFromEnv()
			rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
			rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
			teleportTravel(t, rt, c, clock, path)
			gameReady(rt, c)
			if c.NativeBodyStatus != 0 {
				t.Fatalf("native default granted body status %d", c.NativeBodyStatus)
			}
		})
	}
	for _, on := range []string{"on", "1", "TRUE"} {
		t.Setenv(EnvTeleportGrace, on)
		if !TeleportGraceFromEnv() {
			t.Fatalf("%q did not turn the grace on", on)
		}
	}
}
