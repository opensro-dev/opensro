/*
===========================================================================

protocol_references_test.go - each served release protocol gets its own file

Protocol 6 changed only the reference file (contract 3 adds every creatable
monster's row). A protocol-5 browser refuses an unknown key in that file, so
its sessions are named the contract-2 file, byte for byte the encoding they
read before #369, and their logins carry the monster rows themselves.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/releaseprotocol"
)

/*
================
protocolReferenceFixture

A set over one published monster, plus a login whose snapshot holds that
monster and one unpublished structure.
================
*/
func protocolReferenceFixture(t *testing.T) (BrowserReferenceSet, *Deps, RefObjRow, RefObjRow) {
	t.Helper()
	published := RefObjRow{RefObjID: 9001, TidWord: 0xc6, Codename: "MOB_PUBLISHED", Kind: "monster"}
	own := RefObjRow{RefObjID: 9002, TidWord: 0x2c6, Codename: "STRUCTURE_OWN", Kind: "structure"}
	set, err := NewBrowserReferenceSet(BrowserReferenceSources{
		Skills:      oneSkillCatalogue{},
		StaticItems: []RefItemRow{{RefObjID: 51}},
		Monsters:    []RefObjRow{published},
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := testDeps(chinaSpearman())
	deps.RefObjSnapshot = func() []RefObjRow { return []RefObjRow{published, own} }
	deps.BrowserReferences = set.For(releaseprotocol.Current)
	deps.ProtocolReferences = set
	return set, deps, published, own
}

/*
================
decodedReferences

The decoded JSON bytes a reference file serves.
================
*/
func decodedReferences(t *testing.T, references *BrowserReferences) []byte {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(references.gzipData))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
TestProtocolFiveIsNamedTheContractTwoFile

The protocol-5 file is exactly the contract-2 encoding: its four keys in
order and no refObjSnapshot, which that browser would refuse. The
protocol-6 file is contract 3 with the monster rows.
================
*/
func TestProtocolFiveIsNamedTheContractTwoFile(t *testing.T) {
	set, _, published, _ := protocolReferenceFixture(t)
	five, six := set.For(5), set.For(6)
	if five == nil || six == nil || five.Path == six.Path {
		t.Fatalf("protocol files %+v and %+v must be distinct", five, six)
	}
	if set.For(0) != six || set.For(releaseprotocol.Current+1) != six {
		t.Fatal("an unknown protocol must be named the current file")
	}
	// The contract-2 encoding, field for field the file a protocol-5 server wrote.
	want, err := json.Marshal(struct {
		ReferencesVersion     int                    `json:"referencesVersion"`
		SkillLifecycleVersion int                    `json:"skillLifecycleVersion"`
		RefSkillSnapshot      []SpawnSkillRow        `json:"refSkillSnapshot"`
		RefItemSnapshot       []RefItemRow           `json:"refItemSnapshot"`
		ItemCommandReferences []ItemCommandReference `json:"itemCommandReferences,omitempty"`
	}{2, 1, spawnSkillSnapshot(oneSkillCatalogue{}), []RefItemRow{{RefObjID: 51}}, nil})
	if err != nil {
		t.Fatal(err)
	}
	if got := decodedReferences(t, five); !bytes.Equal(got, want) {
		t.Fatalf("protocol-5 file\n%s\nwant\n%s", got, want)
	}
	var current struct {
		ReferencesVersion int         `json:"referencesVersion"`
		RefObjSnapshot    []RefObjRow `json:"refObjSnapshot"`
	}
	if err := json.Unmarshal(decodedReferences(t, six), &current); err != nil {
		t.Fatal(err)
	}
	if current.ReferencesVersion != 3 || len(current.RefObjSnapshot) != 1 || current.RefObjSnapshot[0].RefObjID != published.RefObjID {
		t.Fatalf("protocol-6 file %+v, want contract 3 with the published monster", current)
	}
	if len(five.objectIDs) != 0 || !six.objectIDs[published.RefObjID] {
		t.Fatal("only the contract-3 file may withhold monster rows from a login")
	}
}

/*
================
TestEachProtocolsLoginRows

A protocol-5 login carries every monster row (its file has none); a
protocol-6 login drops the rows its file publishes.
================
*/
func TestEachProtocolsLoginRows(t *testing.T) {
	_, deps, _, own := protocolReferenceFixture(t)
	for protocol, want := range map[int]int{5: 2, 6: 1} {
		projection := deps.forProtocol(protocol)
		login := Build(&projection, BootstrapRequest{CharacterName: chinaSpearman().Name})
		if len(login.RefObjSnapshot) != want {
			t.Fatalf("protocol %d login rows %+v, want %d", protocol, login.RefObjSnapshot, want)
		}
		if protocol == 6 && login.RefObjSnapshot[0].RefObjID != own.RefObjID {
			t.Fatalf("protocol 6 login rows %+v, want only the unpublished structure", login.RefObjSnapshot)
		}
	}
}

/*
================
TestTheEntryNamesItsProtocolsFile

The EnterWorld blob of each protocol names that protocol's file, and a
re-entry follows the protocol of the session bound to the character.
================
*/
func TestTheEntryNamesItsProtocolsFile(t *testing.T) {
	set, deps, _, _ := protocolReferenceFixture(t)
	for _, protocol := range []int{5, 6} {
		projection := deps.forProtocol(protocol)
		login := Build(&projection, BootstrapRequest{CharacterName: chinaSpearman().Name})
		blob, err := referenceEnterWorldBlob(login, projection.BrowserReferences)
		if err != nil {
			t.Fatal(err)
		}
		other := set.For(11 - protocol)
		if !strings.Contains(string(blob), set.For(protocol).SHA256) || strings.Contains(string(blob), other.SHA256) {
			t.Fatalf("protocol %d entry does not name its own file", protocol)
		}
	}
	deps.CharacterProtocol = func(string, string) int { return 5 }
	if projection := deps.forCharacterProtocol("division", "hero"); projection.BrowserReferences != set.For(5) {
		t.Fatal("a protocol-5 session's re-entry must name the contract-2 file")
	}
	deps.CharacterProtocol = func(string, string) int { return 0 }
	if projection := deps.forCharacterProtocol("division", "hero"); projection.BrowserReferences != set.For(6) {
		t.Fatal("an unbound character's re-entry must name the current file")
	}
}

/*
================
TestTheSetServesEveryFile
================
*/
func TestTheSetServesEveryFile(t *testing.T) {
	set, _, _, _ := protocolReferenceFixture(t)
	for _, protocol := range []int{5, 6} {
		recorder := httptest.NewRecorder()
		set.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, set.For(protocol).Path, nil))
		if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), decodedReferences(t, set.For(protocol))) {
			t.Fatalf("protocol %d file served %d", protocol, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	set.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/transport/references/unknown.json", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown reference file served %d", recorder.Code)
	}
	if _, err := NewBrowserReferences(BrowserReferenceSources{Skills: oneSkillCatalogue{}, Contract: 1}); err == nil {
		t.Fatal("contract 1 is served by no supported protocol")
	}
}
