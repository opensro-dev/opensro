/*
===========================================================================

residue_reuse_test.go - a deleted row's unknown fields leave with it

A residue belongs to its row, not to its key, and two keys come back: a
ground gid is a slot the registry hands to a later drop, and a training
camp's id is its master's character id, so a reopened camp has the old
key. Each test plants a newer release's field, deletes the row through
its own writer, and checks the residue is gone, so no later record under
the same key can be written with it.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
)

/*
================
TestPickedUpGroundItemForgetsItsResidue
================
*/
func TestPickedUpGroundItemForgetsItsResidue(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	r := grounditem.NewRegistry()
	s.AttachGround(r)
	gid := r.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1, Position: grounditem.Point{RegionID: 0x6a9a}}).Gid
	s.Mutate("ground seed", nil)
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM ground_items WHERE division = ? AND gid = ?",
		"UPDATE ground_items SET record = ? WHERE division = ? AND gid = ?", testDivision, gid)

	s = openTest(t, dir, clock)
	key := groundResidueKey{division: testDivision, gid: gid}
	if s.residues.ground[key] == "" {
		t.Fatalf("the planted field was not kept at load")
	}
	r = grounditem.NewRegistry()
	r.Restore(s.GroundSnapshotForRestore())
	s.AttachGround(r)
	if _, ok := r.Remove(testDivision, gid); !ok {
		t.Fatalf("gid %d was not on the ground", gid)
	}
	s.Mutate("pickup", nil)
	s.Close()
	if residue, kept := s.residues.ground[key]; kept {
		t.Fatalf("gid %d is free for a later drop but still holds %s", gid, residue)
	}
}

/*
================
TestWholeGroundRewriteKeepsOnlyWrittenResidues

The whole-plane rewrite (a rename's ground pass) deletes every row and
writes back the live set: a residue whose row is not written back goes.
================
*/
func TestWholeGroundRewriteKeepsOnlyWrittenResidues(t *testing.T) {
	kept := groundResidueKey{division: testDivision, gid: 300001}
	gone := groundResidueKey{division: testDivision, gid: 300002}
	residues := map[groundResidueKey]unknownFields{kept: "{" + futureField + "}", gone: "{" + futureField + "}"}
	keepGroundResidues(residues, map[string][]domain.GroundItemRecord{testDivision: {{Gid: kept.gid}}})
	if _, ok := residues[kept]; !ok {
		t.Fatalf("the written row lost its residue")
	}
	if _, ok := residues[gone]; ok {
		t.Fatalf("a row that was not written back kept its residue")
	}
}

/*
================
TestDisbandedCampForgetsItsResidues

The camp id is the master's character id, so the master's next camp
would otherwise be written with this one's fields.
================
*/
func TestDisbandedCampForgetsItsResidues(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	master := guildTestCharacter("residuemaster")
	if err := s.CreateCharacter(testDivision, "account-a", master); err != nil {
		t.Fatal(err)
	}
	openedAtMs := clock.Now().UnixMilli()
	camp := openTestCamp(t, s, master)
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM training_camps WHERE division = ? AND camp_id = ?",
		"UPDATE training_camps SET record = ? WHERE division = ? AND camp_id = ?", testDivision, camp.ID)
	plantFutureField(t, dir, "SELECT record FROM training_camp_members WHERE division = ? AND camp_id = ? AND char_id = ?",
		"UPDATE training_camp_members SET record = ? WHERE division = ? AND camp_id = ? AND char_id = ?", testDivision, camp.ID, master.ID)

	s = openTest(t, dir, clock)
	campKey := groupResidueKey{division: testDivision, id: camp.ID}
	memberKey := memberResidueKey{division: testDivision, group: camp.ID, charID: master.ID}
	if s.residues.camps[campKey] == "" || s.residues.campMembers[memberKey] == "" {
		t.Fatalf("the planted fields were not kept at load")
	}
	disband := s.TrainingCamps().DisbandCamp(testDivision, master.ID, openedAtMs+campDisbandCooldownSec*1000+1000)
	if disband.Refusal != domain.TrainingCampDisbandAccepted {
		t.Fatalf("disband = %+v", disband)
	}
	s.Close()
	if residue, kept := s.residues.camps[campKey]; kept {
		t.Fatalf("camp %d is the master's id for the next camp but still holds %s", camp.ID, residue)
	}
	if residue, kept := s.residues.campMembers[memberKey]; kept {
		t.Fatalf("the disbanded camp's member still holds %s", residue)
	}
}
