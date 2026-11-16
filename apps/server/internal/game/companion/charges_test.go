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
	p := &domain.CharacterCOS{Codename: "COS_P_RAVEN_040", Level: 90, Training: domain.PetTraining{TreeVersion: 2}}
	s, _ := PetSkill(p, 50)
	row := BroodRow(domain.PetRowCooldown{}, s, 1, 1000, Modifiers{})
	if row.Charges != 1 || row.Capacity != 3 || row.RechargeMs != 1096551 || row.ReadyAtMs != 1097551 {
		t.Fatal(row)
	}
	p.Training.Rows[1] = row
	StartRow(&p.Training, 1, s, 1, 1000, Modifiers{})
	if RowReady(&p.Training, 1, row.ReadyAtMs-1) {
		t.Fatal("early recharge")
	}
	if !RowReady(&p.Training, 1, row.ReadyAtMs) {
		t.Fatal("deadline not ready")
	}
	row = AdvanceCharges(p.Training.Rows[1], row.ReadyAtMs+2*row.RechargeMs)
	if row.Charges != 3 || row.ReadyAtMs != 0 {
		t.Fatal(row)
	}
	last, _ := PetSkill(p, 51)
	row = BroodRow(row, last, 10, 4000000, Modifiers{})
	if row.RechargeMs != 900000 {
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
