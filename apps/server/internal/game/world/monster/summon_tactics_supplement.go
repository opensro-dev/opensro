package monster

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// A later server's tactics for the sixteen own references the primary table
// lacks (SummonTacticsSupplementFile). Existing records and the native
// own-before-original lookup remain authoritative.

type supplementalSummonTactics struct {
	Source, SourceSHA256 string
	Rows                 []struct {
		Codename             string
		PrimaryRefID         uint32
		Normal, Champion     []TacticsControls
		ObjectSourceOffset   uint64
		TacticsSourceOffsets []uint64
		ConditionalSkills    []ConditionalSkill
	}
}

func loadSupplementalSummonTactics(out SummonTacticsTable, data []byte) {
	var doc supplementalSummonTactics
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&doc); err != nil {
		panic(fmt.Errorf("summon tactics supplement: %w", err))
	}
	if len(doc.Rows) != 16 {
		panic("summon tactics supplement closure mismatch")
	}
	for _, row := range doc.Rows {
		if _, exists := out[row.Codename]; exists {
			panic("summon tactics source collision: " + row.Codename)
		}
		if row.Codename == "" || row.PrimaryRefID == 0 || row.ObjectSourceOffset == 0 || len(row.Normal) == 0 || len(row.Champion) == 0 || len(row.TacticsSourceOffsets) != len(row.Normal)+len(row.Champion) {
			panic("incomplete summon tactics supplement")
		}
		var groups [2][]SummonTactics
		for group, records := range [2][]TacticsControls{row.Normal, row.Champion} {
			for _, c := range records {
				if c.ID == 0 || c.ObjectID == 0 || c.SightRange < 0 || (c.ChampionID != 0) != (group == 0) || !c.HasAggroType || c.AggroType != 0 {
					panic("invalid or unsupported supplemental tactics group")
				}
				selected := SummonTactics{TargetPolicy: c.ChangeTarget, SightRange: float64(c.SightRange), NativeFlags: c.Flags, Controls: c, HasControls: true}
				n := 0
				for _, binding := range row.ConditionalSkills {
					if binding.TacticsID != c.ID {
						continue
					}
					if n == len(selected.ConditionalSkills) || binding.SkillID == 0 || binding.Codename == "" || binding.SourceOffset == 0 || binding.ConditionType != 0 || binding.Minimum != 0 || binding.Maximum != 0 || binding.Option != 0 {
						panic("unsupported or incomplete summoned conditional skill")
					}
					selected.ConditionalSkills[n] = binding
					n++
				}
				groups[group] = append(groups[group], selected)
			}
		}
		out[row.Codename] = groups
	}
}
