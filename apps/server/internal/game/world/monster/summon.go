/*
===========================================================================

summon.go - unique monster summon policy and health-band selection

Implements the reference server's unique-encounter summon dispatch (its
health-band selection and random-set selection) and the nine-tuple ssou
entry layout.

===========================================================================
*/

package monster

/*
================
SummonEntry

One of the nine ssou tuples consumed by the reference server's summon
skill: reference, low-nibble grade, inclusive minimum/maximum count.
================
*/
type SummonEntry struct {
	RefObjID         uint32
	Grade            uint8
	Minimum, Maximum uint32
}

/*
================
SummonSkill
================
*/
type SummonSkill struct {
	Present   bool
	HPPercent uint16
	Entries   [9]SummonEntry
}

/*
================
SummonPolicy
================
*/
type SummonPolicy uint8

const (
	NoSummonPolicy SummonPolicy = iota
	SummonByHealthBand
	SummonByRandomSet
)

/*
================
UniqueSummonPolicy

Natural-key bindings of the reference server's tactics dispatch, not
version-dependent numeric monster IDs. L2/L3 rows retain the same authored skill family in
v1.150. Tactics 32900..33300 and 33500 dispatch the Qin-Shi Emperor Tomb
Floor 5 Guardians and Floor 6 BlackSnake/WhiteSnake (BeakYung) to the
unique-summon skill (SummonByHealthBand).
================
*/
func UniqueSummonPolicy(codename string) SummonPolicy {
	switch codename {
	case "MOB_CH_TIGERWOMAN", "MOB_CH_TIGERWOMAN_L2", "MOB_CH_TIGERWOMAN_L3",
		"MOB_OA_URUCHI", "MOB_OA_URUCHI_L2", "MOB_OA_URUCHI_L3",
		"MOB_EU_KERBEROS", "MOB_EU_KERBEROS_L3", "MOB_AM_IVY", "MOB_AM_IVY_L3",
		"MOB_TQ_NORTHGUARDIAN", "MOB_TQ_SOUTHGUARDIAN",
		"MOB_TQ_WESTGUARDIAN", "MOB_TQ_EASTGUARDIAN",
		"MOB_TQ_BLACKSNAKE", "MOB_TQ_WHITESNAKE":
		return SummonByHealthBand
	case "MOB_KK_ISYUTARU", "MOB_KK_ISYUTARU_L2", "MOB_KK_ISYUTARU_L3",
		"MOB_TK_BONELORD", "MOB_TK_BONELORD_L2", "MOB_TK_BONELORD_L3",
		"MOB_RM_TAHOMET", "MOB_RM_TAHOMET_L2", "MOB_RM_TAHOMET_L3":
		return SummonByRandomSet
	}
	return NoSummonPolicy
}

/*
================
summonHealthBand

Native stores the ratio as float before comparing to double literals.
Preserve the boundary behavior (including rounded 20/40/60/80 percent).
================
*/
func summonHealthBand(hp, maximum uint32) int {
	ratio := float64(float32(float64(hp) / float64(maximum)))
	switch {
	case ratio < .2:
		return 0
	case ratio < .4:
		return 1
	case ratio < .6:
		return 2
	case ratio < .8:
		return 3
	default:
		return 4
	}
}

/*
================
SummonDue
================
*/
func SummonDue(instance Instance) bool {
	maximum := instance.EffectiveMaxHP()
	return instance.CurrentHP != 0 && maximum != 0 && instance.SummonActionUntilMs == 0 &&
		UniqueSummonPolicy(instance.Ref.Codename) != NoSummonPolicy &&
		float32(float64(instance.DamageSinceSummon)/float64(maximum)*100) >= 10
}

/*
================
SelectSummon

Returns an index into the ssou-only list in default-skill order. A keeps the
first row in each authored band; B uses the native seven-slot ordering and
its rand()%1000 < 500 alternative. Missing A bands fall back to ordinary
combat, never to an invented wave.
================
*/
func SelectSummon(instance Instance, skills []SummonSkill, sample float64) (int, bool) {
	if !SummonDue(instance) {
		return 0, false
	}
	band := summonHealthBand(instance.CurrentHP, instance.EffectiveMaxHP())
	switch UniqueSummonPolicy(instance.Ref.Codename) {
	case SummonByHealthBand:
		wanted := min(3, 4-band)
		for i, skill := range skills {
			bucket := min(3, 4-summonHealthBand(uint32(skill.HPPercent), 100))
			if bucket == wanted {
				return i, true
			}
		}
	case SummonByRandomSet:
		index := 3
		if band < 4 {
			index = 3 - max(1, band)
			if SummonRandomWord(sample)%1000 >= 500 {
				index += 4
			}
		}
		if index < len(skills) {
			return index, true
		}
	}
	return 0, false
}

/*
================
SummonRandomWord

Projects the injected entropy source into the VC CRT rand domain. Preserve
modulo bias: uniform count buckets are not the native rule.
================
*/
func SummonRandomWord(sample float64) uint32 {
	if !(sample > 0) {
		return 0
	}
	if sample >= 1 {
		return 32767
	}
	return uint32(sample * 32768)
}
