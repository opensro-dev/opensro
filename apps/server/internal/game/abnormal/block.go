/*
===========================================================================

block.go - the 32-slot abnormal-state owner block: apply, cure, expire

===========================================================================
*/

package abnormal

// Slot is one 0x70-byte slot of the owner block (block+0C, stride 0x70):
// the admitted record (+00..+5F) and its runtime state.
/*
================
Slot
================
*/
type Slot struct {
	Record
	Active     bool  // +60
	Retired    bool  // +61: cleared by cure/death rather than natural expiry
	Refreshed  bool  // +62: a stronger record replaced an active one
	StartedAt  int64 // +64 GetTickCount at admission (ms)
	LastTickAt int64 // +68 zero until the first periodic tick
	SourceDied bool  // +6C dead-source latch of damage-over-time
	// Drain is +30: the per-tick resource drain panic/combustion computes
	// from the owner's maximum at installation.
	Drain uint16
}

/*
==================
Modifier

Modifier is one parameter-keeper write owned by the abnormal block.
Channel follows paramkeeper (0 flat, 1 percent sum, 2 percent product,
3 factor product); Source is the native source key (5 for abnormal state).
==================
*/
type Modifier struct {
	Used    bool
	Param   uint16
	Channel uint8
	Source  uint32
	Value   float32
}

// MaxModifiers bounds the fixed modifier table. Every status installs at
// most three writes; all 23 statuses fit well below this bound.
const MaxModifiers = 48

// Block is tagAbnormalStateBlock. It is a plain value: owners store it in
// their snapshot and copy it without aliasing.
/*
================
Block
================
*/
type Block struct {
	Slots [SlotCount]Slot
	Mask  uint32 // +04
	// SpeedOwner is +08: 1 while frostbite owns the movement factors, 8
	// while slow does (4A45D0 / 4A4A90).
	SpeedOwner uint8
	Modifiers  [MaxModifiers]Modifier
}

// Active reports whether any slot is active.
/*
================
Active
================
*/
func (b *Block) Active() bool {
	for i := range b.Slots {
		if b.Slots[i].Active {
			return true
		}
	}
	return false
}

// Has reports an active slot for one status.
/*
================
Has
================
*/
func (b *Block) Has(s Status) bool { return b.Slots[s].Active }

/*
================
applyModifier
================
*/
func (b *Block) applyModifier(param uint16, channel uint8, source uint32, value float32) {
	free := -1
	for i := range b.Modifiers {
		m := &b.Modifiers[i]
		if m.Used && m.Param == param && m.Channel == channel && m.Source == source {
			m.Value = value
			return
		}
		if !m.Used && free < 0 {
			free = i
		}
	}
	if free < 0 {
		panic("abnormal: modifier table exhausted")
	}
	b.Modifiers[free] = Modifier{Used: true, Param: param, Channel: channel, Source: source, Value: value}
}

// removeModifier removes every channel entry of one source (4B31A0).
/*
================
removeModifier
================
*/
func (b *Block) removeModifier(param uint16, source uint32) bool {
	removed := false
	for i := range b.Modifiers {
		m := &b.Modifiers[i]
		if m.Used && m.Param == param && m.Source == source {
			*m = Modifier{}
			removed = true
		}
	}
	return removed
}

// ModifiersFor returns the block's writes to one parameter in table order.
/*
================
ModifiersFor
================
*/
func (b *Block) ModifiersFor(param uint16) []Modifier {
	var out []Modifier
	for _, m := range b.Modifiers {
		if m.Used && m.Param == param {
			out = append(out, m)
		}
	}
	return out
}

/*
==================
Apply

Apply ports 4A4270. The owner must be alive and the record's source must
still resolve; an active slot is replaced only by a strictly stronger
level (+1A) or, when the level is zero, grade (+18). It returns whether the
record took hold; the start callback has then already run.
==================
*/
func (b *Block) Apply(o Owner, r Record, now int64) bool {
	if !o.Alive() || int(r.Status) >= SlotCount {
		return false
	}
	slot := &b.Slots[r.Status]
	if r.Level != 0 {
		if slot.Active && r.Level <= slot.Level {
			return false
		}
	} else {
		if r.Grade == 0 || slot.Active && r.Grade <= slot.Grade {
			return false
		}
	}
	if slot.Active {
		slot.Refreshed = true // 4A42D9, set before the source lookup
	}
	if !o.SourceExists(r.SourceGID) {
		return false
	}
	// 4A433A copies 0x60 record bytes; +62 and the +6C latch survive.
	slot.Record, slot.Drain = r, 0
	slot.Active, slot.Retired, slot.StartedAt, slot.LastTickAt = true, false, now, 0
	callback(b, o, 0, slot)
	b.Mask |= r.Status.Bit()
	return true
}

// Result reports what one update changed for the caller's publication.
/*
================
Result
================
*/
type Result struct {
	Changed bool   // publish the mask (4A4540)
	Mask    uint32 // the recomputed mask
}

/*
==================
Update

Update ports 4A4390: retire expired or cured slots (callback 2), tick the
others (callback 1) and recompute the mask. When the time bomb expires it
also wakes sleep and root, and breaks stun with a 25 % roll.
==================
*/
func (b *Block) Update(o Owner, now int64) Result {
	if b.Mask == 0 {
		return Result{}
	}
	var mask uint32
	flagged := false
	for i := 0; i < SlotCount; i++ {
		slot := &b.Slots[i]
		if !slot.Active {
			continue
		}
		status := Status(i)
		if b.Mask&status.Bit() == 0 || elapsed(now, slot.StartedAt) > slot.DurationMs {
			slot.SourceDied, slot.Active, slot.Retired = false, false, false
			slot.Refreshed = false
			callback(b, o, 2, slot)
			if status == TimeBomb {
				if stun := &b.Slots[Stun]; stun.Active && o.Roll(0x10000000, 25) {
					stun.SourceDied, stun.Active, stun.Retired, stun.Refreshed = false, false, false, false
					callback(b, o, 2, stun)
					mask &^= Stun.Bit()
				}
				for _, s := range []Status{Root, Sleep} {
					if other := &b.Slots[s]; other.Active {
						other.SourceDied, other.Active, other.Retired, other.Refreshed = false, false, false, false
						callback(b, o, 2, other)
						mask &^= s.Bit()
					}
				}
			}
			continue
		}
		mask |= status.Bit()
		if slot.Refreshed {
			flagged = true
			slot.Refreshed = false
		}
		callback(b, o, 1, slot)
	}
	if !flagged && b.Mask == mask {
		return Result{}
	}
	b.Mask = mask
	return Result{Changed: true, Mask: mask}
}

// SkillLevelCure is RefSkill+0x40C (curt): one mask and one shared level.
/*
================
SkillLevelCure
================
*/
type SkillLevelCure struct {
	Mask  uint32
	Level uint16
}

/*
==================
Cure

Cure ports 4A56C0. levels is the six words from 49AC50 (statuses 0..5);
a nil levels pointer skips that arm. skill is arg3 (curt). mask, when
non-nil, is the pill (curl): bit mask, chance numerator, grade base.
limit < 0 is a null limit pointer (every selected slot). limit == 0 is a
present zero cap and treats nothing. A positive limit shuffles by 4A6560
(MSVC random_shuffle, rand() % (i+1)) and keeps that many. A pill bit is
taken before an arg3 bit. Arg3 shortens +64 by durationPerLevel and clears
the slot when elapsed time passes the duration; a zero cut skips the slot.
==================
*/
func (b *Block) Cure(o Owner, levels *[6]int32, skill *SkillLevelCure, mask *[3]int32, limit int, rand func() int32) bool {
	if b == nil {
		return false
	}
	type pick struct {
		status Status
	}
	var picks []pick
	for i := Status(0); i < SlotCount; i++ {
		slot := &b.Slots[i]
		if !slot.Active {
			continue
		}
		bit := i.Bit()
		if mask != nil && mask[0]&int32(bit) != 0 {
			picks = append(picks, pick{i})
			continue
		}
		if skill != nil && skill.Mask&bit != 0 {
			picks = append(picks, pick{i})
			continue
		}
		if levels != nil && i <= Zombie {
			picks = append(picks, pick{i})
		}
	}
	if limit == 0 {
		return false
	}
	if limit > 0 {
		// 4A6560: for i >= 1, swap i with rand() % (i+1). Every bound here is
		// at most 32, below RAND_MAX, so the extended-rand branch never runs.
		for i := 1; i < len(picks); i++ {
			j := int(uint32(rand()&0x7fff) % uint32(i+1))
			picks[i], picks[j] = picks[j], picks[i]
		}
		if len(picks) > limit {
			picks = picks[:limit]
		}
	}
	changed := false
	for _, picked := range picks {
		slot := &b.Slots[picked.status]
		if mask != nil && mask[0]&int32(picked.status.Bit()) != 0 {
			grade := int32(slot.Grade) - mask[2]
			chance := mask[1]
			if grade > 0 && chance != 0 {
				chance = chance / grade
			}
			if o != nil && chance != 0 && o.Roll(0, chance) {
				changed = b.Clear(o, picked.status) || changed
			}
			continue
		}
		if skill != nil && skill.Mask&picked.status.Bit() != 0 {
			cut := int64(durationPerLevel(picked.status, skill.Level))
			if cut == 0 {
				continue
			}
			slot.StartedAt -= cut
			if o != nil && elapsed(o.Now(), slot.StartedAt) > slot.DurationMs {
				changed = b.Clear(o, picked.status) || changed
			} else {
				changed = true
			}
			continue
		}
		if levels == nil || picked.status > Zombie {
			continue
		}
		// 4A5857: a zero word skips the slot; any other word is taken as u16.
		level := levels[picked.status]
		if level == 0 {
			continue
		}
		cut := int64(durationPerLevel(picked.status, uint16(level)))
		if cut == 0 {
			continue
		}
		slot.StartedAt -= cut
		if o != nil && elapsed(o.Now(), slot.StartedAt) > slot.DurationMs {
			changed = b.Clear(o, picked.status) || changed
		} else {
			changed = true
		}
	}
	return changed
}

// Clear ports 4A5660: retire one slot as a cure/break (+61 = 1).
/*
================
Clear
================
*/
func (b *Block) Clear(o Owner, s Status) bool {
	slot := &b.Slots[s]
	if !slot.Active {
		return false
	}
	slot.Refreshed, slot.Active, slot.Retired, slot.SourceDied = false, false, true, false
	callback(b, o, 2, slot)
	var mask uint32
	for i := range b.Slots {
		if b.Slots[i].Active {
			mask |= Status(i).Bit()
		}
	}
	b.Mask = mask
	return true
}

// ClearAll ports 4A59F0 (death and town teleport).
/*
================
ClearAll
================
*/
func (b *Block) ClearAll(o Owner) bool {
	changed := false
	for i := range b.Slots {
		slot := &b.Slots[i]
		if !slot.Active {
			continue
		}
		slot.Refreshed, slot.Active, slot.Retired, slot.SourceDied = false, false, true, false
		callback(b, o, 2, slot)
		changed = true
	}
	changed = changed || b.Mask != 0
	b.Mask = 0
	return changed
}

/*
================
HitContext

The magical lane releases Root at 58F491. The execution selector admits
Sleep/Stun retirement at 5939D8, independently of shield-blocked damage.
================
*/
type HitContext struct {
	Magical bool
	Attack  bool
}

/*
==================
BreakOnHit

BreakOnHit ports the separate consequences of 58F491 and 593BEF: magical
damage frees root; an attack result wakes sleep (motion reset) and breaks
stun with a 25 % roll, unless a time bomb is attached. It runs before the
hit's own statuses are applied.
==================
*/
func (b *Block) BreakOnHit(o Owner, hit HitContext) bool {
	changed := false
	if hit.Magical && b.Mask&Root.Bit() != 0 {
		changed = b.Clear(o, Root) || changed
	}
	if hit.Attack && b.Mask&TimeBomb.Bit() == 0 {
		if b.Mask&Sleep.Bit() != 0 {
			changed = b.Clear(o, Sleep) || changed
			o.SetMotion(0, 0xff, 0)
		}
		if b.Mask&Stun.Bit() != 0 && o.Roll(0x10000000, 25) {
			changed = b.Clear(o, Stun) || changed
		}
	}
	return changed
}

// Grades returns the vitals grade bytes following the mask, ascending bit
// order over GradeMask (client 77A080).
/*
================
Grades
================
*/
func (b *Block) Grades() []uint8 {
	var out []uint8
	for bit := 0; bit < 32; bit++ {
		value := uint32(1) << bit
		if b.Mask&value == 0 || GradeMask&value == 0 {
			continue
		}
		for i := range b.Slots {
			if b.Slots[i].Active && Status(i).Bit() == value {
				out = append(out, b.Slots[i].Grade)
				break
			}
		}
	}
	return out
}

/*
================
elapsed

The native unsigned GetTickCount difference. Native reads one clock, so a
slot never starts after the instant that updates it. The port samples two:
a release inside the world tick admits with the monster registry's clock,
then the same tick updates with its earlier sampled instant. Without the
clamp that negative age wraps to ~49 days and expires the slot at once.
================
*/
func elapsed(now, since int64) uint32 {
	if now < since {
		return 0
	}
	return uint32(now - since)
}

// ForgetSource detaches a caster identity without curing the victim: a
// disconnected source no longer resolves through ObjMgr (its GID is gone).
/*
================
ForgetSource
================
*/
func (b *Block) ForgetSource(gid uint32, name string) bool {
	changed := false
	for i := range b.Slots {
		slot := &b.Slots[i]
		if slot.Active && (gid != 0 && slot.SourceGID == gid || name != "" && equalFold(slot.SourceName, name)) {
			slot.SourceGID, slot.SourceName = 0, ""
			changed = true
		}
	}
	return changed
}

/*
================
equalFold
================
*/
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
