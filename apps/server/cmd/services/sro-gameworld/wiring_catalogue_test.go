/*
===========================================================================

wiring_catalogue_test.go - resident catalogues by default, bounded on request

The tick looks up skills for every learned skill of every player; a bounded
catalogue decodes JSON on each miss. Production keeps rows resident unless a
small host opts in with SRO_BOUNDED_CATALOGUE=1.

===========================================================================
*/
package main

import (
	"errors"
	"testing"
)

/*
================
countingCatalogue
================
*/
type countingCatalogue struct {
	calls    []int
	failWith error
}

/*
================
UseBoundedCache
================
*/
func (c *countingCatalogue) UseBoundedCache(capacity int) error {
	c.calls = append(c.calls, capacity)
	return c.failWith
}

/*
================
TestCataloguesStayResidentByDefault
================
*/
func TestCataloguesStayResidentByDefault(t *testing.T) {
	t.Setenv(boundedCatalogueEnv, "")
	skills, items := &countingCatalogue{}, &countingCatalogue{}
	if err := configureCatalogues(skills, items, boundedCataloguesFromEnv()); err != nil {
		t.Fatal(err)
	}
	if len(skills.calls) != 0 || len(items.calls) != 0 {
		t.Fatalf("default bounded the catalogues: skills %v items %v", skills.calls, items.calls)
	}
}

/*
================
TestBoundedCatalogueOptIn
================
*/
func TestBoundedCatalogueOptIn(t *testing.T) {
	for _, value := range []string{"0", "true", "yes"} {
		t.Setenv(boundedCatalogueEnv, value)
		if boundedCataloguesFromEnv() {
			t.Fatalf("%s=%q opted in; only \"1\" does", boundedCatalogueEnv, value)
		}
	}
	t.Setenv(boundedCatalogueEnv, " 1 ")
	skills, items := &countingCatalogue{}, &countingCatalogue{}
	if err := configureCatalogues(skills, items, boundedCataloguesFromEnv()); err != nil {
		t.Fatal(err)
	}
	if len(skills.calls) != 1 || skills.calls[0] != boundedCatalogueRows ||
		len(items.calls) != 1 || items.calls[0] != boundedCatalogueRows {
		t.Fatalf("opt-in bounds: skills %v items %v", skills.calls, items.calls)
	}

	// A failed skill bound stops before the items and names its catalogue.
	broken := &countingCatalogue{failWith: errors.New("disk full")}
	items = &countingCatalogue{}
	err := configureCatalogues(broken, items, true)
	if err == nil || len(items.calls) != 0 {
		t.Fatalf("skill failure: err %v, item calls %v", err, items.calls)
	}
}
