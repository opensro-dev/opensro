/*
===========================================================================

wall_retirement_test.go - a standing Fire Wall releases its movement lock

BR-261007-2003-4B1D showed a caster clicking the ground inside Fire Wall.
Native 4EF880 -> 4AAB40 refuses those moves while the wall stands. Each
retirement path must also publish the token teardown that frees the client.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestFireWallRetirementReleasesMovementAndPublishesTeardown
================
*/
func TestFireWallRetirementReleasesMovementAndPublishesTeardown(t *testing.T) {
	for _, reason := range []string{"cancel", "mana", "broken"} {
		t.Run(reason, func(t *testing.T) {
			rt, clock, c, mob := wallFixture(t, fireWallA1, 9)
			wall, ok := rt.standingWallOf(testDivision, c.Name)
			if !ok || !rt.PlayerAttackLocked(testDivision, c.Name) {
				t.Fatal("a standing Fire Wall must hold its caster")
			}
			switch reason {
			case "cancel":
				rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{
					EffectID: fireWallA1, InstanceToken: wall.token,
				}.Encode())
			case "mana":
				c.CurrentMP = testInt64(0)
				clock.Advance(5 * time.Second)
			case "broken":
				rt.wallMu.Lock()
				rt.walls[wallKey(testDivision, c.Name)].pool = 1
				rt.wallMu.Unlock()
				wallHit(t, rt, clock, c, mob)
			}
			ended := 0
			for _, burst := range rt.TickHook()(clock.NowMs()) {
				for _, frame := range burst.Frames {
					if frame.Opcode != wire.OpEndedEffectInstances || len(frame.Payload) == 0 {
						continue
					}
					for i := 0; i < int(frame.Payload[0]); i++ {
						if binary.LittleEndian.Uint32(frame.Payload[1+4*i:]) == wall.token {
							ended++
						}
					}
				}
			}
			if rt.PlayerAttackLocked(testDivision, c.Name) || hasSkillEffect(rt, c.Name, fireWallA1) {
				t.Fatal("retired Fire Wall still holds its caster")
			}
			if ended != 1 {
				t.Fatalf("wall token teardown count = %d, want 1", ended)
			}
		})
	}
}
