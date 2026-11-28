/*
===========================================================================

cast_close_anchor_test.go - server tick anchoring for prepared cast close

Verifies the v1.188 reference server rule that a cast's action is timed
from its start, not from its release: cast close (0xB505 mode 2) and queued skill promotion anchor to
castStart + ActionCastingTimeMs + ActionDurationMs instead of adding
ActionDurationMs on top of the overrun release tick.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func setupWizardChainRuntime(t *testing.T, primaryCodename, queuedCodename string) (
	*Runtime, *fakeClock, *enterworld.Character, monster.Instance, enterworld.SkillRow, enterworld.SkillRow,
) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1_000_000)
	clock.now = time.UnixMilli(1_000_000)

	primary := shippedOffense(t, primaryCodename)
	queued := shippedOffense(t, queuedCodename)
	skills := rt.deps.SkillData().(staticSkillSource)
	skills[primary.ID] = primary
	skills[queued.ID] = queued

	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = append(c.Skills, primary.ID, queued.ID)
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)

	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(primary.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()

	return rt, clock, c, target, primary, queued
}

func extractSkillCastOpenToken(t *testing.T, frames []wire.Frame, skillID uint32) (uint32, bool) {
	t.Helper()
	for _, f := range frames {
		if f.Opcode == wire.OpSkillCastResult && len(f.Payload) >= 14 && f.Payload[0] == 1 {
			if binary.LittleEndian.Uint32(f.Payload[2:6]) == skillID {
				return binary.LittleEndian.Uint32(f.Payload[10:14]), true
			}
		}
	}
	return 0, false
}

func hasSkillEffectModeForToken(routed []simulation.DivisionFrames, mode byte, token uint32) bool {
	for _, burst := range routed {
		for _, f := range burst.Frames {
			if f.Opcode != opSkillEffectCtrlB505 || len(f.Payload) < 6 || f.Payload[0] != mode {
				continue
			}
			// Mode 1 carries the 4-byte token at [1:5]; mode 2 finalize (len 6) at [2:6].
			switch mode {
			case 0x01:
				if binary.LittleEndian.Uint32(f.Payload[1:5]) == token {
					return true
				}
			case 0x02:
				if len(f.Payload) == skillCastFinalizeLen && binary.LittleEndian.Uint32(f.Payload[2:6]) == token {
					return true
				}
			}
		}
	}
	return false
}

func findCastOpenInBursts(routed []simulation.DivisionFrames, skillID uint32) bool {
	for _, burst := range routed {
		for _, f := range burst.Frames {
			if f.Opcode == wire.OpSkillCastResult && len(f.Payload) >= 14 &&
				f.Payload[0] == 1 && binary.LittleEndian.Uint32(f.Payload[2:6]) == skillID {
				return true
			}
		}
	}
	return false
}

/*
================
TestCastCloseAnchorsToStartAcrossWizardChains

With 100 ms server ticks, release falls on start + cast + 100 ms when cast is
nonzero. Anchoring close to castStart + cast + duration absorbs that 100 ms
tick overrun so the cast closes and promotes the queued spell on the exact
expected end tick instead of slipping +100 ms per spell.
================
*/
func TestCastCloseAnchorsToStartAcrossWizardChains(t *testing.T) {
	cases := []struct {
		name           string
		primary        string
		queued         string
		wantCastMs     uint32
		wantDurationMs uint32
	}{
		{
			name:           "wizard-area-meteor-into-earth-bolt",
			primary:        "SKILL_EU_WIZARD_FIREA_POINT_B_01",
			queued:         "SKILL_EU_WIZARD_EARTHA_POINT_A_01",
			wantCastMs:     1500,
			wantDurationMs: 1500,
		},
		{
			name:           "wizard-single-fire-bolt-into-earth-bolt",
			primary:        "SKILL_EU_WIZARD_FIREA_POINT_A_01",
			queued:         "SKILL_EU_WIZARD_EARTHA_POINT_A_01",
			wantCastMs:     1000,
			wantDurationMs: 1500,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, target, primary, queued := setupWizardChainRuntime(t, tc.primary, tc.queued)
			if primary.ActionCastingTimeMs != tc.wantCastMs || primary.ActionDurationMs != tc.wantDurationMs {
				t.Fatalf("%s authored cast/duration = %d/%d, want %d/%d",
					tc.primary, primary.ActionCastingTimeMs, primary.ActionDurationMs, tc.wantCastMs, tc.wantDurationMs)
			}

			startMs := clock.NowMs()
			// Put caster right next to the target so projectile flight is instant.
			x := target.Spawn.X - 5
			c.World.Spawn.X = &x

			openRes := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{
				ActionId:  primary.ID,
				HasTarget: true,
				TargetGid: target.Gid,
			}.Encode())
			token, ok := extractSkillCastOpenToken(t, openRes.Frames, primary.ID)
			if !ok {
				t.Fatalf("primary cast %s did not open: %+v", tc.primary, openRes)
			}

			wantReleaseTick := startMs + int64(primary.ActionCastingTimeMs) + 100
			wantCloseTick := startMs + int64(primary.ActionCastingTimeMs) + int64(primary.ActionDurationMs)

			var gotReleaseTick int64
			var gotCloseTick int64
			var gotQueuedOpenTick int64
			queuedPressed := false

			// Step 100 ms server ticks from cast start past expected close.
			for tick := int64(100); tick <= int64(primary.ActionCastingTimeMs)+int64(primary.ActionDurationMs)+500; tick += 100 {
				clock.Advance(100 * time.Millisecond)
				now := clock.NowMs()
				if !queuedPressed && now == startMs+500 {
					queuedPressed = true
					qRes := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{
						ActionId:  queued.ID,
						HasTarget: true,
						TargetGid: target.Gid,
					}.Encode())
					assertQueuedAction(t, qRes)
				}

				routed := rt.TickHook()(now)
				if hasSkillEffectModeForToken(routed, 0x01, token) {
					if gotReleaseTick != 0 {
						t.Fatalf("duplicate release mode 1 at %d (first %d)", now, gotReleaseTick)
					}
					gotReleaseTick = now
				}
				if hasSkillEffectModeForToken(routed, 0x02, token) {
					if gotCloseTick != 0 {
						t.Fatalf("duplicate close mode 2 at %d (first %d)", now, gotCloseTick)
					}
					gotCloseTick = now
				}
				if findCastOpenInBursts(routed, queued.ID) {
					if gotQueuedOpenTick != 0 {
						t.Fatalf("duplicate queued cast open at %d (first %d)", now, gotQueuedOpenTick)
					}
					gotQueuedOpenTick = now
				}
			}

			if gotReleaseTick != wantReleaseTick {
				t.Fatalf("release tick = %d, want %d (start %d + cast %d + 100 ms tick)",
					gotReleaseTick, wantReleaseTick, startMs, primary.ActionCastingTimeMs)
			}
			if gotCloseTick != wantCloseTick {
				t.Fatalf("close tick = %d, want %d (anchored to start %d + cast %d + duration %d)",
					gotCloseTick, wantCloseTick, startMs, primary.ActionCastingTimeMs, primary.ActionDurationMs)
			}
			if gotQueuedOpenTick != wantCloseTick {
				t.Fatalf("queued spell open tick = %d, want %d (same tick as anchored close)",
					gotQueuedOpenTick, wantCloseTick)
			}
		})
	}
}

/*
================
TestCastCloseAtMsUnitCases

Covers nil receiver, overrun absorption, zero duration, stalled release clamping
to nowMs, missing startedAtMs fallback from releaseAtMs, and long projectile
flight extending past anchored close.
================
*/
func TestCastCloseAtMsUnitCases(t *testing.T) {
	t.Run("nil-receiver", func(t *testing.T) {
		var p *pendingProjectileCast
		if got := p.castCloseAtMs(5000, 1000, 1500); got != 7500 {
			t.Fatalf("nil receiver close = %d, want 7500", got)
		}
	})

	t.Run("overrun-absorption", func(t *testing.T) {
		p := &pendingProjectileCast{startedAtMs: 1_000_000, releaseAtMs: 1_001_501}
		if got := p.castCloseAtMs(1_001_600, 1500, 1500); got != 1_003_000 {
			t.Fatalf("overrun close = %d, want 1003000", got)
		}
	})

	t.Run("zero-duration-returns-now", func(t *testing.T) {
		p := &pendingProjectileCast{startedAtMs: 1_000_000, releaseAtMs: 1_000_001}
		if got := p.castCloseAtMs(1_000_100, 0, 0); got != 1_000_100 {
			t.Fatalf("zero duration close = %d, want 1000100", got)
		}
	})

	t.Run("stalled-release-clamps-to-now", func(t *testing.T) {
		p := &pendingProjectileCast{startedAtMs: 1_000_000, releaseAtMs: 1_001_001}
		if got := p.castCloseAtMs(1_004_000, 1000, 1500); got != 1_004_000 {
			t.Fatalf("stalled release close = %d, want 1004000", got)
		}
	})

	t.Run("fallback-from-releaseAtMs", func(t *testing.T) {
		p := &pendingProjectileCast{releaseAtMs: 1_001_501}
		if got := p.castCloseAtMs(1_001_600, 1500, 1500); got != 1_003_000 {
			t.Fatalf("fallback close = %d, want 1003000", got)
		}
	})

	t.Run("projectile-flight-extends-past-anchored-close", func(t *testing.T) {
		rt, clock, c, target, primary, _ := setupWizardChainRuntime(t,
			"SKILL_EU_WIZARD_FIREA_POINT_A_01", "SKILL_EU_WIZARD_EARTHA_POINT_A_01")
		// Short recovery duration + slow projectile + far target so flight
		// lasts longer than anchored close recovery.
		primary.ActionDurationMs = 100
		primary.ActionRange = 180
		primary.ActionRangePinned = true
		primary.ActionHandler = enterworld.SkillActionProjectile
		primary.ProjectileSpeed = 200
		rt.deps.SkillData().(staticSkillSource)[primary.ID] = primary

		x, y, z := target.Spawn.X-150, target.Spawn.Y, target.Spawn.Z
		c.World.Spawn.X = &x
		c.World.Spawn.Y = &y
		c.World.Spawn.Z = &z

		startMs := clock.NowMs()
		openRes := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{
			ActionId:  primary.ID,
			HasTarget: true,
			TargetGid: target.Gid,
		}.Encode())
		token, ok := extractSkillCastOpenToken(t, openRes.Frames, primary.ID)
		if !ok {
			t.Fatalf("fire bolt did not open: %+v", openRes)
		}

		anchoredClose := startMs + int64(primary.ActionCastingTimeMs) + int64(primary.ActionDurationMs)
		flightMs := int64(150 * 1000 / primary.ProjectileSpeed)
		var releaseTick, closeTick int64
		for tick := int64(100); tick <= 5000; tick += 100 {
			clock.Advance(100 * time.Millisecond)
			now := clock.NowMs()
			routed := rt.TickHook()(now)
			if hasSkillEffectModeForToken(routed, 0x01, token) && releaseTick == 0 {
				releaseTick = now
			}
			if hasSkillEffectModeForToken(routed, 0x02, token) && closeTick == 0 {
				closeTick = now
				break
			}
		}

		if releaseTick == 0 {
			t.Fatal("fire bolt never released")
		}
		if closeTick < releaseTick+flightMs || closeTick <= anchoredClose {
			t.Fatalf("cast close %d did not wait for flight %dms past release %d (anchored %d)",
				closeTick, flightMs, releaseTick, anchoredClose)
		}
	})
}
