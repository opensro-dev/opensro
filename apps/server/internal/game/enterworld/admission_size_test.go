package enterworld

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"opensro.online/server/internal/transport"
	"reflect"
	"testing"
)

func TestPublishedReferencesLeaveTheLoginEnvelope(t *testing.T) {
	licensed.RequireGameData(t)
	source := NewTextdataSkills(gamedatatest.TextdataDir(t))
	rows := spawnSkillSnapshot(source)
	if len(rows) == 0 {
		t.Fatal("published skill catalog missing")
	}
	refs, err := NewBrowserReferences(source)
	if err != nil {
		t.Fatal(err)
	}
	character := chinaSpearman()
	deps := testDeps(character)
	deps.Skills = source
	deps.BrowserReferences = refs
	outcome := HandleEnterWorld(deps, transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, DefaultDivisionID, character.Name)))
	if !outcome.OK {
		t.Fatalf("admission failed: %+v", outcome.Result)
	}
	if len(outcome.ResultPayload)+2 > transport.MaxFrameBytes {
		t.Fatal("complete admission frame exceeds transport limit")
	}
	decoded, err := transport.DecodeEnterWorldResult(outcome.ResultPayload)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		V          int
		Bootstrap  map[string]json.RawMessage
		References BrowserReferences
	}
	if err = json.Unmarshal(decoded.Blob, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.V != 2 || envelope.References.SHA256 != refs.SHA256 || envelope.Bootstrap["refSkillSnapshot"] != nil {
		t.Fatal("login must name public data, not embed it")
	}
	var restored struct{ RefSkillSnapshot []SpawnSkillRow }
	identity := httptest.NewRecorder()
	refs.ServeHTTP(identity, httptest.NewRequest("GET", refs.Path, nil))
	referenceData := identity.Body.Bytes()
	if err = json.Unmarshal(referenceData, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, restored.RefSkillSnapshot) {
		t.Fatal("public resource changes reference semantics")
	}
	for _, encoding := range []string{"", "gzip"} {
		q := httptest.NewRequest("GET", refs.Path, nil)
		q.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		refs.ServeHTTP(w, q)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatal("reference cache contract")
		}
		data := w.Body.Bytes()
		if encoding == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			data, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(data, referenceData) {
			t.Fatal("compressed and plain resource differ")
		}
	}
	q := httptest.NewRequest("GET", refs.Path, nil)
	q.Header.Set("If-None-Match", `"`+refs.SHA256+`"`)
	w := httptest.NewRecorder()
	refs.ServeHTTP(w, q)
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("conditional cache hit retransmitted resource")
	}
	w = httptest.NewRecorder()
	refs.ServeHTTP(w, httptest.NewRequest("GET", "/transport/references/stale.json", nil))
	if w.Code != 404 {
		t.Fatal("stale identity alias")
	}
	t.Logf("complete fixture admission=%d bytes; static rows=%d raw=%d gzip=%d", len(outcome.ResultPayload)+2, len(rows), refs.Bytes, len(refs.gzipData))
}
