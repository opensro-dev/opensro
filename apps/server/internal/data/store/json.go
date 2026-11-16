/*
===========================================================================

json.go - the store's record decoder and the unknown-field round trip

Persisted records decode strictly: a trailing document or malformed JSON
fails the load. A record's own top-level fields that this binary does not
know are the exception (#3090, part 1 of #3082): a newer release may have
written them, so they are kept beside the record as its unknownFields and
merged back verbatim whenever the record is written again. A binary that
opens a newer store therefore never drops the newer release's data on
its next commit. Below the top level the strict rule stands: an unknown
field inside a nested object or a list element still fails the load, as
before, because the game rebuilds nested values freely and a residue
there would have nothing stable to follow. A newer release's nested
field therefore still needs the exact-version boot.

Who holds a record's unknownFields between the load and the write
depends on how the record is written:

  - rows the commit rewrites from the store's own state (characters,
    ground items, guilds, training camps and their members, the meta
    planes) keep them in the store's recordResidues, keyed by the
    row's identity;
  - letters have no identity but their place in a mailbox, so the
    letter value carries them (domain.LetterRecord.UnknownFields);
  - every other row is written by a door that reads the row it
    replaces inside the same transaction (storedUnknownFields), so the
    disk itself owns the residue.

===========================================================================
*/
package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"

	"opensro.online/server/internal/domain"
)

/*
================
decodeJSONStrict

The canonical strict decoder: unknown fields or trailing documents fail.
Persisted records reach it through decodeRecordFields, which first moves
a record's unknown top-level fields aside.
================
*/
func decodeJSONStrict(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON document")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

//============================================================================

/*
================
unknownFields

A record's top-level fields this binary does not know: one compact JSON
object, its keys sorted so the same residue always spells the same bytes
(the commit's digest skip compares bytes), each value as stored. Empty
when the record has none, which is every record a current binary wrote.
================
*/
type unknownFields string

/*
================
unknownFields::merge

The record's encoding with its unknown fields appended after the known
ones. Without unknown fields the encoding is returned untouched, so a
record with none costs nothing and keeps today's bytes.
================
*/
func (u unknownFields) merge(encoded []byte) ([]byte, error) {
	if u == "" {
		return encoded, nil
	}
	trimmed := bytes.TrimSpace(encoded)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, fmt.Errorf("record with unknown fields encodes to %.20q, not an object", trimmed)
	}
	merged := make([]byte, 0, len(trimmed)+len(u))
	merged = append(merged, trimmed[:len(trimmed)-1]...)
	if len(bytes.TrimSpace(trimmed[1:len(trimmed)-1])) > 0 {
		merged = append(merged, ',')
	}
	return append(merged, u[1:]...), nil
}

/*
================
splitUnknownFields

Moves the top-level keys the record type t does not know out of raw.
known is raw itself when there are none. raw that is not a JSON object
is returned as it is, for the strict decode to judge. A key matches a
field the way encoding/json matches it: exactly, or else ignoring case.
================
*/
func splitUnknownFields(raw []byte, t reflect.Type) (known []byte, unknown unknownFields, err error) {
	fields, isObject := objectFields(raw)
	if !isObject {
		return raw, "", nil
	}
	names := recordFieldNames(t)
	var foreign []string
	for key := range fields {
		if !names.has(key) {
			foreign = append(foreign, key)
		}
	}
	if len(foreign) == 0 {
		return raw, "", nil
	}
	slices.Sort(foreign)
	var residue bytes.Buffer
	residue.WriteByte('{')
	for i, key := range foreign {
		if i > 0 {
			residue.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, "", err
		}
		residue.Write(name)
		residue.WriteByte(':')
		if err := json.Compact(&residue, fields[key]); err != nil {
			return nil, "", err
		}
		delete(fields, key)
	}
	residue.WriteByte('}')
	if known, err = json.Marshal(fields); err != nil {
		return nil, "", err
	}
	return known, unknownFields(residue.String()), nil
}

/*
================
objectFields

raw's top-level keys with their values as stored; false when raw is not
one JSON object.
================
*/
func objectFields(raw []byte) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if decodeJSONStrict(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

/*
================
decodeRecordFields

decodeJSONStrict for one persisted record, except that the record's
unknown top-level fields are returned instead of failing the decode.
dst points at the record (or at a pointer to it). The strict decode
runs first: a record a current binary wrote decodes exactly as before,
and only a failed decode pays for the split and a second attempt.
================
*/
func decodeRecordFields(raw []byte, dst any) (unknownFields, error) {
	strictErr := decodeJSONStrict(raw, dst)
	if strictErr == nil {
		return "", nil
	}
	known, unknown, err := splitUnknownFields(raw, reflect.TypeOf(dst))
	if err != nil || unknown == "" {
		return "", strictErr
	}
	// The failed attempt may have filled part of dst.
	target := reflect.ValueOf(dst).Elem()
	target.Set(reflect.Zero(target.Type()))
	if err := decodeJSONStrict(known, dst); err != nil {
		return "", err
	}
	return unknown, nil
}

/*
================
decodePlaneFields

A meta plane row: one JSON object whose values are records keyed by
their subject (an account id, a reward pool). Each record keeps its own
unknown fields, returned by key (nil when no record has any).
================
*/
func decodePlaneFields[V any](raw []byte, dst *map[string]V) (map[string]unknownFields, error) {
	if decodeJSONStrict(raw, dst) == nil {
		return nil, nil
	}
	var entries map[string]json.RawMessage
	if err := decodeJSONStrict(raw, &entries); err != nil {
		return nil, err
	}
	plane := make(map[string]V, len(entries))
	var unknown map[string]unknownFields
	for key, entry := range entries {
		var record V
		residue, err := decodeRecordFields(entry, &record)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", key, err)
		}
		if residue != "" {
			if unknown == nil {
				unknown = map[string]unknownFields{}
			}
			unknown[key] = residue
		}
		plane[key] = record
	}
	*dst = plane
	return unknown, nil
}

/*
================
encodePlaneFields

json.Marshal of a meta plane with each record's unknown fields merged
back. Without any the plane marshals exactly as it always did.
================
*/
func encodePlaneFields[V any](plane map[string]V, unknown map[string]unknownFields) ([]byte, error) {
	if len(unknown) == 0 {
		return json.Marshal(plane)
	}
	entries := make(map[string]json.RawMessage, len(plane))
	for key, record := range plane {
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if raw, err = unknown[key].merge(raw); err != nil {
			return nil, fmt.Errorf("%q: %w", key, err)
		}
		entries[key] = raw
	}
	return json.Marshal(entries)
}

/*
================
rowQuerier
================
*/
type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

/*
================
rowWriter

The query and statement halves of *sql.DB and *sql.Tx: a writer that
reads the row it replaces (storedUnknownFields) before writing it.
================
*/
type rowWriter interface {
	rowQuerier
	sqlExecer
}

/*
================
storedUnknownFields

The unknown fields of the row a door is about to replace, read inside
the door's own transaction: query selects that row's record. No row has
none. R is the record type, which decides what is unknown.
================
*/
func storedUnknownFields[R any](q rowQuerier, query string, args ...any) (unknownFields, error) {
	var raw string
	err := q.QueryRow(query, args...).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	_, unknown, err := splitUnknownFields([]byte(raw), reflect.TypeFor[R]())
	return unknown, err
}

/*
================
encodeRecordFields

json.Marshal of one record with the unknown fields it was stored with.
================
*/
func encodeRecordFields(record any, unknown unknownFields) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	return unknown.merge(raw)
}

//============================================================================

/*
================
fieldNames

The JSON keys one record type decodes, as encoding/json resolves them.
================
*/
type fieldNames map[string]bool

var recordFieldNamesCache sync.Map // reflect.Type -> fieldNames

/*
================
fieldNames::has

Exact first, then case-insensitive, as encoding/json matches a key.
================
*/
func (names fieldNames) has(key string) bool {
	if names[key] {
		return true
	}
	for name := range names {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

/*
================
recordFieldNames

The top-level JSON keys of the record type t points at (through any
pointers). Embedded structs without a tag lend their fields, as in
encoding/json. A superset is harmless: a key wrongly taken as known
still meets the strict decode.
================
*/
func recordFieldNames(t reflect.Type) fieldNames {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if cached, ok := recordFieldNamesCache.Load(t); ok {
		return cached.(fieldNames)
	}
	names := fieldNames{}
	if t.Kind() == reflect.Struct {
		collectFieldNames(t, names, map[reflect.Type]bool{})
	}
	recordFieldNamesCache.Store(t, names)
	return names
}

/*
================
collectFieldNames
================
*/
func collectFieldNames(t reflect.Type, names fieldNames, seen map[reflect.Type]bool) {
	if seen[t] {
		return
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if field.Anonymous && name == "" {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				collectFieldNames(embedded, names, seen)
				continue
			}
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		names[name] = true
	}
}

//============================================================================

/*
================
recordResidues

The unknown fields of the rows the commit rewrites from the store's own
state, keyed by each row's identity. Filled once, at load, and only for
records that carried unknown fields, so a store a current binary wrote
holds none and every lookup is a miss on a nil map. A residue belongs
to its row, not to its key, and some keys come back: a ground gid is a
slot the registry hands out again once the drop on it is gone
(Registry.nextFreeSlot, a 100,000-slot ring), and a training camp's id
is its master's character id, so a master who disbands and opens again
gets the same key. Every writer that deletes a row therefore drops its
residue (forgetGroundResidue, forgetGroupResidues), and the whole-plane
ground rewrite keeps only the residues of the rows it writes back, so
a residue never lands on a later record. A rolled-back delete loses
nothing: the store still holds the row as gone, and the next commit
deletes it again. Character and guild ids come from never-reused
watermarks; a meta plane entry's key names its subject (an account),
which keeps its residue for as long as the key exists. Member rows are
keyed by their guild or camp and character; a member who leaves drops
the residue at the next write of the member set (pruneMembers), so a
later membership of the same character starts clean.
================
*/
type recordResidues struct {
	characters   map[committedCharacterKey]unknownFields
	ground       map[groundResidueKey]unknownFields
	guilds       map[groupResidueKey]unknownFields
	guildMembers map[memberResidueKey]unknownFields
	camps        map[groupResidueKey]unknownFields
	campMembers  map[memberResidueKey]unknownFields
	// planes is meta key -> entry key -> residue.
	planes map[string]map[string]unknownFields
}

/*
================
groundResidueKey
================
*/
type groundResidueKey struct {
	division string
	gid      uint32
}

/*
================
groupResidueKey

One guild or training camp row.
================
*/
type groupResidueKey struct {
	division string
	id       int64
}

/*
================
memberResidueKey

One member row of a guild or training camp.
================
*/
type memberResidueKey struct {
	division string
	group    int64
	charID   int64
}

/*
================
keepResidue

Records one row's residue at load; a row without one adds nothing.
================
*/
func keepResidue[K comparable](m *map[K]unknownFields, key K, residue unknownFields) {
	if residue == "" {
		return
	}
	if *m == nil {
		*m = map[K]unknownFields{}
	}
	(*m)[key] = residue
}

/*
================
pruneMembers

Drops the residues of a guild's or camp's former members: called with
the member set being written, it keeps only the rows still in it.
================
*/
func pruneMembers(residues map[memberResidueKey]unknownFields, division string, group int64, present func(charID int64) bool) {
	for key := range residues {
		if key.division == division && key.group == group && !present(key.charID) {
			delete(residues, key)
		}
	}
}

/*
================
forgetGroupResidues

Drops a deleted guild's or camp's own residue and its members': the
next record under the same id (a camp its master opens again) starts
clean.
================
*/
func forgetGroupResidues(groups map[groupResidueKey]unknownFields, members map[memberResidueKey]unknownFields, division string, group int64) {
	delete(groups, groupResidueKey{division: division, id: group})
	pruneMembers(members, division, group, func(int64) bool { return false })
}

/*
================
forgetGroundResidue

Drops a deleted ground row's residue: its gid is a slot the registry
hands to a later drop.
================
*/
func forgetGroundResidue(residues map[groundResidueKey]unknownFields, division string, gid uint32) {
	delete(residues, groundResidueKey{division: division, gid: gid})
}

/*
================
keepGroundResidues

The whole-plane rewrite's prune: only the rows it writes back keep a
residue.
================
*/
func keepGroundResidues(residues map[groundResidueKey]unknownFields, divisions map[string][]domain.GroundItemRecord) {
	for key := range residues {
		if !slices.ContainsFunc(divisions[key.division], func(item domain.GroundItemRecord) bool { return item.Gid == key.gid }) {
			delete(residues, key)
		}
	}
}
