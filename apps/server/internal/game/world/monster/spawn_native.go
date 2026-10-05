package monster

import "math"

// Native nest spawn (GameServer 5607B0 and its creation callee 5F6EB0),
// independently corroborated by the ISRO-R GameServer (593393..59349B).
// Every random draw is one VC CRT rand() result in 0..32767; callers pass the
// same projection used by the AI timers (SummonRandomWord). Draw order is the
// native order, so one deterministic source replays a spawn decision exactly.

// GiantMonsterSpawnRatio is the GiantMonster_SpawnRatio default (427099,
// 427508); neither shipped server.cfg overrides it.
const GiantMonsterSpawnRatio = 14

// NestHiveTickMs is the CAIHive population callback floor (55E924): a hive
// evaluates its nests at most once per second.
const NestHiveTickMs = 1000

// NestRetryFloorMs is 560E10's lower bound for a halved nest interval.
const NestRetryFloorMs = 1000

// Native move-test result bits (CRegionManagerBody slot 30, 98B300; the
// client runs the same JMX NavMesh walk at 412230).
const (
	// NavResultClipped: a blocked terrain edge (404510/9A0710) or a
	// side-blocked object edge (428930) committed the clipped point.
	NavResultClipped uint32 = 0x00000001
	// NavResultBlocked: a failing object leg (403FB0), a global edge without
	// a neighbour, or the six-call continuation limit (98B636).
	NavResultBlocked uint32 = 0x10000000
)

// NativeTypeWord is the full RefObj TypeID word the GameServer classifies
// spawns on: the client-facing TidWord plus TID4 in bits 11-15.
func NativeTypeWord(ref MonsterRef) uint16 {
	return ref.TidWord | uint16(ref.TypeID4&0x1f)<<11
}

// TradeNpcMonster is CGObj_IsThiefMonster (482640) or CGObj_IsHunterMonster
// (4826E0): the monster class with TID4 2 (thief) or 3 (hunter).
func TradeNpcMonster(ref MonsterRef) bool {
	tid := NativeTypeWord(ref)
	band := tid & typeID4Mask
	return nativeMonsterClass(tid) && (band == thiefTypeID4Band || band == hunterTypeID4Band)
}

const (
	typeID4Mask       uint16 = 0xf800
	thiefTypeID4Band  uint16 = 0x1000
	hunterTypeID4Band uint16 = 0x1800
)

// nativeMonsterClass is the 560836..560868 gate: bionic bit, TID1 character,
// TID2 NPC, TID3 monster. Only this class receives spawn grades and random
// headings.
func nativeMonsterClass(tid uint16) bool {
	return tid&2 != 0 && tid&0x1c == 4 && tid&0x60 == 0x40 && tid&0x780 == 0x80
}

// partyRollExcluded is 561020: monster TID4 4 never becomes a party monster.
func partyRollExcluded(tid uint16) bool {
	return nativeMonsterClass(tid) && tid&0xf800 == 0x2000
}

// staticSpawnGrade is 56086E..5608C6: these RefObjChar monster types are the
// spawn grade as authored; every other type starts ordinary and rolls.
func staticSpawnGrade(monsterType uint8) (uint8, bool) {
	switch monsterType {
	case 1, 3, 4, 5, 6, 7, 8:
		return monsterType, true
	}
	return 0, false
}

// NativeSpawnRoll is one attempt's spawn grade and heading decision.
type NativeSpawnRoll struct {
	// Nest carries the instance grade in RarityOverride (always set) and, for
	// a promoted champion/giant, the champion tactics row.
	Nest NestRow
	// HeadingRadians is the native float32 heading passed to creation.
	HeadingRadians float32
}

// Grade is the spawn byte: low nibble monster grade, high nibble party.
func (roll NativeSpawnRoll) Grade() uint8 { return roll.Nest.RarityOverride }

// RollNativeSpawn applies 5607B0 up to the creation call. partyArmed is the
// runtime nest flag (+30) armed by a qualifying party kill; the caller clears
// it when the created grade carries the party nibble (560CAD..560CB3). The
// decision is re-rolled on every attempt, including ones creation rejects.
func RollNativeSpawn(ref MonsterRef, nest NestRow, partyArmed bool, draw func() uint32) NativeSpawnRoll {
	grade := uint8(0)
	tid := NativeTypeWord(ref)
	monsterClass := nativeMonsterClass(tid)
	if monsterClass {
		if static, kept := staticSpawnGrade(ref.MonsterType); kept {
			grade = static
		} else {
			// 5608CF..5608F4: armed nests roll rand()%101 < 50 first; the TID4
			// exclusion is tested after that draw.
			if partyArmed && draw()%101 < 50 && !partyRollExcluded(tid) {
				grade = 0x10
			}
			// 5608F9..560982: promotion needs the tactics' champion row.
			if nest.HasChampionTactics && int(draw()%101) < nest.ChampionGenPercentage {
				if draw()%101 <= GiantMonsterSpawnRatio {
					grade = grade&0xf4 | 4
				} else {
					grade = grade&0xf1 | 1
				}
				nest = nest.PromoteToChampionTactics(grade)
			}
		}
	}
	nest.HasRarityOverride = true
	nest.RarityOverride = grade
	roll := NativeSpawnRoll{Nest: nest}
	if monsterClass && ref.MonsterType != 3 {
		// 5609EE..560A15: rand()/32767 * 2pi + 0, each step spilled to float32.
		fraction := float32(float64(draw()) / 32767.0)
		roll.HeadingRadians = float32(float64(fraction)*6.2831854820251465 + 0.0)
	} else {
		roll.HeadingRadians = InitialDirRadians(nest.InitialDir)
	}
	// 560AB7: the event replacement roll draws before its region gate even
	// though the default configuration (WINTER_EVENT_2009 off) leaves the
	// replacement table empty.
	draw()
	return roll
}

// InitialDirRadians is 5609BF..5609E8 / 560A1B..560A3C:
// word / 65535 * 360 * float32(pi/180), stored as float32.
func InitialDirRadians(word uint16) float32 {
	return float32(float64(word) / 65535.0 * 360.0 * 0.01745329238474369)
}

// NativeSpawnPosition is 531240 as creation 5F6EB0 calls it (zero minimum
// distance): the float32 nest centre moves by a banded offset. 70% of draws
// land in the outer third of the radius, 20% in the middle and 10% inside.
// moved is false when the generate radius does not exceed 1e-6; native then
// leaves the centre unchanged without drawing.
func NativeSpawnPosition(x, z, generateRadius float32, draw func() uint32) (float32, float32, bool) {
	radius := generateRadius
	if epsilon := float32(9.999999974752427e-07); radius < epsilon || radius == epsilon {
		return x, z, false
	}
	if !(1 < radius) {
		radius = 1
	}
	third := float32(float64(radius) / 3.0)
	var base float32
	switch band := draw() % 101; {
	case band < 70:
		base = float32(float64(third) + float64(third))
	case band >= 90:
		base = 0
	default:
		base = third
	}
	fraction := float32(float64(draw()) / 32767.0)
	distance := float32(float64(float32(float64(fraction)*float64(third))) + float64(base))
	angle := float32(float64(float32(float64(draw())/32767.0))*6.2831854820251465 + 0.0)
	// 489490 / 4894B0 return float32 cos/sin; the product is added to the
	// float32 coordinate before the single store.
	cos := float32(math.Cos(float64(angle)))
	sin := float32(math.Sin(float64(angle)))
	return float32(float64(x) + float64(cos)*float64(distance)),
		float32(float64(z) + float64(sin)*float64(distance)), true
}

// SpawnCollisionFallsBackToCentre is 5F7078..5F7096: when the move test from
// the nest centre reports NavResultBlocked, any nonzero grade nibble spawns at
// the centre, as do the COS classes 44B460 admits. Everything else fails
// creation without touching the nest timer.
func SpawnCollisionFallsBackToCentre(ref MonsterRef, grade uint8) bool {
	if grade&0x0f != 0 {
		return true
	}
	tid := NativeTypeWord(ref)
	tid4 := tid >> 11
	return tid&2 != 0 && tid&0x1c == 4 && tid&0x60 == 0x40 && tid&0x780 == 0x180 && (tid4 == 3 || tid4 == 4)
}

// NestDelay is 560380: the next nest interval in milliseconds after the
// timer's spawn-speed reduction (+0C, percent), and that reduction (+10).
func NestDelay(minSec, maxSec int, ratePct float32, draw func() uint32) (intervalMs, reduceMs uint32) {
	minMs := uint32(minSec) * 1000
	maxMs := uint32(maxSec) * 1000
	delay := minMs
	if maxMs > minMs {
		fraction := float32(float64(draw()) / 32767.0)
		delay = uint32(int64(math.Trunc(float64(fraction)*float64(maxMs-minMs)))) + minMs
	}
	reduceMs = uint32(int64(math.Trunc(float64(ratePct) * float64(delay) / 100.0)))
	return delay - reduceMs, reduceMs
}

// HalvedNestInterval is 560E10: a clipped creation retries after half the
// current interval, never sooner than one second.
func HalvedNestInterval(intervalMs uint32) uint32 {
	intervalMs >>= 1
	if intervalMs <= NestRetryFloorMs {
		return NestRetryFloorMs
	}
	return intervalMs
}

// HiveRespawnSelection is 55EEEE..55EF0A: when a full overwrite hive loses a
// member, rand() % (members+1) chooses the next location; the extra bucket
// folds onto the last member.
func HiveRespawnSelection(members int, draw func() uint32) int {
	selected := draw() % uint32(members+1)
	if last := uint32(members - 1); selected >= last {
		selected = last
	}
	return int(selected)
}
