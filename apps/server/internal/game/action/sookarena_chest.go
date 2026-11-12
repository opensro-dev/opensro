/*
===========================================================================
sookarena_chest.go - port-only checkpoint rewards and exactly-once bag delivery.

The character transaction owns the chest and its claim. Ground loot is never
used as temporary storage; full bags leave a durable entitlement at So-ok.
===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/sookarena"
	"opensro.online/server/internal/game/tunables"
)

const opArenaChest uint16 = 0x3fca
const opArenaDecision uint16 = 0x7fca
const arenaChestLimit = 200

/*
================
PrepareSookShutdown - distinguish a server drain from a player's combat forfeit.
================
*/
func (rt *Runtime) PrepareSookShutdown() {
	if rt.sookArena != nil {
		rt.sookArena.draining.Store(true)
	}
}

/*
================
arenaProjection - server-owned values; item bodies reuse commerce previews.
================
*/
type arenaProjection struct {
	Version        int                             `json:"version"`
	ID             string                          `json:"id"`
	Stage          int                             `json:"stage"`
	Kills          int                             `json:"kills"`
	Phase          string                          `json:"phase"`
	RemainingMs    int64                           `json:"remainingMs"`
	DecisionMs     int64                           `json:"decisionMs"`
	Gold           string                          `json:"gold"`
	Items          []shopInventoryRow              `json:"items"`
	Error          string                          `json:"error,omitempty"`
	SkillCooldowns []enterworld.EntrySkillCooldown `json:"skillCooldowns"`
}

/*
================
arenaChestFrame
================
*/
func (rt *Runtime) arenaChestFrame(c *enterworld.Character, run *sookArenaRun, fault string) wire.Frame {
	p := arenaProjection{Version: 1, Phase: "closed", Items: []shopInventoryRow{}, Gold: "0", Error: fault}
	if chest := c.SookChest; chest != nil && rt.sookArena != nil {
		p.ID, p.Stage, p.Kills = chest.ID, chest.Stage, chest.Kills
		p.RemainingMs = max(0, sookarena.DurationMs-chest.ElapsedMs)
		p.Gold = strconv.FormatUint(sookarena.KillReward(chest.Kills), 10)
		if chest.Pending {
			p.Phase = "claimable"
		} else if chest.Checkpoint {
			p.Phase = "checkpoint"
			if run == nil {
				p.Phase = "recoverable"
			}
		}
		items := invItemsFromRowsWithin(chest.Items, arenaChestLimit)
		rows, err := rt.shopInventoryRows(items, nil)
		if err != nil {
			p.Error = "Reward preview is unavailable. Your chest remains saved."
		} else {
			p.Items = rows
		}
		if run != nil && run.program.PausedAt != 0 {
			if run.program.ResumeAt != 0 {
				p.Phase = "countdown"
				p.DecisionMs = max(0, run.program.ResumeAt-rt.Now().UnixMilli())
			} else {
				p.DecisionMs = max(0, run.program.DecisionUntil-rt.Now().UnixMilli())
			}
		}
	}
	if run != nil {
		now := rt.Now().UnixMilli()
		if run.program.PausedAt != 0 && run.program.ResumeAt == 0 {
			now = run.program.PausedAt
		}
		p.SkillCooldowns = enterworld.RemainingSkillCooldowns(rt.deps.SkillData(), c, now)
		if p.SkillCooldowns == nil {
			p.SkillCooldowns = []enterworld.EntrySkillCooldown{}
		}
	}
	// This projection contains only bounded scalar values and item bodies.
	payload, _ := json.Marshal(p)
	return wire.Frame{Opcode: opArenaChest, Payload: payload}
}

/*
================
arenaChestFrames - preview references must arrive before unfamiliar items.
================
*/
func (rt *Runtime) arenaChestFrames(c *enterworld.Character, run *sookArenaRun, fault string) []wire.Frame {
	frames := []wire.Frame{}
	if c.SookChest != nil && len(c.SookChest.Items) > 0 {
		frames = append(frames, rt.commerceReferences(invItemsFromRowsWithin(c.SookChest.Items, arenaChestLimit), nil))
	}
	return append(frames, rt.arenaChestFrame(c, run, fault))
}

/*
================
rollSookChest - existing degree, plus, blue and seal rules; bounded stage bundles.
================
*/
func (rt *Runtime) rollSookChest(run *sookArenaRun, c *enterworld.Character, stage int) []domain.InventoryRow {
	ranges := [5][2]int{{3, 7}, {8, 15}, {18, 28}, {32, 45}, {50, 70}}
	if stage < 1 || stage > len(ranges) {
		return nil
	}
	band := ranges[stage-1]
	count := band[0] + rt.arenaDraw(band[1]-band[0]+1)
	room := int(min(tunables.Current().Int(tunables.SookChestCapacity), arenaChestLimit))
	items := []inventory.Item{}
	for attempt := 0; len(items) < min(count, room) && attempt < count*16; attempt++ {
		item, ok := rt.arenaReward(run, c, stage, attempt)
		if ok {
			item.Slot = uint8(len(items))
			items = append(items, item)
		}
	}
	return rowsFromInvItems(items)
}

/*
================
saveSookCheckpoint - stage identity guards repeated ticks and decisions.
================
*/
func (rt *Runtime) saveSookCheckpoint(run *sookArenaRun, c *enterworld.Character, stage int) bool {
	if c.SookChest == nil {
		return false
	}
	if c.SookChest.Stage >= stage {
		return true
	}
	rows := rt.rollSookChest(run, c, stage)
	return rt.deps.Update(c, "sook-chest-stage", func() bool {
		chest := c.SookChest
		if chest == nil || chest.Stage >= stage {
			return false
		}
		for i := range rows {
			rows[i].Slot = int64(i)
		}
		chest.Items = rows
		chest.Stage, chest.Kills, chest.ElapsedMs = stage, run.program.Kills, rt.Now().UnixMilli()-run.program.Started
		chest.Checkpoint = true
		return true
	})
}

/*
================
claimSookChest - all bag grants and clearing the entitlement share one commit.
================
*/
func (rt *Runtime) claimSookChest(c *enterworld.Character) ([]wire.Frame, string) {
	if c.SookChest == nil || !c.SookChest.Pending {
		return nil, "No rewards are waiting."
	}
	items := invItemsFromRowsWithin(c.SookChest.Items, arenaChestLimit)
	rows, frames, err := rt.planBagChange(c, func(inv *inventory.Inventory) error {
		for _, item := range items {
			ref, ok := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
			if !ok || ref == nil {
				return fmt.Errorf("missing reward reference")
			}
			grant, fault := inv.GrantStack(item, rt.maxStackFor(item.TypeFlags, ref.Codename))
			if fault != nil || grant.GroundRemainder != 0 {
				return fmt.Errorf("bag full")
			}
		}
		return nil
	})
	if err != nil {
		return nil, "Make space in your bag, then claim your saved chest from Event So-ok."
	}
	id := c.SookChest.ID
	if !rt.deps.Update(c, "sook-chest-claim", func() bool {
		if c.SookChest == nil || c.SookChest.ID != id || !c.SookChest.Pending {
			return false
		}
		c.MissionInventory, c.SookChest = rows, nil
		return true
	}) {
		return nil, "Rewards could not be saved. Your chest remains waiting."
	}
	return frames, ""
}

/*
================
HandleArenaDecision - authenticated owner only; a stale run cannot claim another.
================
*/
func (rt *Runtime) HandleArenaDecision(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) < 1 || len(payload) > 129 || payload[0] > 2 || payload[0] == 0 && len(payload) != 1 {
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	if rt.sookArena == nil {
		if payload[0] == 0 {
			return OpResult{Frames: []wire.Frame{rt.arenaChestFrame(c, nil, "")}}
		}
		return failureResult(wire.ErrCodeInvalidRequest)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c = rt.findCharacter(division, c.Name)
	if c == nil {
		return OpResult{}
	}
	run := rt.sookRun(division, c)
	if payload[0] == 0 {
		return OpResult{Frames: rt.arenaChestFrames(c, run, "")}
	}
	if c.SookChest == nil || c.SookChest.ID != string(payload[1:]) {
		return OpResult{Frames: rt.arenaChestFrames(c, run, "This reward decision is no longer current.")}
	}
	if !enterworld.CharacterAlive(c) {
		return OpResult{}
	}
	if run != nil {
		if run.program.PausedAt == 0 || run.program.ResumeAt != 0 {
			return OpResult{}
		}
		if payload[0] == 2 {
			if c.SookChest.Stage >= sookarena.StageCount {
				return OpResult{}
			}
			now := rt.Now().UnixMilli()
			if !rt.deps.Update(c, "sook-countdown-buffs", func() bool {
				resumeSookSkills(c, run.pause, run.program.PausedAt, now-run.program.PausedAt)
				return true
			}) {
				return OpResult{Frames: rt.arenaChestFrames(c, run, "Please try again. Your rewards are saved.")}
			}
			run.pause = nil
			run.program.ResumeAt = now + sookarena.ReadyMs
			return OpResult{Frames: rt.arenaChestFrames(c, run, "")}
		}
		rt.endSookArena(run, c, true)
		return OpResult{}
	}
	selected, ok := rt.Selected.Get(division, c.Name)
	npc, live := rt.npcForCurrentViewer(division, c, selected)
	if !ok || !live || npc.Codename != sookarena.NPC || !rt.npcWithinHitRange(division, c, npc) {
		return OpResult{Frames: rt.arenaChestFrames(c, nil, "Visit Event So-ok to collect or resume your chest.")}
	}
	if payload[0] == 2 && c.SookChest.Checkpoint && !c.SookChest.Pending {
		if rt.sookArena == nil {
			return OpResult{Frames: rt.arenaChestFrames(c, nil, "Arena entry is disabled. Your saved rewards can still be claimed.")}
		}
		return OpResult{Frames: rt.enterSookArenaInsideDoor(division, c)}
	}
	if c.SookChest.Checkpoint || c.SookChest.Pending {
		if !rt.deps.Update(c, "sook-recovered-payout", func() bool {
			if !c.SookChest.GoldPaid {
				reward := sookarena.KillReward(c.SookChest.Kills)
				if goldOf(c) > uint64(^uint64(0)>>1)-reward {
					return false
				}
				setGold(c, goldOf(c)+reward)
				c.SookChest.GoldPaid = true
			}
			c.SookChest.Pending, c.SookChest.Checkpoint = true, false
			return true
		}) {
			return OpResult{}
		}
	}
	frames, fault := rt.claimSookChest(c)
	frames = append(frames, wire.Frame{Opcode: wire.OpPointsUpdate, Payload: wire.GoldRefresh{Balance: goldOf(c), Notify: true}.Encode()})
	return OpResult{Frames: append(frames, rt.arenaChestFrames(c, nil, fault)...)}
}

/*
================
sookPause - preserve skill reuse while a checkpoint dialog owns input.
================
*/
type sookPause struct {
	pets      map[uint32][2]domain.PetRowCooldown
	passives  map[uint32][3]int64
	offensive map[uint32]int64
	shared    map[uint8]int64
	action    int64
}

/*
================
pauseSookSkills
================
*/
func pauseSookSkills(c *enterworld.Character) *sookPause {
	p := &sookPause{offensive: maps.Clone(c.OffensiveSkillCooldowns), shared: maps.Clone(c.SharedSkillCooldowns), action: c.SkillActionRecoveryUntilMs, pets: map[uint32][2]domain.PetRowCooldown{}, passives: map[uint32][3]int64{}}
	for _, pet := range c.Companions() {
		p.pets[pet.GID] = pet.Training.Rows
		p.passives[pet.GID] = pet.Training.PassiveReadyAtMs
	}
	return p
}

/*
================
resumeSookSkills
================
*/
func resumeSookSkills(c *enterworld.Character, p *sookPause, since, delta int64) {
	if p == nil {
		return
	}
	offensive, shared := maps.Clone(p.offensive), maps.Clone(p.shared)
	for id, at := range offensive {
		if at > since {
			offensive[id] = at + delta
		}
	}
	for id, at := range shared {
		if at > since {
			shared[id] = at + delta
		}
	}
	c.OffensiveSkillCooldowns, c.SharedSkillCooldowns = offensive, shared
	for _, pet := range c.Companions() {
		if rows, ok := p.pets[pet.GID]; ok {
			for i := range rows {
				if rows[i].ReadyAtMs > since {
					rows[i].ReadyAtMs += delta
				}
			}
			pet.Training.Rows = rows
			deadlines := p.passives[pet.GID]
			for i, at := range deadlines {
				if at > since {
					deadlines[i] = at + delta
				}
			}
			pet.Training.PassiveReadyAtMs = deadlines
		}
	}
	if p.action > since {
		c.SkillActionRecoveryUntilMs = p.action + delta
	}
}

/*
================
shiftSookAbnormals - preserve negative-state time, without cleansing or damage.
================
*/
func (rt *Runtime) shiftSookAbnormals(division string, c *enterworld.Character, delta int64) {
	if block := shiftSookBlock(rt.playerAbnormal(division, c.Name), delta); block != nil {
		rt.storePlayerAbnormal(division, c.Name, block)
	}
	for _, pet := range c.Companions() {
		if block := shiftSookBlock(rt.cosAbnormal(division, c.Name, pet.GID), delta); block != nil {
			rt.storeCosAbnormal(division, c.Name, pet.GID, block)
		}
	}
}

/*
================
shiftSookBlock
================
*/
func shiftSookBlock(block *abnormal.Block, delta int64) *abnormal.Block {
	if block == nil {
		return nil
	}
	copy := *block
	for i := range copy.Slots {
		slot := &copy.Slots[i]
		if slot.Active {
			slot.StartedAt += delta
			if slot.LastTickAt != 0 {
				slot.LastTickAt += delta
			}
		}
	}
	return &copy
}
