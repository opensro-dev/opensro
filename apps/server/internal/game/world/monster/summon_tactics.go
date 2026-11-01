package monster

import (
	_ "embed"
	"strings"
)

// The reference server partitions default tactics by ChampionTacticsID
// (runtime +8C). Its fallback chain: own reference, then original
// reference; grade zero uses
// the normal list, nonzero the other list; missing list flips preference.
// Multiple candidates consume one CRT modulo draw; singleton lists do not.
//
//go:embed data/v1188_summon_tactics.tsv
var summonTacticsEvidence string

var summonTacticsByCode = loadSummonTactics()

type SummonTactics struct {
	TargetPolicy      uint8
	SightRange        float64
	NativeFlags       uint32
	Controls          TacticsControls
	HasControls       bool
	ConditionalSkills [8]ConditionalSkill
}

// A catalog-owned Tab_RefAISkill binding resolved by codename against the
// target version. Source tactics IDs never become target skill IDs.
type ConditionalSkill struct {
	TacticsID, SkillID              uint32
	Codename                        string
	ConditionType, Minimum, Maximum uint8
	Option, Data                    uint32
	SourceOffset                    uint64
}

func loadSummonTactics() map[string][2][]SummonTactics {
	out := map[string][2][]SummonTactics{}
	for line, text := range strings.Split(summonTacticsEvidence, "\n") {
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		cols := strings.Split(text, "\t")
		if len(cols) != 5 {
			panic("invalid summon tactics evidence")
		}
		group := 1
		if mustEvidenceBool(cols[1], line+1) {
			group = 0
		}
		rows := out[cols[0]]
		rows[group] = append(rows[group], SummonTactics{
			TargetPolicy: uint8(mustEvidenceUint(cols[4], 8, line+1)),
			SightRange:   mustEvidenceFloat(cols[2], line+1),
			NativeFlags:  uint32(mustEvidenceUint(cols[3], 32, line+1)),
		})
		out[cols[0]] = rows
	}
	loadSupplementalSummonTactics(out)
	return out
}

func SummonedSightRange(ref MonsterRef, grade uint8, random func() float64) (float64, bool) {
	row, ok := ResolveSummonTactics(ref, grade, random)
	return row.SightRange, ok
}

// ResolveSummonTactics selects one whole record, consuming at most one draw.
func ResolveSummonTactics(ref MonsterRef, grade uint8, random func() float64) (SummonTactics, bool) {
	rows, ok := summonTacticsByCode[ref.Codename]
	if !ok {
		rows, ok = summonTacticsByCode[ref.OriginalCodename]
	}
	if !ok {
		return SummonTactics{}, false
	}
	group := 0
	if grade&15 != 0 {
		group = 1
	}
	candidates := rows[group]
	if len(candidates) == 0 {
		candidates = rows[1-group]
	}
	if len(candidates) == 0 {
		return SummonTactics{}, false
	}
	index := uint32(0)
	if len(candidates) > 1 {
		index = SummonRandomWord(random()) % uint32(len(candidates))
	}
	chosen := candidates[index]
	chosen.SightRange = float64(float32(chosen.SightRange + ref.BodyRadius))
	return chosen, true
}
