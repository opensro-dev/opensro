/*
===========================================================================

browser_references_test.go - the public references' reverse return map table

Port-only, not native (action/reversemap.go). The points' ids run 1..n,
the contract every shipped client decodes.

===========================================================================
*/
package enterworld

import (
	"net/http/httptest"
	"strings"
	"testing"
)

/*
================
TestReverseMapReferencesRequireDenseIDs
================
*/
func TestReverseMapReferencesRequireDenseIDs(t *testing.T) {
	point := func(id uint32) ReverseMapPoint {
		return ReverseMapPoint{ID: id, Name: "Test Town", RegionID: 25000, X: 100, Z: 200}
	}
	refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{},
		ReverseMapPoints: []ReverseMapPoint{point(1), point(2)}})
	if err != nil {
		t.Fatalf("a dense table refused: %v", err)
	}
	recorder := httptest.NewRecorder()
	refs.ServeHTTP(recorder, httptest.NewRequest("GET", refs.Path, nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"id":2`) {
		t.Fatalf("the published table lost a point: status %d", recorder.Code)
	}
	for _, points := range [][]ReverseMapPoint{{point(2)}, {point(1), point(3)}, {point(0)}} {
		if _, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, ReverseMapPoints: points}); err == nil {
			t.Fatalf("points %+v accepted", points)
		}
	}
}
