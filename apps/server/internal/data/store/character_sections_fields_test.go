/*
===========================================================================

character_sections_fields_test.go - every persisted field is detected

The sampled mutations in character_sections_test.go convict one field
per section; this file convicts them all. For every persisted field of
domain.Character a generic mutation must move the section comparison,
and through the real door it must write exactly the production
encoder's bytes for the mutated record: a field the comparison cannot
see is a save the skip would lose, and no future field addition may
reopen that hole. Fields tagged json:"-" get the mirror pin: mutating
them must convict nothing, or ephemeral churn would force encodes.

===========================================================================
*/
package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/tier"
)

// mutationTimeType is the one struct kind whose fields reflection may not
// set: a persisted time is replaced whole instead of walked.
var mutationTimeType = reflect.TypeOf(time.Time{})

/*
================
mutatePersistedFieldValue

Sets v to a value that differs from the one it holds, walking plain
data the way equalPersistedValues reads it: scalars step, pointers
allocate, slices and maps grow or step their first entry, structs step
their first settable field. False when no generic mutation exists
(func, channel and unsafe kinds): the caller then fails loudly, so a
future field kind is a test to write, never a silent hole.
================
*/
func mutatePersistedFieldValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(!v.Bool())
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v.SetUint(v.Uint() + 1)
		return true
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1.25)
		return true
	case reflect.String:
		v.SetString(v.String() + "x")
		return true
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return mutatePersistedFieldValue(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return false
		}
		next := reflect.New(v.Elem().Type()).Elem()
		next.Set(v.Elem())
		if !mutatePersistedFieldValue(next) {
			return false
		}
		v.Set(next)
		return true
	case reflect.Slice:
		if v.Len() > 0 {
			return mutatePersistedFieldValue(v.Index(0))
		}
		v.Set(reflect.Append(v, reflect.New(v.Type().Elem()).Elem()))
		return true
	case reflect.Array:
		if v.Len() == 0 {
			return false
		}
		return mutatePersistedFieldValue(v.Index(0))
	case reflect.Map:
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		key, ok := mutatedMapKey(v.Type().Key())
		if !ok {
			return false
		}
		if held := v.MapIndex(key); held.IsValid() {
			next := reflect.New(v.Type().Elem()).Elem()
			next.Set(held)
			if !mutatePersistedFieldValue(next) {
				return false
			}
			v.SetMapIndex(key, next)
			return true
		}
		next := reflect.New(v.Type().Elem()).Elem()
		mutatePersistedFieldValue(next)
		v.SetMapIndex(key, next)
		return true
	case reflect.Struct:
		if v.Type() == mutationTimeType {
			v.Set(reflect.ValueOf(time.Unix(1234567, 0).UTC()))
			return true
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if field.CanSet() && mutatePersistedFieldValue(field) {
				return true
			}
		}
		return false
	}
	return false
}

/*
================
mutatedMapKey

A key the fixture maps do not already hold, built per key kind. False
for key kinds with no generic fresh key; the owning test names it.
================
*/
func mutatedMapKey(keyType reflect.Type) (reflect.Value, bool) {
	key := reflect.New(keyType).Elem()
	switch key.Kind() {
	case reflect.String:
		key.SetString("zz-mutated-key")
	case reflect.Bool:
		key.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		key.SetInt(987654)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		key.SetUint(987654)
	case reflect.Float32, reflect.Float64:
		key.SetFloat(987654.5)
	default:
		if !mutatePersistedFieldValue(key) {
			return key, false
		}
	}
	return key, true
}

/*
================
TestCharacterSectionsDetectEveryPersistedField

One mutation per persisted field, applied to a record and its
committed clone: the section comparison must convict every one.
================
*/
func TestCharacterSectionsDetectEveryPersistedField(t *testing.T) {
	t.Parallel()

	characterType := reflect.TypeOf(domain.Character{})
	detected := 0
	for i := 0; i < characterType.NumField(); i++ {
		field := characterType.Field(i)
		if field.Tag.Get("json") == "-" {
			continue
		}
		detected++
		t.Run(field.Name, func(t *testing.T) {
			c := seededCharacter()
			committed := domain.CloneCharacter(c)
			target := reflect.ValueOf(c).Elem().Field(i)
			if !mutatePersistedFieldValue(target) {
				t.Fatalf("no generic mutation for persisted field %s (%s)", field.Name, field.Type)
			}
			if changed := characterSectionsChanged(c, committed); len(changed) == 0 {
				t.Fatalf("mutation of persisted field %s was not detected", field.Name)
			}
		})
	}
	if detected == 0 {
		t.Fatal("no persisted fields found on domain.Character")
	}
}

/*
================
TestCharacterSectionsIgnoreEveryEphemeralField

The mirror: one mutation per json:"-" field must convict nothing, or
runtime churn would buy back the encodes the skip removed.
================
*/
func TestCharacterSectionsIgnoreEveryEphemeralField(t *testing.T) {
	t.Parallel()

	characterType := reflect.TypeOf(domain.Character{})
	ignored := 0
	for i := 0; i < characterType.NumField(); i++ {
		field := characterType.Field(i)
		if field.Tag.Get("json") != "-" {
			continue
		}
		ignored++
		t.Run(field.Name, func(t *testing.T) {
			c := seededCharacter()
			committed := domain.CloneCharacter(c)
			target := reflect.ValueOf(c).Elem().Field(i)
			if !mutatePersistedFieldValue(target) {
				t.Skipf("no generic mutation for ephemeral field %s (%s)", field.Name, field.Type)
			}
			if changed := characterSectionsChanged(c, committed); len(changed) != 0 {
				t.Fatalf("ephemeral field %s forced sections %v", field.Name, changed)
			}
		})
	}
	if ignored == 0 {
		t.Fatal("no ephemeral fields found on domain.Character")
	}
}

/*
================
TestCommitWritesEveryPersistedFieldChange

The door-level half: every persisted field's mutation goes through
the real commit and the row on disk is the production encoder's bytes
for the mutated record. Name rides along like any other field; ID is
the one exception, because it is the row's identity rather than its
data: no door mutates it, and the by-name reader can only ever
address the identity a record already has. The comparison above is
what covers ID's conviction.
================
*/
func TestCommitWritesEveryPersistedFieldChange(t *testing.T) {
	tier.Long(t, "every persisted character field's change is written by its scoped commit")

	characterType := reflect.TypeOf(domain.Character{})
	for i := 0; i < characterType.NumField(); i++ {
		field := characterType.Field(i)
		if field.Tag.Get("json") == "-" {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			if field.Name == "ID" {
				t.Skip("ID is the row identity, not section data; the comparison test covers it")
			}
			t.Parallel()

			s := openTest(t, t.TempDir(), newTestClock())
			c, _ := stageCommittedCopy(t, s)
			s.MutateCharacter(c, "sections-fields", func() {
				target := reflect.ValueOf(c).Elem().Field(i)
				if !mutatePersistedFieldValue(target) {
					t.Fatalf("no generic mutation for persisted field %s (%s)", field.Name, field.Type)
				}
			})
			want, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if got := readCharacterRecordRaw(t, s, testDivision, strings.ToLower(c.Name)); got != string(want) {
				t.Fatalf("mutation of persisted field %s was not persisted:\ngot:  %s\nwant: %s", field.Name, got, want)
			}
		})
	}
}
