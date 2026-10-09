/*
===========================================================================

persisted_shape_test.go - every stored record's fields belong to a schema

Records decode strictly (decodeJSONStrict), so a server cannot read a
record that carries a field it does not know. A new persisted field is
therefore a new schema: CurrentVersion, its offline upgrade step and the
release's store compatibility move with it. #388 added endedQuestIds without
that step and a schema 17 server shipped it; this pins every strictly decoded
record's field set to the version so the next field cannot do the same.

===========================================================================
*/
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

// recordFieldsGolden is the field list a schema owns for one record, one
// file per record and version.
const recordFieldsGolden = "testdata/%s-fields-v%d.txt"

/*
================
persistedFields

Every JSON path the character record can carry, sorted. Slices and maps
are walked into their elements ("activeQuests[].refId"); a type is walked
once per path, so recursive types end.
================
*/
func persistedFields(t reflect.Type, prefix string, seen map[reflect.Type]bool, out *[]string) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		if t.Kind() != reflect.Pointer {
			prefix += "[]"
		}
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	defer delete(seen, t)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			persistedFields(field.Type, prefix, seen, out)
			continue
		}
		if name == "" {
			name = field.Name
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		*out = append(*out, path)
		persistedFields(field.Type, path, seen, out)
	}
}

/*
================
strictRecords

Every record the store decodes strictly (decodeJSONStrict, and
decodeAccountStorage's own strict decoder), named by its golden list.
================
*/
var strictRecords = []struct {
	name   string
	record reflect.Type
}{
	{"character", reflect.TypeOf(domain.Character{})},
	{"ground-item", reflect.TypeOf(domain.GroundItemRecord{})},
	{"letter", reflect.TypeOf(domain.LetterRecord{})},
	{"guild", reflect.TypeOf(domain.GuildRecord{})},
	{"guild-member", reflect.TypeOf(domain.GuildMemberRecord{})},
	{"training-camp", reflect.TypeOf(domain.TrainingCampRecord{})},
	{"training-camp-member", reflect.TypeOf(domain.TrainingCampMemberRecord{})},
	{"alliance", reflect.TypeOf(domain.AllianceRecord{})},
	{"guild-war", reflect.TypeOf(domain.GuildWarRecord{})},
	{"fortress", reflect.TypeOf(domain.FortressRecord{})},
	{"fortress-structure", reflect.TypeOf(domain.FortressStructureRecord{})},
	{"trade-reward-pool", reflect.TypeOf(domain.TradeRewardPool{})},
	{"account-storage", reflect.TypeOf(domain.AccountStorage{})},
}

/*
================
TestRecordFieldsBelongToTheCurrentSchema

A changed field set on any strictly decoded record fails until
CurrentVersion moves to a new version whose golden list holds it: add the
schema step (schema.go), its case in UpgradeAuthority and the store
versions in compatibility.json, then write
testdata/<record>-fields-v<N>.txt from the reported list. A version's list
is never edited after it ships.
================
*/
func TestRecordFieldsBelongToTheCurrentSchema(t *testing.T) {
	for _, record := range strictRecords {
		t.Run(record.name, func(t *testing.T) {
			var fields []string
			persistedFields(record.record, "", map[reflect.Type]bool{}, &fields)
			slices.Sort(fields)
			fields = slices.Compact(fields)
			path := filepath.FromSlash(fmt.Sprintf(recordFieldsGolden, record.name, CurrentVersion))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("schema %d has no %s field list (%v); the current fields are:\n%s", CurrentVersion, record.name, err, strings.Join(fields, "\n"))
			}
			var golden []string
			for _, line := range strings.Split(string(raw), "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
					golden = append(golden, line)
				}
			}
			if !slices.Equal(golden, fields) {
				added, removed := difference(fields, golden), difference(golden, fields)
				t.Fatalf("%s fields changed under schema %d (added %v, removed %v): a persisted field needs a new schema version and upgrade step, not an edit of %s", record.name, CurrentVersion, added, removed, path)
			}
		})
	}
}

/*
================
TestSchemaStepCarriesAFieldChange

A version step lists different fields from the one before it for at
least one record; a step with every list unchanged is not a schema change.
================
*/
func TestSchemaStepCarriesAFieldChange(t *testing.T) {
	compared := false
	for _, record := range strictRecords {
		current, err := os.ReadFile(filepath.FromSlash(fmt.Sprintf(recordFieldsGolden, record.name, CurrentVersion)))
		if err != nil {
			t.Fatal(err)
		}
		previous, err := os.ReadFile(filepath.FromSlash(fmt.Sprintf(recordFieldsGolden, record.name, CurrentVersion-1)))
		if err != nil {
			// The lists start at schema 18; nothing older is pinned.
			return
		}
		compared = true
		if string(current) != string(previous) {
			return
		}
	}
	if compared {
		t.Fatalf("schema %d lists the same fields as %d for every record: a version step carries a change", CurrentVersion, CurrentVersion-1)
	}
}

/*
================
difference

The entries of a that b lacks.
================
*/
func difference(a, b []string) []string {
	var out []string
	for _, entry := range a {
		if !slices.Contains(b, entry) {
			out = append(out, entry)
		}
	}
	return out
}
