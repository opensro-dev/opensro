package action

import (
	"encoding/json"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"strconv"
	"testing"
)

// Real authenticated fixture sessions exercise the production hub and reliable
// transport. Optional export lets the browser consume the actual received burst.
func TestAttachedEffectAuthenticatedTransport(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	skills := enterworld.NewTextdataSkills(dir)
	skillID := uint32(27)
	if value := os.Getenv("SRO_EFFECT_FIXTURE_SKILL"); value != "" {
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		skillID = uint32(id)
	}
	skill, ok := skills.SkillByID(skillID)
	if !ok || !skill.SpawnToken {
		t.Fatal("shipped attached effect missing")
	}
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{skill.ID: skill})
	srv := wireStartServer(t, rt)
	actor := wireConnect(t, srv, testDivision, c.Name)
	peer := wireConnect(t, srv, testDivision, "Observer")
	rt.PushCharacterFrames = func(d, n string, frames []wire.Frame) {
		session, _ := srv.Hub.Session(actor.sessionID)
		for _, f := range frames {
			if err := session.Send(f.Opcode, f.Payload); err != nil {
				t.Error(err)
			}
		}
	}
	rt.PushDivisionPeerFrames = func(d, n string, frames []wire.Frame) {
		session, _ := srv.Hub.Session(peer.sessionID)
		for _, f := range frames {
			if err := session.Send(f.Opcode, f.Payload); err != nil {
				t.Error(err)
			}
		}
	}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, skill.ID, 99, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 10000) {
		t.Fatal("effect application failed")
	}
	apply := actor.readFrame(t)
	peer.expectFrame(t, apply.Opcode, apply.Payload)
	if apply.Opcode != wire.OpAttachedEffect {
		t.Fatal("missing apply packet")
	}
	entry := rt.entrySkillsAt(testDivision, c.Name, 11000)
	if len(entry) != 1 || *entry[0].Remaining != skill.EffectDurationMs-1000 {
		t.Fatal("reentry reset effect duration")
	}
	actor.send(t, wire.OpTargetInteract, (wire.CancelActiveEffectRequest{EffectID: skill.ID, InstanceToken: 99}).Encode())
	actor.readFrame(t) // ordered native action-release acknowledgement
	ended := rt.TickHook()(12000)
	if len(ended) != 0 {
		t.Fatal("teardown must enqueue before the action owner releases retirement")
	}
	// The production owner delivered these; the test must not manufacture
	// transport publication from a detached tick result after token reuse.
	end := actor.readFrame(t)
	if end.Opcode == wire.OpBaseStats {
		// A row with defense writes (27 is SWORD_SHIELD's defp) first gets
		// the owner's private stat refresh; peers never see it.
		end = actor.readFrame(t)
	}
	peer.expectFrame(t, end.Opcode, end.Payload)
	if end.Opcode != wire.OpEndedEffectInstances || len(rt.entrySkillsAt(testDivision, c.Name, 13000)) != 0 {
		t.Fatal("teardown or reentry resurrected effect")
	}
	if path := os.Getenv("SRO_EFFECT_FIXTURE_OUT"); path != "" {
		var ref enterworld.SpawnSkillRow
		for _, row := range skills.SpawnSkillRows() {
			if row.ID == skill.ID {
				ref = row
				break
			}
		}
		value := map[string]any{"gid": enterworld.ObjectIDForCharacter(c), "name": c.Name, "reference": ref, "apply": map[string]any{"opcode": apply.Opcode, "payload": apply.Payload}, "entry": entry, "end": map[string]any{"opcode": end.Opcode, "payload": end.Payload}}
		raw, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
