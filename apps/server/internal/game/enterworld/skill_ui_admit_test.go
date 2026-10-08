package enterworld

import "testing"

/*
================
TestSkillUiAdmitMirrorsTheServerGates

Every player row publishes the inputs of its 58D8F0 gates exactly as the
server reads them, so the client's press prediction refuses what the
server refuses (BUG-066, BR-261007-0624). Monster rows publish none.
================
*/
func TestSkillUiAdmitMirrorsTheServerGates(t *testing.T) {
	skills := sharedShippedSkills(t)
	ammunition, reqi := false, false
	for _, row := range skills.SpawnSkillRows() {
		source, ok := skills.SkillByID(row.ID)
		if !ok || row.UI == nil {
			continue
		}
		admit := row.UI.Admit
		if !playerSkillCodename(source.Codename) {
			if admit != nil {
				t.Fatalf("monster skill %d (%s) published admission inputs", row.ID, source.Codename)
			}
			continue
		}
		if admit == nil {
			t.Fatalf("player skill %d (%s) published no admission inputs", row.ID, source.Codename)
		}
		gate, reqc := source.CastGate, source.Reqc
		serverOnly := gate.Rpkt || gate.Qest || gate.MschPresent || reqc.Dance || reqc.KnockedDown ||
			gate.HideGatePresent && (gate.HideGateMode == 1 || gate.HideGateMode == 2)
		if admit.Nmf != gate.Nmf || admit.ServerOnly != serverOnly ||
			admit.Berserk != (gate.HideGatePresent && !gate.TrapPresent) || admit.LowHP != reqc.LowHP ||
			admit.StealthStrike != reqc.Flag16 || admit.Teleports != (gate.Tele || gate.Tel3) ||
			admit.WeaponKinds != source.RequiredWeaponKinds || admit.Ammunition != (source.Ammunition.Count != 0) {
			t.Fatalf("skill %d (%s) admission inputs %+v disagree with its row", row.ID, source.Codename, *admit)
		}
		if source.Consumption.Pinned && (admit.HP != source.Consumption.HP || admit.HPPercent != source.Consumption.HPPercent) {
			t.Fatalf("skill %d HP cost %d/%d published as %d/%d", row.ID, source.Consumption.HP,
				source.Consumption.HPPercent, admit.HP, admit.HPPercent)
		}
		if (admit.Reqi != nil) != source.Reqi.Present {
			t.Fatalf("skill %d reqi presence %v published as %v", row.ID, source.Reqi.Present, admit.Reqi != nil)
		}
		if admit.Reqi != nil {
			reqi = true
			if admit.Reqi.All != source.Reqi.All || len(admit.Reqi.Pairs) != source.Reqi.Count {
				t.Fatalf("skill %d reqi %+v disagrees with %+v", row.ID, *admit.Reqi, source.Reqi)
			}
			for i, pair := range admit.Reqi.Pairs {
				if pair != [2]uint32{source.Reqi.Pairs[i].Kind, source.Reqi.Pairs[i].Value} {
					t.Fatalf("skill %d reqi pair %d = %v, want %+v", row.ID, i, pair, source.Reqi.Pairs[i])
				}
			}
		}
		ammunition = ammunition || admit.Ammunition
	}
	// The shipped data must exercise both: a bow or crossbow skill, and a
	// skill that names its equipment through reqi pairs.
	if !ammunition || !reqi {
		t.Fatalf("shipped skills exercised ammunition=%v reqi=%v, want both", ammunition, reqi)
	}
}
