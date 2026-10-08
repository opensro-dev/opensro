/*
===========================================================================

pickup_entry_publication_test.go - delayed pickup versus scene replacement

The real resumed entry and pending pickup share the publication boundary.
Refills make snapshot ordering observable even when the pickup is absolute.

===========================================================================
*/
package action

import (
	"fmt"
	"sync"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
TestPickupEntryPublicationOrder

Start at two potions. Pickup-first publishes three before the resumed entry
optionally refills to fifty. An entry-first party member is still loading and
the ready picker receives the drop instead. Solo entry-first completes game
ready before pickup, including a new slot when the refill stack is full.
================
*/
func TestPickupEntryPublicationOrder(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, refill := range []bool{false, true} {
			for _, pickupFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("party=%t/refill=%t/pickupFirst=%t", shared, refill, pickupFirst), func(t *testing.T) {
					h := newPublicationHarness(t, refill, 2)
					h.deps.LockPublication = h.rt.LockPublication
					refills := h.deps.StarterRefills
					h.deps.StarterRefills = nil
					old, first := h.connect(t, nil)
					if got := publicationBootstrapQuantity(t, h.enter(t, old)); got != 2 {
						t.Fatalf("initial bootstrap = %d, want 2", got)
					}
					h.ready(t, old)
					armPublicationPickup(h, shared)
					peer := pickupPublicationPresence(t, h, shared)
					h.deps.StarterRefills = refills
					typeFlags := h.character.MissionInventory[0].TypeFlags
					paused, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					t.Cleanup(unblock)
					var pausePickup sync.Once
					capture := h.rt.CaptureCharacterFrames
					if capture == nil {
						t.Fatal("shared harness must install scene capture")
					}
					h.rt.CaptureCharacterFrames = func(division, name string) func([]wire.Frame) {
						publish := capture(division, name)
						if publish == nil {
							return nil
						}
						return func(frames []wire.Frame) {
							if pickupFirst {
								pausePickup.Do(func() { close(paused); <-release })
							}
							publish(frames)
						}
					}
					h.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
						if pickupFirst {
							pausePickup.Do(func() { close(paused); <-release })
						}
						for _, target := range h.server.Hub.CharacterSessions(division, name) {
							SendFrames(target, frames)
						}
					}
					if !pickupFirst {
						h.deps.CommunitySeedFramesFor = func(string, *enterworld.Character) []enterworld.Packet {
							close(paused)
							<-release
							return nil
						}
					}
					pickupStarted, pickupDone := make(chan struct{}), make(chan struct{})
					pickup := func() {
						close(pickupStarted)
						h.rt.advancePendingPickups(h.clock.NowMs())
						close(pickupDone)
					}
					t.Cleanup(func() {
						unblock()
						if lenSignal(pickupStarted) {
							wait.Eventually(t, publicationTimeout, "pickup released", func() bool { return lenSignal(pickupDone) })
						}
					})
					if pickupFirst {
						go pickup()
						wait.Eventually(t, publicationTimeout, "pickup committed before enqueue", func() bool { return lenSignal(paused) })
					}
					current, resumed := h.connect(t, first.ResumeToken)
					if !resumed.Resumed || resumed.SessionID != first.SessionID {
						t.Fatal("entry did not resume the same session")
					}
					current.in <- transport.Frame{Opcode: transport.OpPing}
					publicationThrough(t, current, transport.OpPong)
					current.in <- transport.Frame{Opcode: transport.OpEnterWorld, Payload: transport.EncodeEnterWorld(h.auth.Entry(testDivision, h.character.Name))}
					if pickupFirst {
						wait.Consistently(t, pickupPublicationWindow, "entry waits for pickup publication", func() bool { return len(current.out) == 0 })
					} else {
						wait.Eventually(t, publicationTimeout, "entry snapshot built before enqueue", func() bool { return lenSignal(paused) })
						if shared {
							go pickup()
							wait.Eventually(t, publicationTimeout, "pickup entered tick", func() bool { return lenSignal(pickupStarted) })
							wait.Consistently(t, pickupPublicationWindow, "pickup waits for entry publication", func() bool { return !lenSignal(pickupDone) })
						}
					}
					unblock()
					var frames []transport.Frame
					if !shared && !pickupFirst {
						current.in <- transport.Frame{Opcode: publicationFence}
						frames = publicationThrough(t, current, publicationFence)
						h.ready(t, current)
						go pickup()
					}
					wait.Eventually(t, publicationTimeout, "pickup completed", func() bool { return lenSignal(pickupDone) })
					current.in <- transport.Frame{Opcode: publicationFence}
					frames = append(frames, publicationThrough(t, current, publicationFence)...)
					bootstrap := publicationBootstrapQuantity(t, frames)
					wantBootstrap := uint16(2)
					if pickupFirst {
						wantBootstrap = 3
					}
					if refill {
						wantBootstrap = 50
					}
					if bootstrap != wantBootstrap {
						t.Fatalf("bootstrap = %d, want %d", bootstrap, wantBootstrap)
					}
					entryIndex, pickupIndex := -1, -1
					var grant wire.ItemMoveResult
					for i, frame := range frames {
						if frame.Opcode == transport.OpEnterWorldResult {
							entryIndex = i
						}
						if frame.Opcode == wire.OpItemMoveResponse {
							if pickupIndex >= 0 {
								t.Fatal("duplicate pickup receipt")
							}
							var err error
							grant, err = wire.DecodeItemMoveResult(frame.Payload, typeFlags)
							if err != nil || grant.MovementType != wire.MoveTypePickup {
								t.Fatalf("pickup receipt %x: %v", frame.Payload, err)
							}
							pickupIndex = i
						}
					}
					if shared && !pickupFirst {
						if pickupIndex >= 0 {
							t.Fatal("loading party recipient received pickup")
						}
						peer.in <- transport.Frame{Opcode: publicationFence}
						found := false
						for _, frame := range publicationThrough(t, peer, publicationFence) {
							if frame.Opcode == wire.OpItemMoveResponse {
								grant, err := wire.DecodeItemMoveResult(frame.Payload, typeFlags)
								if err != nil || grant.MovementType != wire.MoveTypePickup || grant.Item.Quantity != 1 {
									t.Fatalf("picker fallback receipt %x: %v", frame.Payload, err)
								}
								found = true
							}
						}
						if !found || h.rt.findCharacter(testDivision, "PickupPeer").MissionInventory[0].StackCount != 1 || h.character.MissionInventory[0].StackCount != int64(bootstrap) {
							t.Fatal("loading recipient did not fall back to ready picker")
						}
						return
					}
					if entryIndex < 0 || pickupIndex < 0 || (pickupIndex < entryIndex) != pickupFirst {
						t.Fatalf("publication order: pickup=%d entry=%d pickupFirst=%t", pickupIndex, entryIndex, pickupFirst)
					}
					wantGrant := uint16(3)
					if !pickupFirst && refill {
						wantGrant = 1
					}
					if grant.Item.Quantity != wantGrant {
						t.Fatalf("pickup absolute = %d, want %d", grant.Item.Quantity, wantGrant)
					}
					clientTotal := int64(bootstrap)
					if !pickupFirst {
						if grant.PickupSlot == publicationSlot {
							clientTotal = int64(grant.Item.Quantity)
						} else {
							clientTotal += int64(grant.Item.Quantity)
						}
					}
					var serverTotal int64
					for _, row := range h.character.MissionInventory {
						serverTotal += row.StackCount
					}
					if clientTotal != serverTotal {
						t.Fatalf("client total %d differs from server %d", clientTotal, serverTotal)
					}
				})
			}
		}
	}
}

/*
================
TestPickupEntryLoadingSceneReceivesCommittedGrant

Do not send GameReady after the replacement snapshot. A retained solo
approach still commits, and a GM reentry retains WorldReady for party loot.
Private inventory receipts must survive loading in that same scene.
================
*/
func TestPickupEntryLoadingSceneReceivesCommittedGrant(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, gm := range []bool{false, true} {
			t.Run(fmt.Sprintf("party=%t/gm=%t", shared, gm), func(t *testing.T) {
				h := newPublicationHarness(t, false, 2)
				h.deps.LockPublication = h.rt.LockPublication
				conn, first := h.connect(t, nil)
				h.enter(t, conn)
				h.ready(t, conn)
				var peer *publicationConn
				if !gm {
					armPublicationPickup(h, shared)
					peer = pickupPublicationPresence(t, h, shared)
				}
				if h.rt.CaptureCharacterFrames == nil {
					t.Fatal("shared harness must install scene capture")
				}
				h.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
					for _, target := range h.server.Hub.CharacterSessions(division, name) {
						SendFrames(target, frames)
					}
				}
				var frames []transport.Frame
				if gm {
					h.character.GMPrivilege = true
					h.deps.CanEnterWorldRegion = func(*enterworld.Character, uint16) bool { return true }
					h.deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
					pose := simulation.SeedWorldState(h.character).Spawn
					if !h.rt.WarpGM(testDivision, h.character.Name, wire.Position{RegionID: pose.RegionID, X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z)}) {
						t.Fatal("GM reentry refused")
					}
					conn.in <- transport.Frame{Opcode: publicationFence}
					frames = publicationThrough(t, conn, publicationFence)
					// This is a new pickup while the GM replacement scene is
					// loading, not an approach cancelled by the relocation.
					armPublicationPickup(h, shared)
					peer = pickupPublicationPresence(t, h, shared)
				} else {
					var resumed transport.Welcome
					conn, resumed = h.connect(t, first.ResumeToken)
					if !resumed.Resumed {
						t.Fatal("transport did not resume")
					}
					frames = h.enter(t, conn)
				}
				if quantity := publicationBootstrapQuantity(t, frames); quantity != 2 {
					t.Fatalf("replacement bootstrap %d, want 2", quantity)
				}
				session, _ := h.server.Hub.Session(first.SessionID)
				_, active := session.SceneRevision()
				if active || session.WorldReady() != gm {
					t.Fatalf("loading flags active=%t worldReady=%t, GM=%t", active, session.WorldReady(), gm)
				}
				h.rt.advancePendingPickups(h.clock.NowMs())
				recipient, want := conn, uint16(3)
				if shared && !gm {
					recipient, want = peer, 1
				}
				recipient.in <- transport.Frame{Opcode: publicationFence}
				found := false
				for _, frame := range publicationThrough(t, recipient, publicationFence) {
					if frame.Opcode != wire.OpItemMoveResponse {
						continue
					}
					grant, err := wire.DecodeItemMoveResult(frame.Payload, h.character.MissionInventory[0].TypeFlags)
					if err != nil || grant.MovementType != wire.MoveTypePickup || grant.Item.Quantity != want {
						t.Fatalf("loading pickup receipt %x: %v", frame.Payload, err)
					}
					found = true
				}
				if !found {
					t.Fatalf("committed pickup lost while replacement scene is loading: worldReady=%t actor quantity=%d", session.WorldReady(), h.character.MissionInventory[0].StackCount)
				}
				if shared && !gm {
					if h.character.MissionInventory[0].StackCount != 2 || h.rt.findCharacter(testDivision, "PickupPeer").MissionInventory[0].StackCount != 1 {
						t.Fatal("resume loading fallback disagrees with inventories")
					}
				} else if h.character.MissionInventory[0].StackCount != int64(want) {
					t.Fatal("replacement snapshot plus loading receipt disagrees with inventory")
				}
			})
		}
	}
}
