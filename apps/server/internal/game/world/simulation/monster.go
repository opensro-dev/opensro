/*
===========================================================================

monster.go - native monster create rows for bootstrap and live interest

One projection owns scalar state and active skill identities. Late viewers see
the same recipient tokens that existing viewers received through B419.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// Monster create-row emission (Q1, monster-live wave).
//
// Layout source: the prior monster-spawn board's binary derivation of
// sub_861b00 CICMonster_DeserializeSpawn (vt+0x64 @ vtable 0xc11e54): the
// monster row is the SAME shared bionic spawn tail the NPC row ships
// (sub_852f80 pos block -> sub_776170 packed movement -> sub_85fb20
// scalars -> sub_859d40 name) followed by ONE extra u8 rarity byte
// (@0x861b74; low nibble 0=normal/1=champion/4=giant/6=elite). HP is NOT
// on the create row. A selected monster receives current HP through the
// non-CICUser vitals block of 0xB45A; later damage results mutate it.
//
// The decoder layout and monster tidWord (0x00C6) are pinned directly to
// the v1.150 client. The scalar fields line up with the shipped v1.150
// characterdata columns and the independently labelled v1.188 server data.
// Per-instance rarity and AI inputs are resolved by monster from the
// evidence-backed server population.
/*
================
MonsterDef

Detached wire projection shared by initial object lists and live scope entry.
================
*/
type MonsterDef struct {
	LinkedEffects *monster.EffectSnapshot
	SelfEffects   monster.SelfEffects
	RefObjID      uint32
	// TidWord rides the refObjSnapshot mirror row (kind "monster"), not
	// the create row; the client's sub_851420 cascade classifies on it.
	TidWord  uint16
	Codename string
	// Name is the optional wire name the client-pinned sub_859d40
	// mask-bit0 leg carries. The web port deliberately sends the localized
	// retail name. This is a source-fidelity decision, not a claim that the
	// native retail producer populated the optional field.
	Name string
	// WalkSpeed / RunSpeed are the +0x24c/+0x250 sources from the
	// monster's own characterdata row (not the 20/50 fixture pair the NPC
	// roster ships).
	WalkSpeed float64
	RunSpeed  float64
	// ScaleDenom is the third scalar consumed by the same client routine.
	// It is per monster; shipped rows are not uniformly 100.
	ScaleDenom float64
	// Rarity is the packed per-instance monster byte. Low nibble selects
	// normal/champion/unique/giant/titan/elite; high nibble 1 is the party
	// monster scale branch.
	Rarity uint8
	// Native 85FB20 reads the live motion byte even though 858310 does
	// not reconstruct a missed knockdown animation on scope entry.
	MotionState uint8
	// Zero preserves the existing live-row encoding. Retained corpses use DEAD.
	LifeState uint8
	// Structure selects the CICATStruct row (BuildStructureCreateRow) and
	// carries its fields: hit points, event zone, state, TypeID4.
	Structure      bool
	CurrentHP      uint32
	EventStructID  uint32
	StructureState uint16
	TypeID4        uint8
	// TradeNpc rows end with TradeVariant (CGObjMob_WriteSpawnData 4C1930).
	TradeNpc     bool
	TradeVariant uint8
}

// MonsterRarityNormal is the +0x770 low-nibble value for an ordinary
// (non-champion) monster row.
const MonsterRarityNormal uint8 = 0

// MonsterAppearByte is the vt+0x68 appear/effect byte a 0x30D7 SINGLE
// spawn carries after the create row (object-list rows MUST NOT carry it
// - WIP C3 contract, fold-verified both arms). Value 1 = the drop-in
// presentation convention the grounditem and peervis singles already
// ship; the native effect-selector semantics beyond that are PROVISIONAL.
const MonsterAppearByte uint8 = 1

// MonsterSpawnSpeedChannel is the create-row +25 speed-index byte: 2
// selects the WALK channel in the client's packed-movement parse
// (moveLegHost secondFlag==2 -> speedIndex 0; anything else -> RUN). The
// prior-wave fixture shipped 1 (run), which made the client integrate
// monster goals at RUN speed while the server timed wander at WALK - the
// BUG-7 rubber-band's speed half (board seq597/600). Monsters spawn on
// the walk channel; chase flips to run via the pinned 0x3122 MOVE push.
const MonsterSpawnSpeedChannel uint8 = 2

// BuildMonsterCreateRow encodes one object-list create row for a monster
// standing at spawn (the sub_777220 -> monster leg -> vt+0x64 sub_861b00
// consumer). Byte-identical to the NPC row (BuildNpcCreateRow) through the
// name block - same standing movement block and mastery count 0 - then
// the monster rarity byte. The three scalar values come from the
// monster's characterdata row. The gid comes from the monster registry
// (the sole allocator for the 400000+ band); the position is the spawn
// point itself, not the NPC fixture's +8/+5 offset.
/*
================
BuildMonsterCreateRow

Serialize one complete native actor row, including active recipient identities.
================
*/
func BuildMonsterCreateRow(def MonsterDef, gid uint32, spawn Spawn) []byte {
	if def.Structure {
		return BuildStructureCreateRow(def, gid, spawn)
	}
	x := clampFloat(spawn.X, 0, 0xffff)
	z := clampFloat(spawn.Z, 0, 0xffff)
	// Indoor local coordinates are signed F32 values, not outdoor unsigned
	// packed-movement coordinates. Scope creates must agree with movement.
	if IsDungeonRegion(spawn.RegionID) {
		x, z = spawn.X, spawn.Z
	}

	w := wire.NewWriter(64)
	w.U32(def.RefObjID).
		U32(gid).
		U16(spawn.RegionID).
		F32(float32(x)).
		F32(float32(spawn.Y)).
		F32(float32(z)).
		U16(spawn.Angle).
		U8(0).
		U8(MonsterSpawnSpeedChannel).
		U8(0).
		U16(spawn.Angle).
		U8(def.LifeState).
		U8(def.MotionState).
		U8(0).
		F32(float32(def.WalkSpeed)).
		F32(float32(def.RunSpeed)).
		F32(float32(def.ScaleDenom))
	count := def.LinkedEffects.Len()
	for _, e := range def.SelfEffects {
		if e.Token != 0 {
			count++
		}
	}
	if count > maxMonsterSpawnSkills {
		panic("monster effect projection exceeds native spawn capacity")
	}
	w.U8(uint8(count))
	// Retail 85FB20 non-local actor: skill + token; these admitted programs
	// have no status/rider byte and monsters have no remaining-duration word.
	for _, e := range def.SelfEffects {
		if e.Token != 0 {
			w.U32(e.SkillID).U32(e.Token)
		}
	}
	for effect := range def.LinkedEffects.Entries() {
		w.U32(effect.SkillID).U32(effect.Token)
	}
	w.U8(1)
	name := []byte(def.Name)
	w.U16(uint16(len(name))).Bytes(name)
	w.U8(def.Rarity)
	if def.TradeNpc {
		w.U8(def.TradeVariant)
	}
	return w.Payload()
}

/*
================
BuildStructureCreateRow

A fortress structure's row, in CICATStruct_DeserializeSpawnData's order
(4FA0B0): RefObjID, its hit points, its event zone (RefEventStructID),
its state word (bit 2 the destroyed pose), the shared 85FB20 object
block, the 859D40 name mask and name, and for a headquarters (TID4 5) the
holding guild's id, zero here, with no name.
================
*/
func BuildStructureCreateRow(def MonsterDef, gid uint32, spawn Spawn) []byte {
	w := wire.NewWriter(64)
	w.U32(def.RefObjID).
		U32(def.CurrentHP).
		U32(def.EventStructID).
		U16(def.StructureState).
		U32(gid).
		U16(spawn.RegionID).
		F32(float32(clampFloat(spawn.X, 0, 0xffff))).
		F32(float32(spawn.Y)).
		F32(float32(clampFloat(spawn.Z, 0, 0xffff))).
		U16(spawn.Angle).
		U8(0).
		U8(MonsterSpawnSpeedChannel).
		U8(0).
		U16(spawn.Angle).
		U8(def.LifeState).
		U8(def.MotionState).
		U8(0).
		F32(float32(def.WalkSpeed)).
		F32(float32(def.RunSpeed)).
		F32(float32(def.ScaleDenom)).
		// A structure carries no buffs.
		U8(0).
		U8(1)
	name := []byte(def.Name)
	w.U16(uint16(len(name))).Bytes(name)
	if def.TypeID4 == structureHeadquarters {
		w.U32(0)
	}
	return w.Payload()
}

// structureHeadquarters is TID4 5, whose row ends with its guild.
const structureHeadquarters = 5

// BuildMonsterSpawnSingle encodes the 0x30D7 single-spawn body: the same
// create row plus the trailing vt+0x68 appear byte (sub_777220 single
// mode reads it; the list path does not).
/*
================
BuildMonsterSpawnSingle

Only single-spawn packets carry the trailing appearance selector.
================
*/
func BuildMonsterSpawnSingle(def MonsterDef, gid uint32, spawn Spawn) []byte {
	return append(BuildMonsterCreateRow(def, gid, spawn), MonsterAppearByte)
}

// MonsterWireDefFromInstance owns the projection shared by bootstrap object
// lists and live scope entry. A retained corpse is still a published object,
// but must not deserialize as a standing, living monster for a new viewer.
/*
================
MonsterWireDefFromInstance

Corpses retain identity and position but never advertise live attached effects.
================
*/
func MonsterWireDefFromInstance(instance monster.Instance, nowMs int64) MonsterDef {
	ref := instance.Ref
	def := MonsterDef{
		RefObjID: ref.RefObjID, TidWord: ref.TidWord, Codename: ref.Codename,
		Name: ref.DisplayName(), WalkSpeed: ref.WalkSpeed, RunSpeed: ref.RunSpeed,
		ScaleDenom: ref.ScaleDenom, Rarity: instance.Rarity(),
		MotionState: instance.Motion.StateAt(nowMs),
		Structure:   ref.Structure, CurrentHP: instance.CurrentHP,
		EventStructID: instance.Nest.EventStructID, TypeID4: ref.TypeID4,
		TradeNpc: monster.TradeNpcMonster(ref), TradeVariant: instance.TradeVariant,
	}
	if instance.CurrentHP == 0 {
		def.LifeState = wire.LifeStateDead
		def.MotionState = 0
	} else {
		def.LinkedEffects = instance.LinkedEffects
		for n, e := range instance.SelfEffects {
			if e.Active(nowMs) {
				def.SelfEffects[n] = e
			}
		}
	}
	return def
}
