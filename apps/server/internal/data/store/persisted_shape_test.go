/*
===========================================================================

persisted_shape_test.go - the character record's fields belong to a schema

Records decode strictly (decodeJSONStrict), so a server cannot read a
character that carries a field it does not know. A new persisted field is
therefore a new schema: CurrentVersion, its offline upgrade step and the
release's store compatibility move with it. #388 added endedQuestIds without
that step and a schema 17 server shipped it; this pins the field set to the
version so the next field cannot do the same.

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

// characterFieldsGolden is the field list a schema owns, one file per version.
const characterFieldsGolden = "testdata/character-fields-v%d.txt"

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
TestCharacterFieldsBelongToTheCurrentSchema

A changed character field set fails until CurrentVersion moves to a new
version whose golden list holds it: add the schema step (schema.go), its
case in UpgradeAuthority and the store versions in compatibility.json,
then write testdata/character-fields-v<N>.txt from the reported list. A
version's list is never edited after it ships.
================
*/
func TestCharacterFieldsBelongToTheCurrentSchema(t *testing.T) {
	var fields []string
	persistedFields(reflect.TypeOf(domain.Character{}), "", map[reflect.Type]bool{}, &fields)
	slices.Sort(fields)
	fields = slices.Compact(fields)
	path := filepath.FromSlash(fmt.Sprintf(characterFieldsGolden, CurrentVersion))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("schema %d has no character field list (%v); the current fields are:\n%s", CurrentVersion, err, strings.Join(fields, "\n"))
	}
	var golden []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			golden = append(golden, line)
		}
	}
	if !slices.Equal(golden, fields) {
		added, removed := difference(fields, golden), difference(golden, fields)
		t.Fatalf("character fields changed under schema %d (added %v, removed %v): a persisted field needs a new schema version and upgrade step, not an edit of %s", CurrentVersion, added, removed, path)
	}
	previous := filepath.FromSlash(fmt.Sprintf(characterFieldsGolden, CurrentVersion-1))
	if old, err := os.ReadFile(previous); err == nil && string(old) == string(raw) {
		t.Fatalf("schema %d lists the same character fields as %d: a version step carries a change", CurrentVersion, CurrentVersion-1)
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
