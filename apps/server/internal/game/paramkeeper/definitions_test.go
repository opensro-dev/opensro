/*
===========================================================================

definitions_test.go - the definition table's parse refusals

A table that decodes to the wrong numbers must refuse, not install: every
row is exactly id, min, max, base, ignore.

===========================================================================
*/
package paramkeeper

import (
	"strings"
	"testing"
)

/*
================
definitionsDocument

A table in the operator's format with the given rows (JSON arrays).
================
*/
func definitionsDocument(rows ...string) []byte {
	return []byte(`{"Format":"` + definitionsFormat + `","Definitions":[` + strings.Join(rows, ",") + `]}`)
}

/*
================
TestParseDefinitionsReadsEveryColumn
================
*/
func TestParseDefinitionsReadsEveryColumn(t *testing.T) {
	table, err := ParseDefinitions(definitionsDocument(`[34,0,1000,100,-1]`, `[0,1,140,0,0]`))
	if err != nil {
		t.Fatal(err)
	}
	want := Definition{Minimum: 0, Maximum: 1000, Base: 100, Ignore: -1}
	if got := table[34]; got != want {
		t.Fatalf("parameter 34 = %+v, want %+v", got, want)
	}
	if len(table) != 2 {
		t.Fatalf("table has %d rows, want 2", len(table))
	}
}

/*
================
TestParseDefinitionsRefusesMisshapenRows

A short row once decoded as base 0 / ignore 0 and a long one lost its tail,
both without an error.
================
*/
func TestParseDefinitionsRefusesMisshapenRows(t *testing.T) {
	cases := []struct {
		name, row, want string
	}{
		{"short", `[34,0,1000]`, "row 1 has 3 columns, want 5"},
		{"long", `[34,0,1000,100,-1,7]`, "row 1 has 6 columns, want 5"},
		{"empty", `[]`, "row 1 has 0 columns, want 5"},
		{"null cell", `[34,0,1000,null,-1]`, "row 1 column 3 is null"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseDefinitions(definitionsDocument(`[0,1,140,0,0]`, c.row))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ParseDefinitions(%s) error = %v, want %q", c.row, err, c.want)
			}
		})
	}
}

/*
================
TestParseDefinitionsRefusesAnEmptyTable
================
*/
func TestParseDefinitionsRefusesAnEmptyTable(t *testing.T) {
	if _, err := ParseDefinitions(definitionsDocument()); err == nil {
		t.Fatal("an empty table parsed")
	}
}
