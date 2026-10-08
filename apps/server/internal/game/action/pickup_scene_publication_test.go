/*
===========================================================================

pickup_scene_publication_test.go - stale pickup receipts across GM reentry

A division-only GM relocation may replace a scene while its old pickup owns
the publication gate. Captured delivery must retain the original scene.

===========================================================================
*/
package action

import (
	"fmt"
	"sync"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
pickupPublicationPresence

Give the picker its own real transport session in party cases. Presence uses
the session's actual binding and WorldReady state, as production does.
================
*/
func pickupPublicationPresence(t *testing.T, h *publicationHarness, shared bool) *publicationConn {
	t.Helper()
	var peer *publicationConn
	if shared {
		var welcome transport.Welcome
		peer, welcome = h.connect(t, nil)
		session, _ := h.server.Hub.Session(welcome.SessionID)
		picker := h.rt.findCharacter(testDivision, "PickupPeer")
		session.BindCharacter(testDivision, picker.Name, enterworld.ObjectIDForCharacter(picker))
		h.ready(t, peer)
	}
	h.rt.RewardActorPresent = func(division, name string) bool {
		for _, session := range h.server.Hub.CharacterSessions(division, name) {
			if session.WorldReady() {
				return true
			}
		}
		return false
	}
	return peer
}

/*
================
TestPickupScenePublicationAfterGMWarp

Commit 2->3, pause receipt enqueue, then execute the real GM warp and refill
bootstrap to 50. Releasing the old receipt must not overwrite that snapshot.
================
*/
func TestPickupScenePublicationAfterGMWarp(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, readyAfterWarp := range []bool{false, true} {
			t.Run(fmt.Sprintf("party=%t/readyAfterWarp=%t", shared, readyAfterWarp), func(t *testing.T) {
				h := newPublicationHarness(t, true, 2)
				h.deps.LockPublication = h.rt.LockPublication
				refills := h.deps.StarterRefills
				h.deps.StarterRefills = nil
				conn, _ := h.connect(t, nil)
				if got := publicationBootstrapQuantity(t, h.enter(t, conn)); got != 2 {
					t.Fatalf("initial quantity %d", got)
				}
				h.ready(t, conn)
				armPublicationPickup(h, shared)
				pickupPublicationPresence(t, h, shared)
				h.deps.StarterRefills = refills
				h.character.GMPrivilege = true
				h.deps.CanEnterWorldRegion = func(_ *enterworld.Character, region uint16) bool { return region == 25416 }
				h.deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
				paused, release := make(chan struct{}), make(chan struct{})
				var pauseOnce, releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				t.Cleanup(unblock)
				pause := func(name string, frames []wire.Frame) {
					if name != h.character.Name {
						return
					}
					for _, frame := range frames {
						if frame.Opcode == wire.OpItemMoveResponse {
							pauseOnce.Do(func() { close(paused); <-release })
						}
					}
				}
				capture := h.rt.CaptureCharacterFrames
				if capture == nil {
					t.Fatal("shared harness must install scene capture")
				}
				h.rt.CaptureCharacterFrames = func(division, name string) func([]wire.Frame) {
					lane := h.rt.operations.lane(division, &h.rt.maintenance)
					if lane.TryLock() {
						lane.Unlock()
						t.Error("recipient capture ran outside division lock")
					}
					// Store access must be available even though the lane is retained.
					h.deps.Read(division, func() {})
					publish := capture(division, name)
					if publish == nil {
						return nil
					}
					return func(frames []wire.Frame) { pause(name, frames); publish(frames) }
				}
				h.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
					// The original implementation ignores capture and uses this path.
					pause(name, frames)
					for _, target := range h.server.Hub.CharacterSessions(division, name) {
						SendFrames(target, frames)
					}
				}
				done := make(chan struct{})
				go func() {
					// Preserve the original tick's returned-recipient delivery path
					// when running with the pre-fix lifecycle overlay.
					for _, batch := range h.rt.advancePendingPickups(h.clock.NowMs()) {
						if batch.OnlyCharacterID != h.character.ID {
							continue
						}
						frames := make([]wire.Frame, len(batch.Frames))
						for i, frame := range batch.Frames {
							frames[i] = wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload}
						}
						h.rt.PushCharacterFrames(testDivision, h.character.Name, frames)
					}
					close(done)
				}()
				t.Cleanup(func() {
					unblock()
					wait.Eventually(t, publicationTimeout, "pickup released", func() bool { return lenSignal(done) })
				})
				wait.Eventually(t, publicationTimeout, "committed pickup awaiting enqueue", func() bool { return lenSignal(paused) })
				if got := h.character.MissionInventory[0].StackCount; got != 3 {
					t.Fatalf("committed quantity %d, want 3", got)
				}
				warped := make(chan bool, 1)
				go func() {
					warped <- h.rt.WarpGM(testDivision, h.character.Name, wire.Position{RegionID: 25416, X: 703, Y: 42, Z: 1575})
				}()
				wait.Eventually(t, publicationTimeout, "GM warp completes while publication paused", func() bool { return len(warped) == 1 })
				if !<-warped {
					t.Fatal("GM warp refused")
				}
				conn.in <- transport.Frame{Opcode: publicationFence}
				if got := publicationBootstrapQuantity(t, publicationThrough(t, conn, publicationFence)); got != 50 {
					t.Fatalf("warp bootstrap %d, want 50", got)
				}
				// Even becoming ready in the replacement scene must not revive an old capture.
				if readyAfterWarp {
					h.ready(t, conn)
				}
				unblock()
				wait.Eventually(t, publicationTimeout, "old publisher returned", func() bool { return lenSignal(done) })
				conn.in <- transport.Frame{Opcode: publicationFence}
				for _, frame := range publicationThrough(t, conn, publicationFence) {
					if frame.Opcode == wire.OpItemMoveResponse {
						t.Fatalf("old pickup receipt escaped after replacement bootstrap: %x", frame.Payload)
					}
				}
				if h.character.MissionInventory[0].StackCount != 50 {
					t.Fatal("warp authority no longer agrees with replacement bootstrap")
				}
			})
		}
	}
}
