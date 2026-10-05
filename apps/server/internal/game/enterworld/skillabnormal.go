/*
===========================================================================

skillabnormal.go - a skill's abnormal-state blocks

===========================================================================
*/

package enterworld

import "opensro.online/server/internal/game/abnormal"

const (
	skillPulseTag = 0x70756c73 // puls, tagRefSkill+384
)

/*
==================
encodedAbnormalParams

encodedAbnormalParams projects the tagRefSkill abnormal pointers
(SkillGlobal_BuildParameterIndex 587630): one block per status tag, the
puls period and the caster getv requests at +500/+504/+53C. Repeated
blocks overwrite their pointer; the walk stops at ssou like every other
primary-block reader.
==================
*/
func encodedAbnormalParams(fields []string) abnormal.SkillParams {
	var out abnormal.SkillParams
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok || n == 0x73736f75 {
			break
		}
		arity := spawnParamArity(uint32(n))
		if i+arity >= len(fields) {
			break
		}
		args := make([]uint32, arity)
		for k := range args {
			args[k] = textdataU32(fields[i+1+k])
		}
		if index, found := abnormal.SourceIndex(uint32(n)); found {
			p := abnormal.Param{Present: true}
			copy(p.Args[:], args)
			out.Params[index] = p
		}
		switch uint32(n) {
		case skillPulseTag:
			out.Pulse, out.PulsePresent = args[0], true
		case tagGetv:
			switch args[0] {
			case abnormal.KeyPoisonDamage:
				out.PoisonDamageGetv = true
			case abnormal.KeyPoisonDuration:
				out.PoisonDurationGetv = true
			case abnormal.KeyTrapDamage:
				out.TrapDamageGetv = true
			}
		case 0x63757274: // curt -> +0x40C, two words packed as mask and level
			if len(args) >= 2 {
				out.CurtMask = args[0]
				out.CurtLevel = uint16(args[1])
				out.Curt = true
			}
		case 0x6375726c: // curl -> +0x410, pill mask, chance, grade
			if len(args) >= 3 {
				out.CurlMask = int32(args[0])
				out.CurlChance = int32(args[1])
				out.CurlGrade = int32(args[2])
				out.Curl = true
			}
		case 0x72637572: // rcur -> +0x414, selection limit
			if len(args) >= 1 {
				out.Rcur = int32(args[0])
				out.RcurSet = true
			}
		case 0x72657375: // resu -> +0x330 (588F8B): level ceiling, EXP percent
			out.AdmitDeadParty = true
			out.ResuMaxLevel, out.ResuExpPercent = args[0], args[1]
		case 0x726d7574: // rmut -> +0x4AC: the skill a revival starts
			out.Rmut = args[0]
		case tagEfr: // efr: the first word selects slot +0x28C/+0x290/+0x294
			// Only kind 1 (+0x28C) is the action area: SkillAction_Instant and
			// SkillAction_Projectile dispatch 58CB70 from it; kind 2 feeds
			// CastLifecycle_ProcessPersistent and is not a cure area.
			if len(args) >= 6 && args[0] == 1 {
				out.EffectArea = abnormal.EffectArea{
					Present: true, Kind: args[0], Shape: args[1], Radius: args[2],
					MaxTargets: args[3], Reduction: args[4], Select: args[5],
				}
			}
		}
		i += 1 + arity
	}
	return out
}

// validAbnormalBlock admits one status block for execution: unsigned
// signed-range words and a byte grade (tagSkillStatusEffect+18).
func validAbnormalBlock(fields []string, i int, source abnormal.Source) bool {
	if i+int(source.Arity) >= len(fields) {
		return false
	}
	for j := 1; j <= int(source.Arity); j++ {
		v, ok := textdataInt(fields[i+j])
		if !ok || v < 0 || v > 0x7fffffff {
			return false
		}
	}
	if source.Status >= abnormal.Sleep {
		grade, _ := textdataInt(fields[i+3])
		if grade > 255 {
			return false
		}
	}
	return true
}
