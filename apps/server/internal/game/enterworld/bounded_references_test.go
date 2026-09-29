package enterworld

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"reflect"
	"testing"
)

func TestBoundedShippedReferencesPreserveEveryRowAndExecutionStage(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	skills := NewTextdataSkills(dir)
	if err := skills.Load(); err != nil {
		t.Fatal(err)
	}
	rows := make(map[uint32]SkillRow)
	plans := make(map[uint32]SkillExecutionPlan)
	for id, row := range skills.rows.values() {
		rows[id] = row
		plans[id] = skills.ExecutionPlan(id)
	}
	if err := skills.UseBoundedCache(16); err != nil {
		t.Fatal(err)
	}
	defer skills.Close()
	if len(skills.rows.hot) != 0 {
		t.Fatal("full skill table still resident")
	}
	for id, want := range rows {
		got, ok := skills.SkillByID(id)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("skill %d changed after eviction", id)
		}
		p, w := skills.ExecutionPlan(id), plans[id]
		if p.Kind() != w.Kind() || p.Len() != w.Len() {
			t.Fatalf("skill plan %d changed", id)
		}
		for i := 0; i < p.Len(); i++ {
			if !reflect.DeepEqual(p.Stage(i), w.Stage(i)) {
				t.Fatalf("skill stage %d/%d changed", id, i)
			}
		}
	}
	items := NewTextdataItems(dir)
	items.Len()
	before := make(map[string]*ItemRef, len(items.byCodename))
	for name, row := range items.byCodename {
		before[name] = row
	}
	if err := items.UseBoundedCache(16); err != nil {
		t.Fatal(err)
	}
	defer items.Close()
	if items.byCodename != nil || items.byID != nil {
		t.Fatal("full item table still resident")
	}
	for name, want := range before {
		got, ok := items.ItemRefByCodename(name)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("item %s changed", name)
		}
	}
}
