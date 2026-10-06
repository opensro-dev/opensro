/*
===========================================================================

skillmovement_test.go - movement skill activation, replacement and retirement

Exercise the complete action lifecycle and bind qualification receipts to
the tested inputs when the evidence runner requests one.

===========================================================================
*/
package action

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
================
TestDirectMovementSkillCompleteLifecycle
================
*/
func TestDirectMovementSkillCompleteLifecycle(t *testing.T) {
	// Receipt is emitted only after every lifecycle subtest passes. It binds
	// the production inventory (including source hashes), not a mutable label.
	t.Cleanup(func() {
		output := os.Getenv("SRO_MOVEMENT_QUALIFICATION_OUT")
		if output == "" || t.Failed() {
			return
		}
		input, err := os.ReadFile(os.Getenv("SRO_SKILL_COVERAGE_INPUT"))
		if err != nil {
			t.Fatal(err)
		}
		var inventory struct {
			Hashes map[string]string `json:"sha256"`
		}
		if err = json.Unmarshal(input, &inventory); err != nil {
			t.Fatal(err)
		}
		if len(inventory.Hashes) == 0 {
			t.Fatal("missing inventory source binding")
		}
		for label, want := range inventory.Hashes {
			if !filepath.IsLocal(label) {
				t.Fatal("nonlocal source binding")
			}
			source := filepath.Join("..", label)
			if strings.HasPrefix(label, "data/") {
				source = filepath.Join(os.Getenv("SRO_SKILL_INVENTORY_DATA"), strings.TrimPrefix(label, "data/"))
			}
			bytes, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256(bytes)) != want {
				t.Fatalf("stale source binding %s", label)
			}
		}
		row := shippedOffense(t, "SKILL_CH_LIGHTNING_GYEONGGONG_A_01")
		receipt := struct {
			Schema string   `json:"schema"`
			Input  string   `json:"input_sha256"`
			Kind   string   `json:"kind"`
			Test   string   `json:"test"`
			Passed bool     `json:"passed"`
			Rows   []uint32 `json:"rows"`
		}{"sro-skill-qualification-v1", fmt.Sprintf("%x", sha256.Sum256(input)), "server-integration", t.Name(), true, []uint32{row.ID}}
		b, err := json.MarshalIndent(receipt, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, append(b, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	})

	for _, mode := range []string{"expiry", "cancel", "disconnect", "death", "recast", "unlearned", "target", "mp"} {
		t.Run(mode, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			row := shippedOffense(t, "SKILL_CH_LIGHTNING_GYEONGGONG_A_01")
			if !row.InstantSelfEffectPinned || row.MovementModifier.Persistent {
				t.Fatal("production activation not admitted", row)
			}
			rt.deps.SkillData().(staticSkillSource)[row.ID] = row
			c.Skills = append(c.Skills, row.ID)
			c.CurrentMP = testInt64(1000)
			request := wire.SkillAction{ActionId: row.ID}
			if mode == "unlearned" {
				c.Skills = nil
			}
			if mode == "target" {
				request.HasTarget = true
				request.TargetGid = target.Gid
			}
			if mode == "mp" {
				c.CurrentMP = testInt64(0)
			}
			before := *c.CurrentMP
			result := rt.HandleTargetInteract(testDivision, c, request.Encode())
			effects := rt.effects.Snapshot(testDivision, c.Name)
			if mode == "unlearned" || mode == "target" || mode == "mp" {
				if len(effects) != 0 || *c.CurrentMP != before {
					t.Fatal("refusal mutated authority", result)
				}
				return
			}
			if len(effects) != 1 || !effects[0].Movement || effects[0].MovementPercent != row.MovementModifier.Percent || effects[0].Imbue || *c.CurrentMP >= before || len(c.TimedSkillJobs) != 0 {
				t.Fatal("activation ownership", effects, result)
			}
			_, run := rt.EntryMovementSpeeds(testDivision, c.Name)
			want := float32(simulation.RunSpeed) * (1 + float32(row.MovementModifier.Percent)/100)
			if run != want || rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("instant speed/queue", run, want)
			}
			foundAttach, foundSpeed := false, false
			for _, f := range result.Broadcast {
				foundAttach = foundAttach || f.Opcode == wire.OpAttachedEffect
				foundSpeed = foundSpeed || f.Opcode == 0x376f
			}
			if !foundAttach || !foundSpeed {
				t.Fatal("peer effect or speed missing", result)
			}
			for _, f := range result.Frames {
				if f.Opcode == wire.OpSkillCastResult && f.Payload[0] != 1 {
					t.Fatal("cast refused")
				}
			}
			mp := *c.CurrentMP
			rt.HandleTargetInteract(testDivision, c, request.Encode())
			if *c.CurrentMP != mp || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
				t.Fatal("cooldown duplicated effect")
			}
			switch mode {
			case "recast":
				if row.CoolTimeMs == 0 || row.EffectDurationMs <= row.CoolTimeMs {
					t.Fatalf("skill cannot be refreshed while active: cooldown=%d duration=%d", row.CoolTimeMs, row.EffectDurationMs)
				}
				clock.Advance(time.Duration(row.CoolTimeMs) * time.Millisecond)
				refreshed := rt.HandleTargetInteract(testDivision, c, request.Encode())
				pending := rt.effects.Snapshot(testDivision, c.Name)
				if len(pending) != 1 || pending[0].StopRequested ||
					pending[0].InstanceToken == effects[0].InstanceToken || pending[0].MovementPercent != row.MovementModifier.Percent {
					t.Fatal("recast replacement ownership", pending, refreshed)
				}
				newToken := pending[0].InstanceToken
				_, run = rt.EntryMovementSpeeds(testDivision, c.Name)
				if run != want {
					t.Fatal("recast changed effective run speed before retirement", run, want)
				}
				attached, retiredOldToken := false, false
				for _, frame := range refreshed.Broadcast {
					attached = attached || frame.Opcode == wire.OpAttachedEffect
					if frame.Opcode == wire.OpEndedEffectInstances {
						ended, err := wire.DecodeEndedEffectInstances(frame.Payload)
						if err != nil || len(ended.InstanceTokens) != 1 {
							t.Fatal("decode recast retirement", ended, err)
						}
						retiredOldToken = ended.InstanceTokens[0] == effects[0].InstanceToken
					}
				}
				if !attached {
					t.Fatal("recast did not publish replacement buff", refreshed)
				}
				retirement := rt.TickHook()(clock.NowMs() + 1)
				emittedSpeed := false
				for _, batch := range retirement {
					for _, frame := range batch.Frames {
						emittedSpeed = emittedSpeed || frame.Opcode == 0x376f
						if frame.Opcode == wire.OpEndedEffectInstances {
							t.Fatal("replacement retired twice")
						}
					}
				}
				remaining := rt.effects.Snapshot(testDivision, c.Name)
				_, run = rt.EntryMovementSpeeds(testDivision, c.Name)
				if !retiredOldToken || emittedSpeed || len(remaining) != 1 || remaining[0].InstanceToken != newToken ||
					remaining[0].StopRequested || remaining[0].MovementPercent != row.MovementModifier.Percent || run != want {
					t.Fatal("recast lost the replacement across retirement tick", remaining, run, retirement)
				}
				// Let the refreshed instance expire so this subtest also proves the
				// ordinary teardown path restores the unbuffed speed.
				rt.TickHook()(remaining[0].ExpiresAtMs + 1)
			case "expiry":
				rt.TickHook()(clock.NowMs() + int64(row.EffectDurationMs) + 1)
			case "cancel":
				rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: row.ID, InstanceToken: effects[0].InstanceToken}.Encode())
				rt.TickHook()(clock.NowMs() + 1)
			case "disconnect":
				rt.ForgetCharacter(testDivision, c.Name)
				clock.Advance(time.Hour)
				rt.RestoreTimedSkillJobs(testDivision, c.Name)
			case "death":
				c.CurrentHP = testInt64(0)
				rt.retireBodyEffectsOnDeath(testDivision, c)
			}
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || len(rt.EntrySkills(testDivision, c.Name)) != 0 || len(c.TimedSkillJobs) != 0 {
				t.Fatal("retired ordinary buff restored", mode)
			}
			_, run = rt.EntryMovementSpeeds(testDivision, c.Name)
			if run != float32(simulation.RunSpeed) {
				t.Fatal("speed not restored", mode, run)
			}
		})
	}
}
