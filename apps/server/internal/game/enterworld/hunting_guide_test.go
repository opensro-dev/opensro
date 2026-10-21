/*
===========================================================================

hunting_guide_test.go - independently cached atlas publication without listeners

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"net/http"
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
	t.Setenv(EnvHuntingGuide, "on")
	source := BrowserReferenceSources{Skills: oneSkillCatalogue{}}
	native, err := NewBrowserReferences(source)
	if err != nil {
		t.Fatal(err)
	}
	source.HuntingGuide = huntingGuideRows{{RefObjID: 1933, Name: "Mangyang", Level: 1,
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

/*
================
TestHuntingGuideCoexistsWithReverseMapReferences
================
*/
func TestHuntingGuideCoexistsWithReverseMapReferences(t *testing.T) {
	t.Setenv(EnvHuntingGuide, "on")
	refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{},
		ReverseMapPoints: []ReverseMapPoint{{ID: 1, Name: "Jangan", RegionID: 25000, X: 900, Z: 900}},
		HuntingGuide: huntingGuideRows{{RefObjID: 1933, Name: "Mangyang", Level: 1,
			Points: []monster.HuntingGuidePoint{{RegionID: 25000, X: 100, Z: 200}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{refs.Path, refs.HuntingGuide.Path} {
		recorder := httptest.NewRecorder()
		refs.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		expected := "Jangan"
		if path == refs.HuntingGuide.Path {
			expected = "Mangyang"
		}
		if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("coexisting reference %s lost %s: status %d", path, expected, recorder.Code)
		}
	}
}

/*
================
huntingGuideRows
================
*/
type huntingGuideRows []monster.HuntingGuideEntry

/*
================
HuntingGuide
================
*/
func (rows huntingGuideRows) HuntingGuide() []monster.HuntingGuideEntry {
	return rows
}

/*
================
countedHuntingGuide
================
*/
type countedHuntingGuide struct {
	calls int
	rows  huntingGuideRows
}

/*
================
HuntingGuide
================
*/
func (source *countedHuntingGuide) HuntingGuide() []monster.HuntingGuideEntry {
	source.calls++
	return source.rows
}

/*
================
TestHuntingGuidePublicationSetting

Off must skip construction, omit the wire identity, and refuse even a known
content-addressed URL. On keeps the native reference bytes unchanged.
================
*/
func TestHuntingGuidePublicationSetting(t *testing.T) {
	rows := huntingGuideRows{{RefObjID: 1933, Name: "Mangyang", Level: 1,
		Points: []monster.HuntingGuidePoint{{RegionID: 25000, X: 100, Z: 200}}}}
	t.Setenv(EnvHuntingGuide, "on")
	enabled, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, HuntingGuide: rows})
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"", "off", "0", "false", "invalid", "on", "1", "true", " ON "} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv(EnvHuntingGuide, setting)
			want := setting == "on" || setting == "1" || setting == "true" || setting == " ON "
			source := &countedHuntingGuide{rows: rows}
			refs, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, HuntingGuide: source})
			if err != nil {
				t.Fatal(err)
			}
			if (refs.HuntingGuide != nil) != want || (source.calls == 1) != want || source.calls > 1 {
				t.Fatalf("setting %q: guide %v, construction calls %d", setting, refs.HuntingGuide, source.calls)
			}
			if refs.SHA256 != enabled.SHA256 || refs.Bytes != enabled.Bytes {
				t.Fatal("optional setting changed native reference identity")
			}
			wire, err := json.Marshal(refs)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), `"huntingGuide"`) != want {
				t.Fatalf("unexpected wire identity: %s", wire)
			}
			recorder := httptest.NewRecorder()
			refs.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, enabled.HuntingGuide.Path, nil))
			status := http.StatusNotFound
			if want {
				status = http.StatusOK
			}
			if recorder.Code != status {
				t.Fatalf("guide request status %d, want %d", recorder.Code, status)
			}
		})
	}
}
