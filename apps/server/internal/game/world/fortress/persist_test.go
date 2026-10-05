package fortress

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
memoryFortressStore

A FortressStore that keeps rows in maps and can be told to fail.
================
*/
type memoryFortressStore struct {
	records  map[uint32]domain.FortressRecord
	requests map[int64]domain.FortressRequestRecord
	fail     bool
}

func (m *memoryFortressStore) FortressState(string) ([]domain.FortressRecord, []domain.FortressRequestRecord, error) {
	var records []domain.FortressRecord
	for _, r := range m.records {
		records = append(records, r)
	}
	var requests []domain.FortressRequestRecord
	for _, r := range m.requests {
		requests = append(requests, r)
	}
	return records, requests, nil
}

func (m *memoryFortressStore) SaveFortress(_ string, record domain.FortressRecord) error {
	if m.fail {
		return errors.New("disk on fire")
	}
	m.records[record.FortressID] = record
	return nil
}

func (m *memoryFortressStore) SaveFortressRequest(_ string, request domain.FortressRequestRecord, present bool) error {
	if m.fail {
		return errors.New("disk on fire")
	}
	if present {
		m.requests[request.GuildID] = request
	} else {
		delete(m.requests, request.GuildID)
	}
	return nil
}

func (m *memoryFortressStore) FortressStructures(string) ([]domain.FortressStructureRecord, error) {
	return nil, nil
}

func (m *memoryFortressStore) SaveFortressStructure(string, domain.FortressStructureRecord, bool) error {
	return nil
}

/*
================
TestFortressRowsSurviveARestart

Restore brings back an occupation and the war's requests; requests and
captures save as they change, a request the store refuses is undone, and
the war's end clears its requests.
================
*/
func TestFortressRowsSurviveARestart(t *testing.T) {
	store := &memoryFortressStore{
		records:  map[uint32]domain.FortressRecord{1: {FortressID: 1, GuildID: 7}},
		requests: map[int64]domain.FortressRequestRecord{9: {FortressID: 1, GuildID: 9}},
	}
	a := New([]Catalog{{ID: 1, CodeName: "FORTRESS_JANGAN"}})
	if err := a.Restore("a", store); err != nil {
		t.Fatal(err)
	}
	record, _ := a.Get("a", 1)
	if record.GuildID != 7 || len(record.Applicants) != 1 {
		t.Fatalf("restored %+v", record)
	}
	if !a.SetApplication("a", 1, 11, RequestAlly, true) || store.requests[11].Kind != uint8(RequestAlly) {
		t.Fatal("the request did not save")
	}
	store.fail = true
	if a.SetApplication("a", 1, 12, RequestAttack, true) {
		t.Fatal("a request the store refused was confirmed")
	}
	if record, _ = a.Get("a", 1); len(record.Applicants) != 2 {
		t.Fatalf("the refused request stayed: %+v", record.Applicants)
	}
	store.fail = false
	a.Capture("a", 1, 9, 0)
	if store.records[1].TempGuildID != 9 {
		t.Fatalf("capture not saved: %+v", store.records[1])
	}
	a.FinishWar("a", 1)
	if store.records[1] != (domain.FortressRecord{FortressID: 1, GuildID: 9}) || len(store.requests) != 0 {
		t.Fatalf("war end saved %+v with requests %+v", store.records[1], store.requests)
	}
	stranger := &memoryFortressStore{records: map[uint32]domain.FortressRecord{4: {FortressID: 4}}}
	if err := New([]Catalog{{ID: 1}}).Restore("a", stranger); err == nil {
		t.Fatal("a stored fortress the shard does not serve was accepted")
	}
}
