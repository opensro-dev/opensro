/*
===========================================================================
charges_test.go - sequential stored casts and Mana Spring timed recovery.
===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestStoredCrowCharges
================
*/
func TestStoredCrowCharges(t *testing.T) {
	p := &domain.CharacterCOS{Codename: "COS_P_RAVEN_040", Level: 90}
	s, _ := PetSkill(p, 50)
	row := BroodRow(domain.PetRowCooldown{}, s, 1, 1000, Modifiers{})
	if row.Charges != 1 || row.Capacity != 3 || row.RechargeMs != 600000 || row.ReadyAtMs != 601000 {
		t.Fatal(row)
	}
	p.Training.Rows[1] = row
	StartRow(&p.Training, 1, s, 1, 1000, Modifiers{})
	if RowReady(&p.Training, 1, 600999) {
		t.Fatal("early recharge")
	}
	if !RowReady(&p.Training, 1, 601000) {
		t.Fatal("deadline not ready")
	}
	row = AdvanceCharges(p.Training.Rows[1], 1801000)
	if row.Charges != 3 || row.ReadyAtMs != 0 {
		t.Fatal(row)
	}
	row = BroodRow(row, s, 10, 1801000, Modifiers{})
	if row.RechargeMs != 300000 {
		t.Fatal(row)
	}
	basic, _ := PetSkill(p, 49)
	if next := BroodRow(row, basic, 10, 1801000, Modifiers{}); next.Capacity != 3 {
		t.Fatal("book swap lost stored charges", next)
	}
}

/*
================
TestManaSpringRecoversChargeNotCounter
================
*/
func TestManaSpringRecoversChargeNotCounter(t *testing.T) {
	p := &domain.CharacterCOS{}
	p.Training.Rows[0] = domain.PetRowCooldown{Kind: TriggerHits, Left: 10}
	p.Training.Rows[1] = domain.PetRowCooldown{Kind: ChargeKind, Capacity: 3, RechargeMs: 300000, ReadyAtMs: 1200}
	if !RecoverPetCooldowns(p, 1000, 500) {
		t.Fatal("no recovery")
	}
	if p.Training.Rows[0].Left != 10 || p.Training.Rows[1].Charges != 1 || p.Training.Rows[1].ReadyAtMs != 301000 {
		t.Fatal(p.Training.Rows)
	}
}
