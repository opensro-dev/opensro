/*
===========================================================================

monsterstate_vitals.go - monster natural recovery and the vital latches

A living monster that stands (motion 0) or sits (motion 4) with its HP
latch up regains its reference's RecoveryHP (characterdata column 40)
every four seconds, up to its maximum. Applying a deferred vital
instruction (action's monsterdeferred.go) puts the latch down; it comes
back when the instruction's duration elapses. The native spawn resets
both latches up; a missing record reads exactly that.

The heal is state only: the original sends no per-tick frame for it, so
observers re-read the monster's HP from the next hit record.

===========================================================================
*/

package simulation

// monsterRecoveryIntervalMs is the native recovery timer's period,
// armed at spawn.
const monsterRecoveryIntervalMs int64 = 4000

// monsterVitalScanIntervalMs bounds how often a division's population
// is scanned for newly hurt monsters; recovery records are few, and the
// due pass below walks only them.
const monsterVitalScanIntervalMs int64 = 1000

/*
================
monsterVitalRecord

One hurt or latched monster's recovery state. Kept beside the division
state, not on the instance: cold (dormant) monsters must recover
without waking, and their archived row carries only HP scalars.
================
*/
type monsterVitalRecord struct {
	hpLatch, mpLatch bool
	hpRestoreMs      int64 // when the HP latch returns; 0 = not down
	mpRestoreMs      int64
	nextRecoveryMs   int64
}

/*
================
freshMonsterVitalRecord

The spawn/reset state: both latches up, first tick one period out.
================
*/
func freshMonsterVitalRecord(nowMs int64) *monsterVitalRecord {
	return &monsterVitalRecord{hpLatch: true, mpLatch: true, nextRecoveryMs: nowMs + monsterRecoveryIntervalMs}
}

/*
================
MonsterVitalLatches

The monster's vital latches; a monster with no record reads as freshly
spawned (both up).
================
*/
func (s *MonsterState) MonsterVitalLatches(division string, gid uint32) (hp, mp bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if state == nil {
		return true, true
	}
	if rec := state.vitals[gid]; rec != nil {
		return rec.hpLatch, rec.mpLatch
	}
	return true, true
}

/*
================
SetMonsterVitalLatches

Write the latches after a deferred application, with the instant each
down latch returns (the instruction's expiry). Creates the record on
first use, phased from now.
================
*/
func (s *MonsterState) SetMonsterVitalLatches(division string, gid uint32, hp, mp bool, hpRestoreMs, mpRestoreMs int64, nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if state == nil {
		return
	}
	if state.vitals == nil {
		state.vitals = make(map[uint32]*monsterVitalRecord)
	}
	rec := state.vitals[gid]
	if rec == nil {
		rec = freshMonsterVitalRecord(nowMs)
		state.vitals[gid] = rec
	}
	rec.hpLatch, rec.mpLatch = hp, mp
	rec.hpRestoreMs, rec.mpRestoreMs = hpRestoreMs, mpRestoreMs
}

/*
================
AdvanceMonsterVitals

The four-second recovery pass: restore elapsed latches, then heal the
due records under the native gates (alive, standing or sitting, latch
up, an authored amount, below maximum). A record whose monster is full
and unlatched, dead or gone is dropped; the hurt scan re-creates it if
the monster is hurt again.
================
*/
func (s *MonsterState) AdvanceMonsterVitals(nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range s.populationKeys() {
		state := s.populationForLease(key.division, key.lease)
		if state == nil {
			continue
		}
		s.discoverHurtMonsters(state, nowMs)
		for gid, rec := range state.vitals {
			s.advanceMonsterVital(state, key.division, gid, rec, nowMs)
		}
	}
}

/*
================
discoverHurtMonsters

Find hurt monsters with an authored recovery and give them a record,
throttled per division. Both residency tiers are read in place: a
dormant monster is discovered without waking it.
================
*/
func (s *MonsterState) discoverHurtMonsters(state *divisionMonsterState, nowMs int64) {
	if nowMs < state.nextVitalScanMs {
		return
	}
	state.nextVitalScanMs = nowMs + monsterVitalScanIntervalMs
	if state.vitals == nil {
		state.vitals = make(map[uint32]*monsterVitalRecord)
	}
	for gid, row := range state.instances.hotRows() {
		if row.currentHP == 0 || state.vitals[gid] != nil {
			continue
		}
		if row.ref.Value().RecoveryHP == 0 {
			continue
		}
		if instance := state.instances.hotValue(gid, row); instance.CurrentHP < instance.EffectiveMaxHP() {
			state.vitals[gid] = freshMonsterVitalRecord(nowMs)
		}
	}
	for gid, cold := range state.instances.cold {
		if cold.hp == 0 || cold.hp >= cold.maxHP || state.vitals[gid] != nil {
			continue
		}
		if cold.ref.Value().RecoveryHP == 0 {
			continue
		}
		state.vitals[gid] = freshMonsterVitalRecord(nowMs)
	}
}

/*
================
advanceMonsterVital

One record's pass: latch restores first, then the due heal. Returns
with the record dropped when nothing remains for it to do.
================
*/
func (s *MonsterState) advanceMonsterVital(state *divisionMonsterState, division string, gid uint32, rec *monsterVitalRecord, nowMs int64) {
	if rec.hpRestoreMs != 0 && nowMs >= rec.hpRestoreMs {
		rec.hpLatch, rec.hpRestoreMs = true, 0
	}
	if rec.mpRestoreMs != 0 && nowMs >= rec.mpRestoreMs {
		rec.mpLatch, rec.mpRestoreMs = true, 0
	}
	if nowMs >= rec.nextRecoveryMs {
		rec.nextRecoveryMs += monsterRecoveryIntervalMs
		s.recoverMonster(state, division, gid, rec, nowMs)
	}
	if rec.hpLatch && rec.mpLatch && rec.hpRestoreMs == 0 && rec.mpRestoreMs == 0 {
		if hp, maxHP, ok := s.monsterVitalHP(state, gid); ok && hp >= maxHP {
			delete(state.vitals, gid)
		}
	}
}

/*
================
monsterVitalHP

Current and maximum HP in either residency tier, without waking.
================
*/
func (s *MonsterState) monsterVitalHP(state *divisionMonsterState, gid uint32) (uint32, uint32, bool) {
	if row, ok := state.instances.hotRow(gid); ok {
		instance := state.instances.hotValue(gid, row)
		return instance.CurrentHP, instance.EffectiveMaxHP(), true
	}
	if cold, ok := state.instances.cold[gid]; ok {
		return cold.hp, cold.maxHP, true
	}
	return 0, 0, false
}

/*
================
recoverMonster

One due heal under the gates. The hot tier republishes through the
storage's set and notes the change for the tick's 0x33A6 with the
natural-recovery cause: the reference server's setter ORs the cause 0x10
its recovery tick passes into the vitals word the periodic sync sends,
so a holder's HP bar rises with each heal. The cold tier's archived HP
is written in place so the monster stays dormant; nobody holds it.
================
*/
func (s *MonsterState) recoverMonster(state *divisionMonsterState, division string, gid uint32, rec *monsterVitalRecord, nowMs int64) {
	if !rec.hpLatch {
		return
	}
	if row, ok := state.instances.hotRow(gid); ok {
		instance := state.instances.hotValue(gid, row)
		amount := instance.Ref.RecoveryHP
		maxHP := instance.EffectiveMaxHP()
		if instance.CurrentHP == 0 || amount == 0 || instance.CurrentHP >= maxHP {
			return
		}
		if motion := instance.Motion.StateAt(nowMs); motion != 0 && motion != 4 {
			return
		}
		instance.CurrentHP += min(amount, maxHP-instance.CurrentHP)
		state.instances.set(gid, instance)
		s.noteVitalsLocked(division, gid, VitalsSourceNaturalRecovery)
		return
	}
	if cold, ok := state.instances.cold[gid]; ok {
		amount := cold.ref.Value().RecoveryHP
		if cold.hp == 0 || amount == 0 || cold.hp >= cold.maxHP {
			return
		}
		cold.hp += min(amount, cold.maxHP-cold.hp)
		state.instances.cold[gid] = cold
	}
}
