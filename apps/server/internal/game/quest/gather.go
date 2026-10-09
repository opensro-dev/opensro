/*
===========================================================================

gather.go - quest tool admission and admitted-character gathering timers

The item owner consumes the tool after admission. This owner retains the
short online countdown and awards the gathered material through inventory
authority. Leaving the world cancels the countdown, matching 8C8770.

Seven quests own tools. Ivy 2's knife (8BB6C0) cuts a vine after ten seconds
on a 50% roll. Cerberus 1's Long Scissors (QNO_EU_EASTEU_19, 8B2AB0) cut a
Golden Apple after ten seconds with no roll, and the Golden Apple lures a
quest Ladon whose Bloody Orbs are the quest's objective. Rahid 5's Essence
of Roc Mountain (QNO_RM_OLDWOMAN_5, 8A0AA0) gathers a Pile of Rainbow Grass
on each of seven peaks in order, the later peaks taking longer and the
earlier ones failing more often. Hidden Treasure 5's Seal Key
(QNO_CA_TREASURE_5, 8C7910) calls a Treasure Guardian at the Hungry Blood
Ong habitat, whose King's Treasure Box is the quest's objective. Demetri's
Shiny Moss (QNO_EU_ADVENTURER_1, 8B6840) lures a Red Spotted Crab by the
Troy wooden horse; its Yellow Eggs are the objective. Irina's amulet
orders (QNO_WC_WAREHOUSE_W_2 and _3, 895BC0 / 8970F0) turn each hunted
amulet into an authentic or a flawed one on a right-click.

===========================================================================
*/
package quest

import (
	"math"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	ivyMaterialQuest         = "QNO_EU_IVY_2"
	ivyKnife                 = "ITEM_QNO_EU_IVY_2_03"
	ivyVine                  = "ITEM_QNO_EU_IVY_2_02"
	ivyGatherSeconds         = 10
	ivyGatherRegion          = 0x655e
	ivyGatherRadius          = 600
	ivyGuardian              = "MOB_QT_02_PUNISHER_CLON"
	ivyGuardianRadius        = 20
	cerberusQuest            = "QNO_EU_EASTEU_19"
	cerberusScissors         = "ITEM_QNO_EU_EASTEU_19_03"
	cerberusApple            = "ITEM_QNO_EU_EASTEU_19_01"
	cerberusLadon            = "MOB_QT_01_LADON"
	cerberusCutSeconds       = 10
	cerberusRegion           = 0x634c
	cerberusRadius           = 400
	cerberusHeldLimit        = 20
	cerberusLureMin          = 20
	cerberusLureSpan         = 80
	rahidQuest               = "QNO_RM_OLDWOMAN_5"
	rahidEssence             = "ITEM_QNO_RM_OLDWOMAN_5_08"
	rahidPile                = "ITEM_QNO_RM_OLDWOMAN_7_03"
	rahidPileTarget          = 7
	adventurerQuest          = "QNO_EU_ADVENTURER_1"
	adventurerMoss           = "ITEM_QNO_EU_ADVENTURER_1_01"
	adventurerCrab           = "MOB_QT_01_CRAB"
	adventurerRegion         = 0x685a
	adventurerRadius         = 420 // 8B6840: 0x1A4
	adventurerCrabMin        = 20
	adventurerCrabSpan       = 80
	amuletFirstQuest         = "QNO_WC_WAREHOUSE_W_2"
	amuletSecondQuest        = "QNO_WC_WAREHOUSE_W_3"
	amuletHunted             = "ITEM_QNO_WC_WAREHOUSE_W_2_01"
	amuletAuthentic          = "ITEM_QNO_WC_WAREHOUSE_W_2_02"
	amuletFlawed             = "ITEM_QNO_WC_WAREHOUSE_W_2_03"
	treasureQuest            = "QNO_CA_TREASURE_5"
	treasureKey              = "ITEM_QNO_CA_TREASURE_5_01"
	treasureBox              = "ITEM_QNO_CA_TREASURE_5_02"
	treasureGuardian         = "MOB_QT_01_ONG"
	treasureKeyCount         = 5
	treasureRegion           = 0x666b
	treasureRadius           = 400
	treasureGuardianMin      = 20
	treasureGuardianSpan     = 80
	treasureGuardianLifeMs   = 300000 // 8C7910: CEVENTHandler_SetTiming 0x493E0
	questSecondMs            = 1000
	OpQuestGatherStart       = 0x36bd
	OpQuestGatherCancel      = 0x775d
	OpQuestGatherCancelReply = 0xb75d
)

/*
================
gatherRoll

How a countdown's end is rolled: never (Cerberus), failing below 50 of
rand % 101 (Ivy's 8BB5F0), or failing above the step's chance (Rahid's
8A0970).
================
*/
type gatherRoll uint8

const (
	gatherAlways gatherRoll = iota
	gatherIvyRoll
	gatherAtMost
)

/*
================
gatherJob

Transient actor state. Persisted quest progress never doubles as a countdown.
award is the item cut when the countdown ends; failSymbol is the notice of a
failed roll and full the notice of a full bag. advanceStep moves the quest's
ToolStep on a successful roll.
================
*/
type gatherJob struct {
	quest            uint32
	remaining        uint8
	nextMs           int64
	award            string
	failSymbol, full string
	roll             gatherRoll
	chance           uint8
	advanceStep      bool
}

/*
================
rahidPeak

One of the seven peaks of 8A0AA0's table: the area, the gathering seconds
and the chance in rand % 101 that 8A0970 grants the Pile.
================
*/
type rahidPeak struct {
	region          uint16
	x, y, z, radius float64
	seconds, chance uint8
}

var rahidPeaks = [rahidPileTarget]rahidPeak{
	{region: 0x5e74, x: 1081, y: 5775, z: 1631, radius: 270, seconds: 5, chance: 25},
	{region: 0x5f6b, x: 1282, y: 5590, z: 285, radius: 280, seconds: 10, chance: 30},
	{region: 0x5c70, x: 44, y: 4767, z: 1898, radius: 300, seconds: 15, chance: 40},
	{region: 0x5a71, x: 1825, y: 4689, z: 1871, radius: 270, seconds: 20, chance: 50},
	{region: 0x596d, x: 1322, y: 4378, z: 1761, radius: 270, seconds: 25, chance: 60},
	{region: 0x5674, x: 1023, y: 5785, z: 1845, radius: 270, seconds: 30, chance: 75},
	{region: 0x576b, x: 1013, y: 5910, z: 85, radius: 280, seconds: 40, chance: 100},
}

/*
================
questToolCleanup

Materials and tools have different identities. Completion and abandonment
discard unused knives without touching the traps awarded by the same quest.
Cerberus 1 registers its Golden Apple as a quest item (8B2130 +0x164), so
leftover apples go too; its scissors leave with the supply cleanup.
================
*/
func questToolCleanup(c *enterworld.Character, def *Definition) []inventory.ItemAmount {
	tool := ""
	switch def.Codename {
	case ivyMaterialQuest:
		tool = ivyKnife
	case cerberusQuest:
		tool = cerberusApple
	case rahidQuest:
		// INFERENCE: the Essences are the gather mission's +0x241 item; the
		// base mission cleanup that removes collected mission items takes
		// them with the quest.
		tool = rahidEssence
	default:
		return nil
	}
	count := captureItemCount(c, tool)
	if count == 0 {
		return nil
	}
	return []inventory.ItemAmount{{Codename: tool, Count: count}}
}

/*
================
withinQuestArea

Outdoor-only planar distance to a quest area's centre.
================
*/
func withinQuestArea(at, center simulation.Spawn, radius float64) bool {
	distance := simulation.WorldDistance2D(at, center)
	return !simulation.IsDungeonRegion(at.RegionID) && !math.IsNaN(distance) && !math.IsInf(distance, 0) && distance < radius
}

/*
================
startGatherJob

Opens the countdown and its client bar, unless one is already running.
================
*/
func (rt *Runtime) startGatherJob(c *enterworld.Character, job gatherJob) ([]wire.Frame, bool) {
	rt.gatherMu.Lock()
	defer rt.gatherMu.Unlock()
	if _, pending := rt.gatherJobs[c.ID]; pending {
		return nil, false
	}
	if rt.gatherJobs == nil {
		rt.gatherJobs = make(map[int64]gatherJob)
	}
	rt.gatherJobs[c.ID] = job
	return []wire.Frame{{Opcode: OpQuestGatherStart,
		Payload: wire.NewWriter(5).U32(job.quest).U8(job.remaining).Payload()}}, true
}

/*
================
emptyQuestBagSlot

Native tool admission requires an empty slot before consuming the knife,
even when its resulting material could stack onto an existing inventory row.
================
*/
func emptyQuestBagSlot(c *enterworld.Character) bool {
	var occupied [inventory.MaxBagEnd]bool
	for _, row := range c.MissionInventory {
		if inventory.InBag(c, row.Slot) {
			occupied[row.Slot] = true
		}
	}
	for slot := inventory.EquipmentSlotEnd; slot < inventory.BagEnd(c); slot++ {
		if !occupied[slot] {
			return true
		}
	}
	return false
}

/*
================
BeginItemUse

Routes a quest tool to its quest's handler. The caller holds the character
door and consumes the tool only when admitted.
================
*/
func (rt *Runtime) BeginItemUse(c *enterworld.Character, code string, at simulation.Spawn, nowMs int64) ([]wire.Frame, bool) {
	if c == nil || c.DeletePending || rt.PlanInventory == nil || !enterworld.CharacterAlive(c) {
		return nil, false
	}
	quest := ""
	switch code {
	case ivyKnife:
		quest = ivyMaterialQuest
	case cerberusScissors, cerberusApple:
		quest = cerberusQuest
	case rahidEssence:
		quest = rahidQuest
	case treasureKey:
		quest = treasureQuest
	case adventurerMoss:
		quest = adventurerQuest
	case amuletHunted:
		return rt.useAmulet(c)
	default:
		return nil, false
	}
	def, exists := rt.Defs.ByCodename(quest)
	if !exists || activeQuestIndex(c, def.RefID) < 0 {
		return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
	}
	switch code {
	case ivyKnife:
		return rt.beginIvyKnife(c, def, at, nowMs)
	case cerberusScissors:
		return rt.beginCerberusScissors(c, def, at, nowMs)
	case rahidEssence:
		return rt.beginRahidEssence(c, def, at, nowMs)
	case treasureKey:
		return rt.useTreasureKey(c, def, at)
	case adventurerMoss:
		return rt.useShinyMoss(c, def, at)
	}
	return rt.useCerberusApple(c, def, at)
}

/*
================
beginRahidEssence

8A0AA0 admits the Essence only at the peak of the current step, within its
radius and while fewer than seven Piles are held; then it needs an empty
slot (_16). Anything else is the wrong place (_17). Native also requires
quest state 7 or 8, set by the first graded Rocky kill (91C4D0): the only
Essence source, so holding one implies it.
================
*/
func (rt *Runtime) beginRahidEssence(c *enterworld.Character, def *Definition, at simulation.Spawn, nowMs int64) ([]wire.Frame, bool) {
	step := int(c.ActiveQuests[activeQuestIndex(c, def.RefID)].ToolStep)
	if step < len(rahidPeaks) {
		peak := rahidPeaks[step]
		center := simulation.Spawn{RegionID: peak.region, X: peak.x, Y: peak.y, Z: peak.z}
		if withinQuestArea(at, center, peak.radius) && captureItemCount(c, rahidPile) < rahidPileTarget {
			if !emptyQuestBagSlot(c) {
				return []wire.Frame{questNotification("SN_TALK_QNO_RM_OLDWOMAN_5_16")}, false
			}
			return rt.startGatherJob(c, gatherJob{quest: def.RefID, remaining: peak.seconds, nextMs: nowMs + questSecondMs,
				award: rahidPile, failSymbol: "SN_TALK_QNO_RM_OLDWOMAN_5_18", roll: gatherAtMost, chance: peak.chance,
				advanceStep: true})
		}
	}
	return []wire.Frame{questNotification("SN_TALK_QNO_RM_OLDWOMAN_5_17")}, false
}

/*
================
beginIvyKnife

8BB6C0 checks the Pond Ruins radius, material cap and empty inventory slot.
================
*/
func (rt *Runtime) beginIvyKnife(c *enterworld.Character, def *Definition, at simulation.Spawn, nowMs int64) ([]wire.Frame, bool) {
	if !withinQuestArea(at, simulation.Spawn{RegionID: ivyGatherRegion, X: 253, Y: 85, Z: 449}, ivyGatherRadius) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_IVY_2_09")}, false
	}
	if heldCollectCount(c, def) >= def.CollectCount {
		return nil, false
	}
	if !emptyQuestBagSlot(c) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_IVY_2_15")}, false
	}
	return rt.startGatherJob(c, gatherJob{quest: def.RefID, remaining: ivyGatherSeconds, nextMs: nowMs + questSecondMs,
		award: ivyVine, failSymbol: "SN_TALK_QNO_EU_IVY_2_10", full: "SN_TALK_QNO_EU_IVY_2_15", roll: gatherIvyRoll})
}

/*
================
beginCerberusScissors

8B2AB0 mode 1: the Long Scissors work only within 400 of the lure point,
while fewer than twenty Golden Apples are held, and with an empty slot
(_11 "Item use has failed"). The ten-second cut then always yields an
apple (8B2A10 has no roll, unlike Ivy's 8BB5F0).
================
*/
func (rt *Runtime) beginCerberusScissors(c *enterworld.Character, def *Definition, at simulation.Spawn, nowMs int64) ([]wire.Frame, bool) {
	if !withinQuestArea(at, cerberusLurePoint(), cerberusRadius) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_EASTEU_19_12")}, false
	}
	if captureItemCount(c, cerberusApple) >= cerberusHeldLimit {
		return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
	}
	if !emptyQuestBagSlot(c) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_EASTEU_19_11")}, false
	}
	return rt.startGatherJob(c, gatherJob{quest: def.RefID, remaining: cerberusCutSeconds, nextMs: nowMs + questSecondMs,
		award: cerberusApple})
}

/*
================
useCerberusApple

8B2AB0 mode 2: a Golden Apple lures a quest Ladon 20-100 from the player
while fewer than twenty Bloody Orbs are held, inside the same area (_13).
The native mission-state-8 refusal is the finished objective, which the
orb cap already refuses. A failed spawn keeps the apple.
================
*/
func (rt *Runtime) useCerberusApple(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if heldCollectCount(c, def) >= cerberusHeldLimit {
		return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
	}
	if !withinQuestArea(at, cerberusLurePoint(), cerberusRadius) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_EASTEU_19_13")}, false
	}
	lure := simulation.QuestMonsterSpawn{Codename: cerberusLadon, RadiusMin: cerberusLureMin, RadiusSpan: cerberusLureSpan}
	if rt.SpawnQuestMonster == nil || !rt.SpawnQuestMonster(c, lure) {
		return nil, false
	}
	return nil, true
}

/*
================
useTreasureKey

8C7910: a Seal Key works only while the quest runs unfinished (state 1 or
7; holding the Treasure Box is state 8) and within 400 of the habitat
point, else _17. It calls the Treasure Guardian 20-100 from that point,
not from the player as Cerberus's apple does, and removes it after five
minutes. A failed spawn keeps the key.
================
*/
func (rt *Runtime) useTreasureKey(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if heldCollectCount(c, def) >= def.CollectCount {
		return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
	}
	if !withinQuestArea(at, treasureHabitat(), treasureRadius) {
		return []wire.Frame{questNotification("SN_TALK_QNO_CA_TREASURE_5_17")}, false
	}
	guardian := simulation.QuestMonsterSpawn{Codename: treasureGuardian, Position: treasureHabitat(), FixedPosition: true,
		RadiusMin: treasureGuardianMin, RadiusSpan: treasureGuardianSpan, LifetimeMs: treasureGuardianLifeMs}
	if rt.SpawnQuestMonster == nil || !rt.SpawnQuestMonster(c, guardian) {
		return nil, false
	}
	return nil, true
}

/*
================
useShinyMoss

8B6840: with the Yellow Eggs gathered (quest state 8) the moss answers _09;
otherwise it works only within 420 of the Troy wooden horse, else _08. It
lures MOB_QT_01_CRAB 20-100 from the player, as Cerberus's apple does
(the spawn ring's centre is the player's position from vtable +0x158),
with no timer of its own. A failed spawn keeps the moss.
================
*/
func (rt *Runtime) useShinyMoss(c *enterworld.Character, def *Definition, at simulation.Spawn) ([]wire.Frame, bool) {
	if objectiveMet(c, def, c.ActiveQuests[activeQuestIndex(c, def.RefID)]) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_ADVENTURER_1_09")}, false
	}
	if !withinQuestArea(at, adventurerHorse(), adventurerRadius) {
		return []wire.Frame{questNotification("SN_TALK_QNO_EU_ADVENTURER_1_08")}, false
	}
	crab := simulation.QuestMonsterSpawn{Codename: adventurerCrab, RadiusMin: adventurerCrabMin, RadiusSpan: adventurerCrabSpan}
	if rt.SpawnQuestMonster == nil || !rt.SpawnQuestMonster(c, crab) {
		return nil, false
	}
	return nil, true
}

/*
================
useAmulet

895BC0 / 8970F0 (one per order, both on the W_2 items): while the order is
running, a hunted amulet becomes an authentic one or a flawed one on an
even or odd rand(), until the order's count of authentic ones is held;
then it is refused and kept. The grant lands before the amulet is spent,
so a full bag keeps it too. The orders never overlap: the second needs the
first completed twice.
INFERENCE: the v1.188 handlers stop at 0x46 and 0x82, their classes' own
counts; v1.150's lines ask for 100 and 200, so the stop is the order's
count.
================
*/
func (rt *Runtime) useAmulet(c *enterworld.Character) ([]wire.Frame, bool) {
	var def *Definition
	for _, code := range []string{amuletFirstQuest, amuletSecondQuest} {
		if d, exists := rt.Defs.ByCodename(code); exists && activeQuestIndex(c, d.RefID) >= 0 {
			def = d
		}
	}
	if def == nil || def.Objective != ObjectiveParallel {
		return []wire.Frame{questNotification("UIIT_MSG_QUEST_ERR_CANNOT_USE_ITEM")}, false
	}
	if captureItemCount(c, amuletAuthentic) >= def.Objectives[0].CollectCount {
		return nil, false
	}
	roll := rt.CaptureRoll
	if roll == nil {
		roll = combat.SecureRoll32767
	}
	value, err := roll()
	if err != nil {
		return nil, false
	}
	award := amuletAuthentic
	if value&1 != 0 {
		award = amuletFlawed
	}
	rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: award, Count: 1}})
	if err != nil {
		return nil, false
	}
	c.MissionInventory = rows
	objectives, _ := rt.applyInventoryChange(c)
	return append(updates, objectives...), true
}

/*
================
adventurerHorse

8B6840 compares against region 0x685A (986, 20, 340), the Troy wooden
horse northeast of Droa Dock.
================
*/
func adventurerHorse() simulation.Spawn {
	return simulation.Spawn{RegionID: adventurerRegion, X: 986, Y: 20, Z: 340}
}

/*
================
treasureHabitat

8C7910 compares against region 0x666B (1525, 152, 1495), south of
Samarkand.
================
*/
func treasureHabitat() simulation.Spawn {
	return simulation.Spawn{RegionID: treasureRegion, X: 1525, Y: 152, Z: 1495}
}

/*
================
cerberusLurePoint

8B2AB0 compares against region 0x634C (126, -41, 1863).
================
*/
func cerberusLurePoint() simulation.Spawn {
	return simulation.Spawn{RegionID: cerberusRegion, X: 126, Y: -41, Z: 1863}
}

/*
================
HandleGatherCancel

766950 accepts B75D only for the current quest identity. Cancellation owns
the same short-job lock as its timer, so an acknowledged cancellation cannot
subsequently award a material. A consumed knife is never refunded.
================
*/
func (rt *Runtime) HandleGatherCancel(c *enterworld.Character, payload []byte) (OpResult, error) {
	id, err := DecodeQuestRefRequest(payload)
	if err != nil || c == nil {
		return OpResult{Frames: []wire.Frame{{Opcode: OpQuestGatherCancelReply, Payload: []byte{2, 0}}}}, nil
	}
	rt.gatherMu.Lock()
	defer rt.gatherMu.Unlock()
	job, exists := rt.gatherJobs[c.ID]
	if !exists || job.quest != id {
		return OpResult{Frames: []wire.Frame{{Opcode: OpQuestGatherCancelReply, Payload: []byte{2, 0}}}}, nil
	}
	delete(rt.gatherJobs, c.ID)
	return OpResult{Frames: []wire.Frame{{Opcode: OpQuestGatherCancelReply,
		Payload: wire.NewWriter(5).U8(1).U32(id).Payload()}}}, nil
}

/*
================
ForgetItemUse

Actor teardown cancels the transient countdown; reconnect cannot release it.
================
*/
func (rt *Runtime) ForgetItemUse(c *enterworld.Character) {
	if c == nil {
		return
	}
	rt.gatherMu.Lock()
	delete(rt.gatherJobs, c.ID)
	rt.gatherMu.Unlock()
}

/*
================
AdvanceItemUse

Advance once per admitted second (quest event 0x18). Ivy's 8BB5F0 fails
rolls below 50, so its success boundary differs from trap capture; Cerberus's
8B2A10 never rolls; Rahid's 8A0970 fails rolls above the peak's chance and
moves to the next peak on success even when the bag refuses the Pile. Bag
changes during the wait are rechecked.
================
*/
func (rt *Runtime) AdvanceItemUse(c *enterworld.Character, nowMs int64) []wire.Frame {
	if c == nil {
		return nil
	}
	rt.gatherMu.Lock()
	job, pending := rt.gatherJobs[c.ID]
	if !pending || nowMs < job.nextMs {
		rt.gatherMu.Unlock()
		return nil
	}
	job.remaining--
	job.nextMs += questSecondMs
	if job.remaining > 0 {
		rt.gatherJobs[c.ID] = job
		rt.gatherMu.Unlock()
		return nil
	}
	delete(rt.gatherJobs, c.ID)
	rt.gatherMu.Unlock()
	var frames []wire.Frame
	rt.deps.Update(c, "quest-gather", func() bool {
		at := activeQuestIndex(c, job.quest)
		if c.DeletePending || at < 0 || !enterworld.CharacterAlive(c) {
			return false
		}
		if job.roll != gatherAlways {
			roll := rt.CaptureRoll
			if roll == nil {
				roll = combat.SecureRoll32767
			}
			value, err := roll()
			if err != nil || value > captureMaximumRoll {
				return false
			}
			draw := value % captureRandomDomain
			if job.roll == gatherIvyRoll && draw < captureBaseChance || job.roll == gatherAtMost && draw > uint32(job.chance) {
				frames = []wire.Frame{questNotification(job.failSymbol)}
				return false
			}
		}
		stepped := false
		if job.advanceStep {
			c.ActiveQuests[at].ToolStep++
			stepped = true
		}
		rows, updates, err := rt.PlanInventory(c, nil, []inventory.ItemAmount{{Codename: job.award, Count: 1}})
		if err != nil {
			if job.full != "" {
				frames = []wire.Frame{questNotification(job.full)}
			}
			return stepped
		}
		c.MissionInventory = rows
		objectives, _ := rt.applyInventoryChange(c)
		frames = append(updates, objectives...)
		return true
	})
	return frames
}
