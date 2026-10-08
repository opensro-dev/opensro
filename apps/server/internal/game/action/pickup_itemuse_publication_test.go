/*
===========================================================================

pickup_itemuse_publication_test.go - inventory commit and receipt ordering

Uses the shared real-transport publication harness. Barriers stop a producer
after commit and before enqueue; the competing producer must not overtake it.
The direct runtime API remains transport-free.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const pickupPublicationWindow = 100 * time.Millisecond

/*
================
armPublicationPickup

Arm an already-arrived approach, leaving completion to the real tick owner.
The party case awards the potion to the connected member, not the picker.
================
*/
func armPublicationPickup(h *publicationHarness, shared bool) {
	picker := h.character
	if shared {
		picker = h.character.Snapshot()
		picker.ID, picker.Name = 4, "PickupPeer"
		picker.MissionInventory = nil
		h.deps.Characters = enterworld.StaticCharacterSource{testDivision: {h.character, picker}}
		members := []uint32{enterworld.ObjectIDForCharacter(picker), enterworld.ObjectIDForCharacter(h.character)}
		h.rt.RewardParties = func(string) []RewardParty {
			return []RewardParty{{Order: 1, Options: sharedLootOption, Members: members}}
		}
		h.rt.NextPartyLootMember = func(string, string) uint32 { return members[1] }
	}
	row := h.character.MissionInventory[0]
	pose := simulation.SeedWorldState(picker).Spawn
	heap := h.rt.Ground.Add(testDivision, grounditem.Item{
		RefObjID: row.RefObjID, Codename: row.Codename, TypeFlags: row.TypeFlags, StackCount: 1,
		Position: grounditem.Point{RegionID: pose.RegionID, X: float32(pose.X), Z: float32(pose.Z)},
		Y:        float32(pose.Y),
	})
	h.rt.Pending.ArmOwned("publication-pickup", testDivision, picker.Name, heap.Gid, h.clock.Now())
}

/*
================
TestPickupItemUsePublicationOrder

The injected operation is the real item-use authority plus a commit barrier;
hubHandler owns the same publication path installed by Runtime.Register.
================
*/
func TestPickupItemUsePublicationOrder(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, pickupFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("party=%t/pickupFirst=%t", shared, pickupFirst), func(t *testing.T) {
				initial := int64(50)
				if pickupFirst {
					initial = 49
				}
				h := newPublicationHarness(t, false, initial)
				paused, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				t.Cleanup(unblock)
				useStarted, useCommitted := make(chan struct{}), make(chan struct{})
				handler := h.rt.hubHandler(h.server.Hub, func(division string, c *enterworld.Character, payload []byte) OpResult {
					result := h.rt.HandleItemUse(division, c, payload)
					close(useCommitted)
					if !pickupFirst {
						close(paused)
						<-release
					}
					return result
				})
				h.server.Hub.Handle(wire.OpItemUseRequest, func(s *transport.Session, opcode uint16, payload []byte) {
					close(useStarted)
					handler(s, opcode, payload)
				})
				conn, welcome := h.connect(t, nil)
				if quantity := publicationBootstrapQuantity(t, h.enter(t, conn)); int64(quantity) != initial {
					t.Fatalf("initial quantity %d, want %d", quantity, initial)
				}
				session, _ := h.server.Hub.Session(welcome.SessionID)
				session.TryMarkWorldReady()
				armPublicationPickup(h, shared)
				var pausePickup sync.Once
				h.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
					if pickupFirst {
						pausePickup.Do(func() { close(paused); <-release })
					}
					for _, target := range h.server.Hub.CharacterSessions(division, name) {
						SendFrames(target, frames)
					}
				}
				pickupStarted, pickupDone := make(chan struct{}), make(chan struct{})
				t.Cleanup(func() {
					unblock()
					if lenSignal(pickupStarted) {
						wait.Eventually(t, publicationTimeout, "pickup goroutine released", func() bool { return lenSignal(pickupDone) })
					}
				})
				pickup := func() {
					close(pickupStarted)
					// Mirror the tick's recipient route on the original implementation.
					// The fixed path enqueues these before returning from completion.
					for _, batch := range h.rt.advancePendingPickups(h.clock.NowMs()) {
						if batch.OnlyCharacterID == h.character.ID {
							frames := make([]wire.Frame, len(batch.Frames))
							for i, frame := range batch.Frames {
								frames[i] = wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}
							}
							SendFrames(session, frames)
						}
					}
					close(pickupDone)
				}
				if pickupFirst {
					go pickup()
				} else {
					conn.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
				}
				wait.Eventually(t, publicationTimeout, "first producer paused after commit", func() bool { return lenSignal(paused) })
				if pickupFirst {
					conn.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
					wait.Eventually(t, publicationTimeout, "competing use entered adapter", func() bool { return lenSignal(useStarted) })
					wait.Consistently(t, pickupPublicationWindow, "use waits for pickup publication", func() bool { return !lenSignal(useCommitted) })
				} else {
					go pickup()
					wait.Eventually(t, publicationTimeout, "competing pickup started", func() bool { return lenSignal(pickupStarted) })
					wait.Consistently(t, pickupPublicationWindow, "pickup waits for use publication", func() bool { return !lenSignal(pickupDone) })
				}
				unblock()
				wait.Eventually(t, publicationTimeout, "pickup completed", func() bool { return lenSignal(pickupDone) })
				conn.in <- transport.Frame{Opcode: publicationFence}
				frames := publicationThrough(t, conn, publicationFence)
				quantity, receipts := uint16(initial), 0
				for _, frame := range frames {
					switch frame.Opcode {
					case wire.OpItemMoveResponse:
						if len(frame.Payload) != 9 || frame.Payload[0] != 1 || frame.Payload[1] != wire.MoveTypePickup || frame.Payload[2] != publicationSlot {
							t.Fatalf("unexpected pickup receipt %x", frame.Payload)
						}
						quantity = binary.LittleEndian.Uint16(frame.Payload[7:])
						receipts++
					case wire.OpItemUseResponse:
						if len(frame.Payload) != 6 || frame.Payload[0] != 1 || binary.LittleEndian.Uint16(frame.Payload[2:]) != quantity-1 {
							t.Fatalf("stale use receipt %x against client quantity %d", frame.Payload, quantity)
						}
						quantity--
						receipts++
					}
				}
				if receipts != 2 || h.character.MissionInventory[0].StackCount != int64(quantity) || int64(quantity) != initial {
					t.Fatalf("receipts=%d client=%d server=%d, want final %d", receipts, quantity, h.character.MissionInventory[0].StackCount, initial)
				}
			})
		}
	}
}

/*
================
TestPickupItemUsePublicationOverflowClosesOutsideDivision

A detached authenticated session retains its bounded reliable queue. Fill it
exactly, then publish through each production boundary. The synchronous close
hook uses the same commerce cleanup as production, which reacquires the lane.
================
*/
func TestPickupItemUsePublicationOverflowClosesOutsideDivision(t *testing.T) {
	for _, producer := range []string{"itemuse", "pickup", "party-pickup"} {
		t.Run(producer, func(t *testing.T) {
			h := newPublicationHarness(t, false, 49)
			conn, welcome := h.connect(t, nil)
			h.enter(t, conn)
			session, _ := h.server.Hub.Session(welcome.SessionID)
			session.TryMarkWorldReady()
			h.rt.BeginCommerceSession(testDivision, h.character, session.ID)
			closed := make(chan error, 1)
			h.server.Hub.OnSessionClose(func(s *transport.Session, cause error) {
				h.rt.EndCommerceSession(testDivision, h.character, s.ID)
				closed <- cause
			})
			if err := conn.Close("retain queue for overflow test"); err != nil {
				t.Fatal(err)
			}
			wait.Eventually(t, publicationTimeout, "session detached", func() bool {
				return h.server.Hub.Metrics().DetachedSessions == 1
			})
			queue := make([]transport.Frame, transport.DefaultConfig().OutboundQueue)
			for i := range queue {
				queue[i].Opcode = publicationFence
			}
			if err := session.SendBatch(queue); err != nil {
				t.Fatalf("fill reliable queue: %v", err)
			}
			h.rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
				for _, target := range h.server.Hub.CharacterSessions(division, name) {
					SendFrames(target, frames)
				}
			}
			if producer != "itemuse" {
				armPublicationPickup(h, producer == "party-pickup")
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				if producer == "itemuse" {
					h.rt.hubHandler(h.server.Hub, h.rt.HandleItemUse)(session, wire.OpItemUseRequest, h.request)
					return
				}
				h.rt.advancePendingPickups(h.clock.NowMs())
			}()
			wait.Eventually(t, publicationTimeout, "overflow publisher returned", func() bool { return lenSignal(done) })
			select {
			case cause := <-closed:
				if cause == nil || !strings.Contains(cause.Error(), "outbound queue overflow") {
					t.Fatalf("close cause = %v, want queue overflow", cause)
				}
			default:
				t.Fatal("overflow did not finish synchronous close hook")
			}
			if h.character.BuybackSession != 0 {
				t.Fatal("commerce close hook did not clear session")
			}
		})
	}
}

/*
================
lenSignal

A nonblocking barrier observation; all timed waiting belongs to wait.
================
*/
func lenSignal(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	default:
		return false
	}
}
