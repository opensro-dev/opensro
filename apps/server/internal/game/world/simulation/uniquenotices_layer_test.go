/*
===========================================================================

uniquenotices_layer_test.go - a layer's uniques speak only where its world does

===========================================================================
*/

package simulation

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

// The Roc's summit and Tahomet's room: both type-1 instance worlds.
const (
	noticeTestAnnouncing instance.DefinitionID = 40
	noticeTestSilent     instance.DefinitionID = 41
)

/*
================
noticeTestState

A state with one rarity-3 kind and an open layer in each test world.
================
*/
func noticeTestState(t *testing.T) (*MonsterState, instance.Lease, instance.Lease) {
	t.Helper()
	s := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: "MOB_RM_ROC", TidWord: 0xc6, MaxHP: 100, MonsterType: 3},
	}, nil))
	s.SetRandomSource(func() float64 { return 0 })
	s.StartDivision("a")
	announcing, ok := s.OpenInstanceLayer("a", noticeTestAnnouncing)
	silent, ok2 := s.OpenInstanceLayer("a", noticeTestSilent)
	if !ok || !ok2 {
		t.Fatal("the test worlds did not open")
	}
	return s, announcing, silent
}

/*
================
noticeTestSpawn
================
*/
func noticeTestSpawn(t *testing.T, s *MonsterState, lease instance.Lease, announce bool) uint32 {
	t.Helper()
	gid, ok := s.SpawnQuestMonsterGID(QuestMonsterSpawn{Division: "a", Population: lease, Codename: "MOB_RM_ROC",
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, FixedPosition: true, NowMs: 1000,
		AnnounceUnique: announce})
	if !ok {
		t.Fatal("the unique did not spawn")
	}
	return gid
}

/*
================
noticeTestKill
================
*/
func noticeTestKill(t *testing.T, s *MonsterState, gid uint32, killer string) {
	t.Helper()
	if hit, ok := s.ApplyDamage("a", gid, 100); !ok || !hit.Fatal {
		t.Fatal("the unique did not fall")
	}
	s.RecordUniqueKiller("a", gid, killer)
}

/*
================
TestLayerUniqueNoticesSpeakOnlyForAnnouncingWorlds

An announcing world's layer tells the division of an announced spawn and
of the kill; a spawn that does not ask, a world that does not announce,
and a world switched back off stay silent.
================
*/
func TestLayerUniqueNoticesSpeakOnlyForAnnouncingWorlds(t *testing.T) {
	s, announcing, silent := noticeTestState(t)
	s.AnnounceUniqueWorld(noticeTestAnnouncing, true)

	roc := noticeTestSpawn(t, s, announcing, true)
	if got := s.DrainUniqueNotices("a"); len(got) != 1 || !bytes.Equal(got[0].Payload, []byte{5, 1, 0, 0, 0}) {
		t.Fatalf("announced spawn in an announcing world: %+v", got)
	}
	noticeTestSpawn(t, s, announcing, false)
	noticeTestSpawn(t, s, silent, true)
	if got := s.DrainUniqueNotices("a"); len(got) != 0 {
		t.Fatalf("an unannounced spawn or a silent world spoke: %+v", got)
	}
	noticeTestKill(t, s, roc, "asd2")
	want := []byte{6, 1, 0, 0, 0, 4, 0, 'a', 's', 'd', '2'}
	if got := s.DrainUniqueNotices("a"); len(got) != 1 || !bytes.Equal(got[0].Payload, want) {
		t.Fatalf("the kill in an announcing world: %+v", got)
	}
	if got := s.DrainUniqueNotices("b"); len(got) != 0 {
		t.Fatalf("another division heard it: %+v", got)
	}

	s.AnnounceUniqueWorld(noticeTestAnnouncing, false)
	noticeTestKill(t, s, noticeTestSpawn(t, s, announcing, true), "late")
	if got := s.DrainUniqueNotices("a"); len(got) != 0 {
		t.Fatalf("a world switched off spoke: %+v", got)
	}
}
