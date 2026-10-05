/*
===========================================================================

catalog.go - monster templates, nests and live instance values

===========================================================================
*/

package monster

import (
	"iter"
	"math"
	"opensro.online/server/internal/game/abnormal"
	"sort"

	"opensro.online/server/internal/domain"
)

// GidBase is the bottom of the monster entity-id band. Existing Go bands:
// players 100000+characterID, NPCs 200000+characterID+index (per-session),
// ground drops 300000+counter - so monsters start above all three. The old
// Node convention (210000+) is deliberately NOT carried over: it sits
// inside the Go NPC band's reachable range (200000+characterID) and would
// silently hand one gid to two entities (monster-live board seq25/seq18).
const GidBase uint32 = domain.MonsterGIDBase

/*
==================
NestRow

NestRow is one v1.150 npcpos anchor enriched by a matching v1.188
Nest/Tactics row. Numeric ids never cross that version boundary: the
adapter joins by codename, region, and the rounded anchor coordinates.
==================
*/
type NestRow struct {
	// WorldCode joins the source hive's GameWorldID to the authored world
	// definition by name; numeric IDs from another version are not portable.
	WorldCode         string
	Controls          TacticsControls
	HasControls       bool
	ConditionalSkills [8]ConditionalSkill
	TargetPolicy      uint8 // native tactics+20: 0 retain, 1 latest, 2 scored
	SpawnPoint
	// EventStructID is a fortress structure's event zone (eventzonedata id):
	// the RefEventStructID its spawn row carries (CICATStruct 4FA0B0).
	EventStructID uint32
	// RetailEvidence reports that the server-side fields below came from a
	// matched population row. An unmatched v1.150 anchor remains passive but
	// receives the class-wide idle-wander primitive when its RefObj row has
	// nonzero walk speed; MaxCount zero in a matched row means zero.
	RetailEvidence bool
	// PolicyPinned says the population policy fields are intentionally
	// authored. RetailEvidence is provenance; resource-authored game areas set
	// PolicyPinned without pretending their rows came from the v1.188 tables.
	PolicyPinned bool
	// RarityOverride is an instance's full spawn byte: the grade 5607B0 rolled
	// (or a summon wave's authored grade). Template nests never carry one;
	// an instance without it inherits MonsterRef.MonsterType.
	RarityOverride    uint8
	HasRarityOverride bool
	// Radius is the Tab_RefNest nRadius containment radius.
	Radius float64
	// GenerateRadius is passed by CNest to the native spawn routine.
	GenerateRadius float64
	// ChampionGenPercentage is Tab_RefNest.nChampionGenPercentage.
	// The GameServer rolls it when activating an ordinary monster slot,
	// then splits a successful promotion between champion and giant.
	ChampionGenPercentage int
	// MaxCount is the native dwMaxTotalCount live-instance cap.
	MaxCount int
	// Shared population cap across alternative nest locations. Key is a hash
	// of codename/anchor membership, not a newer-server numeric entity ID.
	// HiveMaxCount is dwOverwriteMaxTotalCount; HiveOrder is this nest's rank
	// in native hive order (ascending dwNestID).
	HiveKey      string
	HiveMaxCount int
	HiveOrder    int
	HiveDensity  HiveDensityPolicy
	// Seconds are converted to milliseconds before 560380 samples its CRT
	// fraction. This is a millisecond interval, not an integer-second roll.
	RespawnDelayMinSec int
	RespawnDelayMaxSec int
	Respawn            bool
	// Aggressive is btAggressType == 0. Type 1 is passive.
	Aggressive bool
	SightRange float64
	// Raw reference-tactics +90 bits. Aggressive is a different native field;
	// never synthesize detection or acquisition cadence from that boolean.
	NativeTacticsFlags uint32
	// Champion is the tactics row named by this nest tactics' ChampionTacticsID
	// (runtime +8C). Without one, GameServer 5607B0 never promotes the slot.
	HasChampionTactics bool
	ChampionTactics    ChampionTactics
	// InitialDir is Tab_RefNest.wInitialDir: the authored heading word used
	// for uniques and non-monster classes (5609BF / 560A1B).
	InitialDir uint16
}

/*
==================
ChampionTactics

ChampionTactics includes the complete numeric row in Controls when sourced.
Storage coverage and runtime branch coverage are separate contracts.
A promoted champion or giant is created with this row instead of the nest's
ordinary tactics (5607B0 560968 -> 5F6EB0; ISRO-R 593470 -> 593488).
==================
*/
type ChampionTactics struct {
	Controls           TacticsControls
	HasControls        bool
	Aggressive         bool
	SightRange         float64
	NativeTacticsFlags uint32
	TargetPolicy       uint8
}

/*
==================
PromoteToChampionTactics

PromoteToChampionTactics returns the nest copy a promoted instance is
created with: population fields stay, while every tactics-owned field
comes from the champion row.
==================
*/
func (nest NestRow) PromoteToChampionTactics(rarity uint8) NestRow {
	nest.HasRarityOverride = true
	nest.RarityOverride = rarity
	nest.Aggressive = nest.ChampionTactics.Aggressive
	nest.SightRange = nest.ChampionTactics.SightRange
	nest.NativeTacticsFlags = nest.ChampionTactics.NativeTacticsFlags
	nest.TargetPolicy = nest.ChampionTactics.TargetPolicy
	nest.Controls, nest.HasControls = nest.ChampionTactics.Controls, nest.ChampionTactics.HasControls
	return nest
}

/*
==================
Template

Template is the immutable shipped-data population: every npcpos spawn
point whose refObjID passes the binary monster gate, joined with its
characterdata row. Built once at wiring time, shared read-only by every
division.
==================
*/
type Template struct {
	SummonRefs      []uint32
	Refs            map[uint32]MonsterRef
	Nests           []NestRow
	EvidenceMatches int
	// nestsByRegion indexes Nests by regionID for per-region
	// materialization.
	nestsByRegion map[uint16][]int
	// nestsByHive lists each overwrite hive's members in native hive order.
	nestsByHive map[string][]int
}

/*
==================
LoadTemplate

LoadTemplate builds the population template from one textdata directory
(the verified projection's textdata directory; both characterdata*.txt and npcpos.txt live there).
Empty data degrades to an empty template - simulation state over it allocates
nothing and emission stays empty, never a fault.
==================
*/
func LoadTemplate(textdataDir string) Template {
	// Evidence joins are compilation inputs. The returned template owns the
	// runtime projection; retaining these three source indexes duplicates it.
	populationEvidenceRows := combinePopulationEvidence(mustLoadPopulationEvidence(populationEvidenceTSV), mustLoadPopulationEvidence(populationSupplementTSV))
	laterDisabled := laterDisabledCodenames(populationEvidenceRows)
	populationTacticsControls := loadTacticsControls(tacticsControlsJSON)
	hiveCaps := loadHiveCaps(hiveCapsTSV)
	refs := LoadMonsterRefs(textdataDir)
	var nests []NestRow
	evidenceMatches := 0
	for _, point := range LoadSpawnPoints(textdataDir) {
		ref, ok := refs[point.RefObjID]
		if !ok {
			continue
		}
		nest := NestRow{SpawnPoint: point, WorldCode: "INS_DEFAULT"}
		if cap, ok := hiveCaps[evidenceKey(ref.Codename, point.RegionID, point.X, point.Y, point.Z)]; ok {
			nest.HiveKey, nest.HiveMaxCount, nest.HiveOrder = cap.Key, cap.Limit, cap.Order
			nest.HiveDensity = cap.Density
			nest.WorldCode = cap.WorldCode
		}
		if evidence, matched := populationEvidenceRows[evidenceKey(
			ref.Codename,
			point.RegionID,
			point.X,
			point.Y,
			point.Z,
		)]; matched {
			nest.RetailEvidence = true
			nest.PolicyPinned = true
			nest.Radius = evidence.Radius
			nest.GenerateRadius = evidence.GenerateRadius
			nest.ChampionGenPercentage = evidence.ChampionGenPercentage
			nest.MaxCount = evidence.MaxCount
			if laterDisabled[ref.Codename] {
				// v1.150 places and requires this monster; see laterDisabledCodenames.
				nest.MaxCount = unmatchedAnchorInstanceLimit
			}
			nest.RespawnDelayMinSec = evidence.RespawnDelayMinSec
			nest.RespawnDelayMaxSec = evidence.RespawnDelayMaxSec
			nest.Respawn = evidence.Respawn
			nest.Aggressive = evidence.Aggressive
			nest.TargetPolicy = evidence.TargetPolicy
			nest.SightRange = evidence.SightRange
			nest.NativeTacticsFlags = evidence.NativeTacticsFlags
			nest.HasChampionTactics = evidence.HasChampion
			nest.ChampionTactics = evidence.Champion
			nest.InitialDir = evidence.InitialDir
			if controls, ok := populationTacticsControls[evidenceKey(ref.Codename, point.RegionID, point.X, point.Y, point.Z)]; ok {
				nest.Controls, nest.HasControls = controls.Normal, true
				if nest.HasChampionTactics && controls.HasChampion {
					nest.ChampionTactics.Controls, nest.ChampionTactics.HasControls = controls.Champion, true
				}
				if nest.HasChampionTactics != controls.HasChampion {
					panic("champion controls disagree with population evidence: " + ref.Codename)
				}
			} else {
				panic("population evidence lacks its complete tactics row: " + ref.Codename)
			}
			evidenceMatches++
		}
		nests = append(nests, nest)
	}
	template := TemplateFromParts(refs, nests)
	template.EvidenceMatches = evidenceMatches
	return template
}

/*
==================
TemplateFromParts

TemplateFromParts assembles a template from explicit refs + nest rows,
building the region and hive indexes (construction outside the package -
tests and fixtures; LoadTemplate is the production path).
==================
*/
func TemplateFromParts(refs map[uint32]MonsterRef, nests []NestRow) Template {
	template := Template{Refs: refs, Nests: make([]NestRow, 0, len(nests)), nestsByRegion: make(map[uint16][]int), nestsByHive: make(map[string][]int)}
	for _, nest := range nests {
		if nest.HasRarityOverride {
			panic("monster: template nests carry no spawn grade; 5607B0 rolls it per spawn")
		}
		template.Nests = append(template.Nests, nest)
		index := len(template.Nests) - 1
		template.nestsByRegion[nest.RegionID] = append(template.nestsByRegion[nest.RegionID], index)
		if nest.HiveKey != "" {
			template.nestsByHive[nest.HiveKey] = append(template.nestsByHive[nest.HiveKey], index)
		}
	}
	for key, members := range template.nestsByHive {
		sort.SliceStable(members, func(i, j int) bool {
			return template.Nests[members[i]].HiveOrder < template.Nests[members[j]].HiveOrder
		})
		for _, index := range members {
			if template.Nests[index].WorldCode != template.Nests[members[0]].WorldCode {
				panic("monster: hive crosses world definitions")
			}
			if template.Nests[index].HiveDensity != template.Nests[members[0]].HiveDensity {
				panic("monster: hive " + key + " has inconsistent density policy")
			}
			if template.Nests[index].HiveMaxCount != template.Nests[members[0]].HiveMaxCount {
				panic("monster: overwrite hive " + key + " has inconsistent member limits")
			}
		}
	}
	return template
}

/*
==================
WithAdditionalNests

WithAdditionalNests returns a new immutable template with resource-authored
population rows appended and the region index rebuilt. The reference map is
shared read-only; nest slices and indexes are never aliased.
==================
*/
func (t Template) WithAdditionalNests(nests []NestRow) Template {
	combined := make([]NestRow, 0, len(t.Nests)+len(nests))
	combined = append(combined, t.Nests...)
	combined = append(combined, nests...)
	result := TemplateFromParts(t.Refs, combined)
	result.EvidenceMatches = t.EvidenceMatches
	result.SummonRefs = append([]uint32(nil), t.SummonRefs...)
	return result
}

/*
==================
SpawnableRefs

SpawnableRefs returns the monster references that actually appear in
the template's nest rows, ordered by RefObjID. This is the bootstrap
refObjSnapshot roster source: the client seeds its RefObj mirror ONCE
per session before the packet loop, so the snapshot must carry every
refObjId that can EVER stream to the session - an unseeded id arriving
on a later 0x30D7 is a silent client-side drop (WIP seq70 C contract;
there is no incremental roster seam and adding one would be
divergence).
==================
*/
func (t Template) SpawnableRefs() []MonsterRef {
	seen := make(map[uint32]bool)
	var refs []MonsterRef
	for _, id := range t.SummonRefs {
		if !seen[id] {
			seen[id] = true
			refs = append(refs, t.Refs[id])
		}
	}
	for _, nest := range t.Nests {
		if nest.InstanceLimit() == 0 {
			continue
		}
		if seen[nest.RefObjID] {
			continue
		}
		seen[nest.RefObjID] = true
		refs = append(refs, t.Refs[nest.RefObjID])
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].RefObjID < refs[j].RefObjID })
	return refs
}

/*
==================
InstanceLimit

InstanceLimit returns the authored live-instance cap for this nest. An
unmatched client anchor represents one fallback slot; an explicitly pinned
zero cap remains empty.
==================
*/
func (nest NestRow) InstanceLimit() int {
	if nest.PolicyPinned || nest.RetailEvidence {
		return nest.MaxCount
	}
	return unmatchedAnchorInstanceLimit
}

// unmatchedAnchorInstanceLimit is the live cap of a v1.150 anchor without a
// usable evidence cap: one fallback slot.
const unmatchedAnchorInstanceLimit = 1

/*
==================
NestIndexesInRegion

NestIndexesInRegion returns a copy of the immutable catalog index for one
region. Runtime population state lives in simulation; callers cannot retain
or mutate the catalog's internal index.
==================
*/
func (t Template) NestIndexesInRegion(regionID uint16) []int {
	return append([]int(nil), t.nestsByRegion[regionID]...)
}

/*
================
HiveNestIndexes

Return one overwrite hive's members in native hive order as a detached slice.
================
*/
func (t Template) HiveNestIndexes(key string) []int {
	return append([]int(nil), t.nestsByHive[key]...)
}

/*
================
HiveNestIndexAt

Expose a scalar without publishing the catalog's backing slice.
================
*/
func (t Template) HiveNestIndexAt(key string, ordinal int) int {
	return t.nestsByHive[key][ordinal]
}

/*
================
HiveNestIndexSequence

Iterate native hive order without lending mutable storage to simulation.
================
*/
func (t Template) HiveNestIndexSequence(key string) iter.Seq[int] {
	return func(yield func(int) bool) {
		for _, index := range t.nestsByHive[key] {
			if !yield(index) {
				return
			}
		}
	}
}

/*
==================
Instance

Instance is a value snapshot of one live monster. Mutable ownership lives
in simulation.MonsterState; feature code receives copies and cannot retain
a pointer into authoritative world state.
==================
*/
type Instance struct {
	LinkedEffects   *EffectSnapshot
	ConditionalUsed uint8
	SelfEffects     SelfEffects
	Help            HelpInbox
	Motion          MotionHold
	// Abnormal is the immutable abnormal-state block snapshot (nil when no
	// slot has ever been installed). Writers copy, mutate and replace it.
	Abnormal *abnormal.Block
	Gid      uint32
	Ref      MonsterRef
	// Nest is the population row the instance belongs to; Spawn is this
	// particular instance's generated home point inside Nest.GenerateRadius.
	Nest  NestRow
	Spawn SpawnPoint
	// NestDetached records removal of the live CNest attachment. The authored
	// row and its creation-time home remain intact; simulation owns this flag
	// together with population capacity, independently of actor lifetime.
	NestDetached bool
	// SpawnHeading is the wire heading word of the native creation heading
	// (5607B0: random for monsters, wInitialDir for uniques).
	SpawnHeading uint16
	// TradeVariant picks a thief or hunter trade NPC's equipment
	// (TradeNpcMonster): CGObjMob_ResetRuntimeState (4C1030) draws it as
	// rand() & 0xFF, CGObjMob_WriteSpawnData (4C1930) sends it after the
	// spawn grade, and CICMonster_InitializeTradeEquipmentAndSkill (client
	// 861720) indexes the trade equipment table with it.
	TradeVariant uint8
	// StructureState is a fortress structure's state word (+0x764 on the
	// client, CGObjSiegeStruct_GetStateWord 4CED50): bit 0 destroyed.
	StructureState uint16
	// CurrentHP is mutable instance state. Ref.MaxHP is only the static
	// RefObjChar base; EffectiveMaxHP applies the client-pinned rarity and
	// party-monster multipliers used by the target-status plane.
	CurrentHP uint32
	// AI +B0 accumulator; reset when the selected ssou command completes,
	// including command error (562540), before any delayed cast release.
	DamageSinceSummon   uint32
	LastSummonCommandMs uint32
	// Identity projection of native AI target records +BC/+D0. Simulation owns
	// insertion and clearing; this is separate from the mover's active pursuit.
	Opponents [2]Opponent
	// Zero is idle; a positive deadline owns an accepted summon action.
	SummonActionUntilMs int64
	// Summoned instances have no nest slot and never respawn independently.
	SummonerGID uint32
	// Acquisition uses default tactics plus body radius; follow uses leader action range.
	SummonSightRange    float64
	SummonerFollowRange float64
}

/*
==================
Rarity

Rarity returns the byte consumed by CICMonster spawn. Static RefObjChar
type is the base; an evidenced population source may override both the
low monster-type nibble and high party-monster nibble per instance.
==================
*/
func (instance Instance) Rarity() uint8 {
	if instance.Nest.HasRarityOverride {
		return instance.Nest.RarityOverride
	}
	return instance.Ref.MonsterType & 0x0f
}

/*
==================
EffectiveMaxHP

EffectiveMaxHP returns the per-instance maximum the v1.150 target-status
client derives from RefObjChar MaxHP and the spawn rarity byte:
champion x2, giant x20, titan x100, elite x4; a high nibble of 1 then
applies the party-monster x10 multiplier. Unknown grades retain the base.
==================
*/
func (instance Instance) EffectiveMaxHP() uint32 {
	rarity := instance.Rarity()
	multiplier := uint64(1)
	switch rarity & 0x0f {
	case 1:
		multiplier = 2
	case 4:
		multiplier = 20
	case 5:
		multiplier = 100
	case 6:
		multiplier = 4
	}
	if rarity>>4 == 1 {
		multiplier *= 10
	}
	scaled := uint64(instance.Ref.MaxHP) * multiplier
	if scaled > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(scaled)
}
