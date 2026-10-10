/*
===========================================================================

hunting_guide_test.go - independently cached atlas publication without listeners

===========================================================================
*/
package enterworld

import (
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestHuntingGuideReferenceIsSeparateAndImmutable
================
*/
func TestHuntingGuideReferenceIsSeparateAndImmutable(t *testing.T) {
	source := BrowserReferenceSources{Skills: oneSkillCatalogue{}}
	native, err := NewBrowserReferences(source)
	if err != nil {
		t.Fatal(err)
	}
	source.HuntingGuide = []monster.HuntingGuideEntry{{RefObjID: 1933, Name: "Mangyang", Level: 1,
		NameKey: "SN_MOB_CH_MANGNYANG", Points: []monster.HuntingGuidePoint{{RegionID: 0x619f, X: 100, Z: 200}}}}
	refs, err := NewBrowserReferences(source)
	if err != nil {
		t.Fatal(err)
	}
	if refs.SHA256 != native.SHA256 || refs.HuntingGuide == nil || refs.HuntingGuide.Path == refs.Path {
		t.Fatal("optional atlas changed the native catalogue or lacks its own identity")
	}
	recorder := httptest.NewRecorder()
	refs.ServeHTTP(recorder, httptest.NewRequest("GET", refs.HuntingGuide.Path, nil))
	if recorder.Code != 200 || recorder.Body.Len() != refs.HuntingGuide.Bytes ||
		!strings.Contains(recorder.Body.String(), `"name":"Mangyang"`) ||
		!strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("atlas not served as its immutable public projection: %d", recorder.Code)
	}
	request := httptest.NewRequest("GET", refs.HuntingGuide.Path, nil)
	request.Header.Set("If-None-Match", `"`+refs.HuntingGuide.SHA256+`"`)
	recorder = httptest.NewRecorder()
	refs.ServeHTTP(recorder, request)
	if recorder.Code != 304 {
		t.Fatalf("conditional atlas request = %d", recorder.Code)
	}
}
