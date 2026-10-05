/*
===========================================================================

statusresistance.go - reat and real, shared by passives and buffs

A resistance passive (Protection) and a resistance buff (Holy Word, Poison
Circle) author the same two blocks. reat raises the keeper's flat status
reduction; real files a grade-keyed flat in each masked status's
resistance bucket. Both owners use these helpers so the rule stays one.

===========================================================================
*/

package combat

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
==================
StatusReductionWrites

595542..59568F: reat adds its value to the flat status reduction 0x91+i
for each mask bit i (0..5). The caller stamps the source.
==================
*/
func StatusReductionWrites(reat enterworld.SkillPassiveReat) []paramkeeper.Write {
	var writes []paramkeeper.Write
	for i := uint16(0); i < 6; i++ {
		if reat.Mask&(1<<i) != 0 {
			writes = append(writes, paramkeeper.Write{Parameter: 0x91 + i, Channel: paramkeeper.Flat, Value: float32(reat.Value)})
		}
	}
	return writes
}

/*
==================
FileStatusResistance

59DF20: file real's flat under its grade in each masked status's bucket.
5999E0 and 599740 decrement the ends of unsigned-key trees: highest grade,
then highest flat within that grade. Preserve the independent percent side.
filed distinguishes an empty bucket from a present zero-grade contribution.
==================
*/
func FileStatusResistance(out *[17]abnormal.Resistance, filed *[17]bool, real enterworld.SkillPassiveReal) {
	if real.Mask == 0 {
		return
	}
	for _, source := range abnormal.Sources {
		if source.Resist < 0 || real.Mask&source.Status.Bit() == 0 {
			continue
		}
		bucket := &out[source.Resist]
		if !filed[source.Resist] || real.Grade > uint32(bucket.Grade) ||
			real.Grade == uint32(bucket.Grade) && real.Flat > uint32(bucket.Flat) {
			bucket.Grade, bucket.Flat = int32(real.Grade), int32(real.Flat)
			filed[source.Resist] = true
		}
	}
}
