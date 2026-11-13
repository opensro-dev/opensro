/*
===========================================================================

store_steps_test.go - a step marked additive is one, and only those enter
a read window

storeSteps (schema.go) classifies every schema step (#3092, part 3 of
#3082). These pins hold the classification against the frozen field
lists beside persisted_shape_test.go (testdata/<record>-fields-v<N>.txt):
a step marked additive may not drop or rename a field of the version
before it, and the read window may only carry an additive step whose
new fields the previous binary keeps on a rewrite. The lists start at
schema 18, so the steps before 19 stand on their log lines alone.

===========================================================================
*/
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fieldLists are one schema version's frozen field lists by record name.
type fieldLists map[string][]string

/*
================
frozenFieldLists

The version's lists for every strictly decoded record that has one;
false when no record has a list at that version.
================
*/
func frozenFieldLists(t *testing.T, version int) (fieldLists, bool) {
	t.Helper()
	lists := fieldLists{}
	for _, record := range strictRecords {
		raw, err := os.ReadFile(filepath.FromSlash(fmt.Sprintf(recordFieldsGolden, record.name, version)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var fields []string
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				fields = append(fields, line)
			}
		}
		lists[record.name] = fields
	}
	return lists, len(lists) > 0
}

/*
================
droppedFields

The fields of previous that current no longer carries, by record: what
an additive step may never do. A rename is a drop and an add.
================
*/
func droppedFields(previous, current fieldLists) []string {
	var dropped []string
	for record, fields := range previous {
		next, present := current[record]
		if !present {
			dropped = append(dropped, record+": the whole record")
			continue
		}
		for _, field := range fields {
			if !slices.Contains(next, field) {
				dropped = append(dropped, record+": "+field)
			}
		}
	}
	slices.Sort(dropped)
	return dropped
}

// topLevelField is a field path's record-level key.
var topLevelField = regexp.MustCompile(`^[^.\[]+`)

/*
================
nestedAdditions

current's new fields that sit inside an object the previous version
already had (a new key on job, an item row, a world point). The previous
binary keeps a record's unknown top-level fields on a rewrite (json.go,
#3090) but refuses an unknown nested one, so such a step cannot ride
inside a read window: the binary a revert lands on would not boot.
================
*/
func nestedAdditions(previous, current fieldLists) []string {
	var nested []string
	for record, fields := range current {
		known, present := previous[record]
		if !present {
			continue
		}
		for _, field := range fields {
			if slices.Contains(known, field) {
				continue
			}
			if top := topLevelField.FindString(field); top != field && slices.Contains(known, top) {
				nested = append(nested, record+": "+field)
			}
		}
	}
	slices.Sort(nested)
	return nested
}

/*
================
readWindowViolations

What keeps the step at version out of a read window headroom versions
wide: a non-additive step that does not skip past the window of the
step before it, or an additive one the previous binary could not keep.
headroom 0 admits nothing, so nothing is checked.
================
*/
func readWindowViolations(steps map[int]storeStep, version, headroom int, previous, current fieldLists) []string {
	if headroom == 0 {
		return nil
	}
	prior := 0
	for v := range steps {
		if v < version && v > prior {
			prior = v
		}
	}
	step := steps[version]
	if !step.additive {
		if version-prior <= headroom {
			return []string{fmt.Sprintf("non-additive step %d sits inside the read window of %d (headroom %d): it must skip to %d (%s)",
				version, prior, headroom, prior+headroom+1, step.why)}
		}
		return nil
	}
	var out []string
	for _, field := range droppedFields(previous, current) {
		out = append(out, "dropped "+field)
	}
	for _, field := range nestedAdditions(previous, current) {
		out = append(out, "nested "+field+": the previous binary refuses it")
	}
	return out
}

//============================================================================

/*
================
TestEveryStoreStepIsClassified

Every version from UpgradeFromVersion to CurrentVersion has a class, so
a new schema step cannot land unclassified. A missing version is allowed
only right before a non-additive step: the versions it skipped past an
older binary's read window.
================
*/
func TestEveryStoreStepIsClassified(t *testing.T) {
	t.Parallel()

	if _, ok := storeSteps[CurrentVersion]; !ok {
		t.Fatalf("schema %d has no storeSteps entry: classify it in schema.go beside its log line", CurrentVersion)
	}
	if _, ok := storeSteps[UpgradeFromVersion]; !ok {
		t.Fatalf("the oldest upgradable schema %d has no class", UpgradeFromVersion)
	}
	for version, step := range storeSteps {
		if version < UpgradeFromVersion || version > CurrentVersion {
			t.Fatalf("storeSteps classifies schema %d outside %d..%d", version, UpgradeFromVersion, CurrentVersion)
		}
		if strings.TrimSpace(step.why) == "" {
			t.Fatalf("schema %d is classified without a reason", version)
		}
	}
	for version := UpgradeFromVersion + 1; version <= CurrentVersion; version++ {
		if _, ok := storeSteps[version]; ok {
			continue
		}
		next := version + 1
		for ; next <= CurrentVersion; next++ {
			if _, ok := storeSteps[next]; ok {
				break
			}
		}
		if storeSteps[next].additive {
			t.Fatalf("schema %d is missing, but the step after the gap (%d) is additive: only a non-additive step skips versions", version, next)
		}
	}
}

/*
================
TestAdditiveStepsDropNoField

A step marked additive keeps every field of the version before it, for
every record that has a list at both versions. Steps 37 through 41, the
ones #3082 found additive, are among those checked.
================
*/
func TestAdditiveStepsDropNoField(t *testing.T) {
	t.Parallel()

	checked := 0
	for version := UpgradeFromVersion + 1; version <= CurrentVersion; version++ {
		step, ok := storeSteps[version]
		if !ok || !step.additive {
			continue
		}
		previous, before := frozenFieldLists(t, version-1)
		current, after := frozenFieldLists(t, version)
		if !before || !after {
			continue
		}
		checked++
		if dropped := droppedFields(previous, current); len(dropped) != 0 {
			t.Errorf("schema %d is marked additive but drops %v (%s)", version, dropped, step.why)
		}
	}
	for version := 37; version <= 41; version++ {
		if !storeSteps[version].additive {
			t.Errorf("schema %d is one of #3082's additive steps but is not marked additive", version)
		}
	}
	if checked == 0 {
		t.Fatal("no additive step had field lists to check")
	}
}

/*
================
TestARenamedFieldIsNotAdditive

The rule bites: a step that renames a field, marked additive, is
refused, whether a binary would read it inside a window or not.
================
*/
func TestARenamedFieldIsNotAdditive(t *testing.T) {
	t.Parallel()

	previous := fieldLists{"character": {"gold", "name"}}
	renamed := fieldLists{"character": {"goldAmount", "name"}}
	if dropped := droppedFields(previous, renamed); !slices.Equal(dropped, []string{"character: gold"}) {
		t.Fatalf("the rename's drop = %v", dropped)
	}
	steps := map[int]storeStep{50: {additive: true, why: "a test"}, 51: {additive: true, why: "renames gold (marked additive by mistake)"}}
	if violations := readWindowViolations(steps, 51, 2, previous, renamed); len(violations) == 0 {
		t.Fatal("a renamed field marked additive entered the read window")
	}
	if dropped := droppedFields(previous, fieldLists{}); len(dropped) != 1 {
		t.Fatalf("a dropped record = %v", dropped)
	}
}

/*
================
TestReadWindowAdmitsOnlyKeptAdditions

Inside a window two versions wide: a new top-level field or record
enters; a field nested in a known object does not, since the previous
binary refuses it; a non-additive step enters only by skipping past the
window.
================
*/
func TestReadWindowAdmitsOnlyKeptAdditions(t *testing.T) {
	t.Parallel()

	previous := fieldLists{"character": {"job", "job.type", "name"}}
	steps := map[int]storeStep{50: {additive: true, why: "a test"}, 51: {additive: true, why: "a test"}, 54: {additive: false, why: "a test"}}
	for _, test := range []struct {
		name    string
		version int
		current fieldLists
		admit   bool
	}{
		{"new top-level field", 51, fieldLists{"character": {"job", "job.type", "name", "online", "online.total"}}, true},
		{"new record", 51, fieldLists{"character": {"job", "job.type", "name"}, "guild": {"id"}}, true},
		{"field nested in a known object", 51, fieldLists{"character": {"job", "job.reward", "job.type", "name"}}, false},
		{"non-additive step that skips the window", 54, previous, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			violations := readWindowViolations(steps, test.version, 2, previous, test.current)
			if (len(violations) == 0) != test.admit {
				t.Fatalf("violations %v, want admitted %v", violations, test.admit)
			}
		})
	}
	inside := map[int]storeStep{50: {additive: true, why: "a test"}, 52: {additive: false, why: "a test"}}
	if violations := readWindowViolations(inside, 52, 2, previous, previous); len(violations) != 1 {
		t.Fatalf("a non-additive step two versions on entered a two-wide window: %v", violations)
	}
	if violations := readWindowViolations(inside, 52, 0, previous, previous); len(violations) != 0 {
		t.Fatalf("headroom 0 checked something: %v", violations)
	}
}

/*
================
TestHistoricStepsAWindowCouldNotCarry

The record of which shipped additive steps (19 through 43) a read
window could not have carried, because each adds a field inside an
object the previous version already had: a new key on job, mostWanted
or a world point, or on every item or companion row. They are additive
by the rule, but the binary before them would refuse them (json.go
keeps only top-level unknown fields). Part 4 (#3093) plans its window
against this: a step of that shape still takes a maintenance release
until nested unknown fields are kept too.
================
*/
func TestHistoricStepsAWindowCouldNotCarry(t *testing.T) {
	t.Parallel()

	var refused []int
	for version := 19; version <= 43; version++ {
		if !storeSteps[version].additive {
			continue
		}
		previous, before := frozenFieldLists(t, version-1)
		current, after := frozenFieldLists(t, version)
		if before && after && len(nestedAdditions(previous, current)) > 0 {
			refused = append(refused, version)
		}
	}
	if want := []int{19, 28, 29, 31, 35, 40}; !slices.Equal(refused, want) {
		t.Fatalf("additive steps with nested additions = %v, want %v", refused, want)
	}
}

/*
================
TestCurrentStepFitsTheReadWindow

The gate on the live constant: the step this binary adds must fit the
read window it declares (StoreReadHeadroom, #3091). At headroom 0 the
window holds no newer step and nothing is checked; part 4 (#3093)
turns it on.
================
*/
func TestCurrentStepFitsTheReadWindow(t *testing.T) {
	t.Parallel()

	previous, _ := frozenFieldLists(t, CurrentVersion-1)
	current, _ := frozenFieldLists(t, CurrentVersion)
	if violations := readWindowViolations(storeSteps, CurrentVersion, StoreReadHeadroom, previous, current); len(violations) != 0 {
		t.Fatalf("schema %d does not fit a read window %d wide: %v", CurrentVersion, StoreReadHeadroom, violations)
	}
}
