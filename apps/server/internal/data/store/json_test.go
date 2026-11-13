/*
===========================================================================

json_test.go - the strict decoder and the unknown-field round trip

===========================================================================
*/
package store

import (
	"encoding/json"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCanonicalRecordDecoderRefusesDrift
================
*/
func TestCanonicalRecordDecoderRefusesDrift(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{"unknown field", `{"id":1,"masterCharId":1,"retiredField":true}`, "unknown field"},
		{"trailing document", `{"id":1,"masterCharId":1}{"id":2,"masterCharId":2}`, "trailing JSON document"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var record domain.TrainingCampRecord
			err := decodeJSONStrict([]byte(test.raw), &record)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decode error = %v, want %q", err, test.want)
			}
		})
	}
}

/*
================
TestRecordDecoderKeepsOnlyTopLevelUnknownFields

decodeRecordFields lifts the record's own unknown fields out, sorted and
compact, and leaves every other strict refusal in place: a trailing
document, an unknown field inside a nested object, a known field of the
wrong type. A key differing from a field only in case is that field, as
encoding/json reads it.
================
*/
func TestRecordDecoderKeepsOnlyTopLevelUnknownFields(t *testing.T) {
	t.Parallel()

	var camp domain.TrainingCampRecord
	unknown, err := decodeRecordFields([]byte(`{"zeta":[1, 2],"id":4,"MasterCharID":9,"alpha":{"b":1, "a":"x"}}`), &camp)
	if err != nil || camp.ID != 4 || camp.MasterCharID != 9 {
		t.Fatalf("decode = %+v, %v", camp, err)
	}
	if want := unknownFields(`{"alpha":{"b":1,"a":"x"},"zeta":[1,2]}`); unknown != want {
		t.Fatalf("residue = %s, want %s", unknown, want)
	}
	for _, test := range []struct {
		name, raw, want string
	}{
		{"trailing document", `{"id":1}{"id":2}`, "trailing JSON"},
		{"trailing document beside an unknown field", `{"id":1,"extra":1}{"id":2}`, "extra"},
		{"nested unknown field", `{"id":1,"extra":1,"pk":{"mystery":1}}`, "mystery"},
		{"known field of the wrong type", `{"id":"one","extra":1}`, "cannot unmarshal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var c domain.Character
			if _, err := decodeRecordFields([]byte(test.raw), &c); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	// A record a current binary wrote has no residue and takes the strict
	// decode alone.
	if unknown, err := decodeRecordFields([]byte(`{"id":4,"masterCharId":9}`), &camp); err != nil || unknown != "" {
		t.Fatalf("current record = %q, %v", unknown, err)
	}
}

/*
================
TestUnknownFieldsMergeAfterTheKnownOnes
================
*/
func TestUnknownFieldsMergeAfterTheKnownOnes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		encoded string
		unknown unknownFields
		want    string
	}{
		{`{"id":1}`, "", `{"id":1}`},
		{`{"id":1}`, `{"extra":[1]}`, `{"id":1,"extra":[1]}`},
		{`{}`, `{"extra":true}`, `{"extra":true}`},
		{"{\"id\":1}\n", `{"extra":true}`, `{"id":1,"extra":true}`},
	} {
		got, err := test.unknown.merge([]byte(test.encoded))
		if err != nil || string(got) != test.want {
			t.Fatalf("merge(%s, %s) = %s, %v; want %s", test.encoded, test.unknown, got, err, test.want)
		}
	}
	if _, err := unknownFields(`{"extra":true}`).merge([]byte(`[1]`)); err == nil {
		t.Fatal("unknown fields merged into a non-object")
	}
}

/*
================
TestPlaneEncodingWithoutUnknownFieldsIsUnchanged

No behaviour change at today's exact-version boot: a plane without
residues encodes to json.Marshal's bytes, and one with residues adds
them to its own entry only.
================
*/
func TestPlaneEncodingWithoutUnknownFieldsIsUnchanged(t *testing.T) {
	t.Parallel()

	plane := map[string]*domain.SookArenaAccount{"b": {Day: 2, Entries: 1}, "a": {Day: 1, Entries: 3}}
	want, err := json.Marshal(plane)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := encodePlaneFields(plane, nil); err != nil || string(got) != string(want) {
		t.Fatalf("plain plane = %s, %v; want %s", got, err, want)
	}
	got, err := encodePlaneFields(plane, map[string]unknownFields{"b": `{"spare":1}`})
	if err != nil || string(got) != `{"a":{"day":1,"entries":3},"b":{"day":2,"entries":1,"spare":1}}` {
		t.Fatalf("plane with a residue = %s, %v", got, err)
	}
	var decoded map[string]*domain.SookArenaAccount
	unknown, err := decodePlaneFields(got, &decoded)
	if err != nil || len(unknown) != 1 || unknown["b"] != `{"spare":1}` || *decoded["a"] != *plane["a"] || *decoded["b"] != *plane["b"] {
		t.Fatalf("round trip = %+v %v, %v", decoded, unknown, err)
	}
}

/*
================
TestFormerMembersDropTheirUnknownFields

A member who left a guild loses the residue at the member set's next
write, so a later membership of the same character starts clean; other
guilds and divisions keep theirs.
================
*/
func TestFormerMembersDropTheirUnknownFields(t *testing.T) {
	t.Parallel()

	residues := map[memberResidueKey]unknownFields{
		{division: "d", group: 1, charID: 10}: `{"a":1}`,
		{division: "d", group: 1, charID: 11}: `{"b":1}`,
		{division: "d", group: 2, charID: 11}: `{"c":1}`,
		{division: "e", group: 1, charID: 11}: `{"d":1}`,
	}
	pruneMembers(residues, "d", 1, func(charID int64) bool { return charID == 10 })
	if len(residues) != 3 || residues[memberResidueKey{division: "d", group: 1, charID: 11}] != "" {
		t.Fatalf("after the prune: %v", residues)
	}
}
