package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestFollowRejectsEveryChangedGeometryAndHomeInput(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*monster.Instance)
	}{
		{"walk speed", func(a *monster.Instance) { a.Ref.WalkSpeed++ }},
		{"own body radius", func(a *monster.Instance) { a.Ref.BodyRadius++ }},
		{"nest attachment", func(a *monster.Instance) { a.NestDetached = !a.NestDetached }},
		{"home tactics", func(a *monster.Instance) { a.Nest.Controls.HomingData++ }},
	} {
		t.Run(change.name, func(t *testing.T) {
			ops, _, child, now := followFixture(t)
			mover, _ := ops.Monsters.Mover("summon", child.Gid)
			plan, ok := ops.Monsters.prepareFollow("summon", &child, mover, now)
			if !ok {
				t.Fatal("fixture not admitted")
			}
			ops.Monsters.mu.Lock()
			state := ops.Monsters.populationForObject("summon", child.Gid)
			changed := state.instances.get(child.Gid)
			change.apply(&changed)
			state.instances.set(child.Gid, changed)
			ops.Monsters.mu.Unlock()
			if _, accepted := ops.Monsters.commitFollow(plan, mover, nil); accepted {
				t.Fatal("stale callback committed")
			}
			if _, accepted := ops.Monsters.prepareFollow("summon", &child, mover, now); accepted {
				t.Fatal("stale input admitted")
			}
		})
	}
}
