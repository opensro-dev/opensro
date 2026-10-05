/*
===========================================================================

capture.go - how a fortress changes hands during its war

The fort stone is guarded twice. While any guard tower stands, the
fortress's +0x0C flag (CSiegeFortress_SetTowersStanding 62A740) keeps the
stone from being struck (0x3040). The last tower's fall clears it and
starts a three-minute countdown (CGameWorld_Siege_BeginEndCountdown
600EC0, 0xB4 s) during which the stone still refuses (0x3046). Breaking
the stone hands the fortress to the guild that dealt it the most damage
(CSiegeFortress_TopStoneDamageGuild 62A5B0) as its TempGuildID, shuts the
gates to attackers for five minutes (CGameWorld_Siege_ApplyCapture
601400) and reinstalls every structure for the new holder. The holder at
the war's end occupies the fortress (_SiegeFortressFinished).

===========================================================================
*/
package fortress

const (
	// CountdownMs is the stone's guard after the last tower falls: the
	// siege tick's slot 5, 0x2BF20 ms.
	CountdownMs = 180000
	// CaptureWaitMs keeps attackers out after a capture: slot 3, 0x493E0 ms.
	CaptureWaitMs = 300000

	// Fort stone refusals of CGObjPC_CanAttackTarget (52BF90).
	StoneRefusedTowersStand uint16 = 0x3040
	StoneRefusedCountdown   uint16 = 0x3046
)

/*
================
capture

The per-war state CSiegeFortress (+0x0C) and its siege world (+0x84,
+0x8C) keep: whether guard towers still stand, when the stone's
countdown ends and when the gates reopen.
================
*/
type capture struct {
	TowersStanding     bool
	CountdownUntilMs   int64
	EntryClosedUntilMs int64
}

/*
================
BeginWar

CSiegeFortress_BeginWar (61F130) for one fortress: the gates open, no
countdown runs, and the stone is guarded while any tower stands.
================
*/
func (a *Authority) BeginWar(divisionID string, fortressID uint32, towersStanding bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return
	}
	record.EntryOpen = true
	record.capture = capture{TowersStanding: towersStanding}
}

/*
================
TowersFallen

The last guard tower fell (CSiegeFortress_OnGuardTowersFallen 61F230):
the stone's tower guard ends and its countdown starts. Reports whether
the countdown started.
================
*/
func (a *Authority) TowersFallen(divisionID string, fortressID uint32, nowMs int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok || !record.TowersStanding {
		return false
	}
	record.TowersStanding = false
	record.CountdownUntilMs = nowMs + CountdownMs
	return true
}

/*
================
StoneRefusal

Why the fort stone may not be struck now, or 0.
================
*/
func (a *Authority) StoneRefusal(divisionID string, fortressID uint32, nowMs int64) uint16 {
	record, ok := a.Get(divisionID, fortressID)
	switch {
	case !ok:
		return 0
	case record.TowersStanding:
		return StoneRefusedTowersStand
	case nowMs < record.CountdownUntilMs:
		return StoneRefusedCountdown
	}
	return 0
}

/*
================
Capture

The fort stone broke: guildID holds the fortress until the war ends, and
attackers wait five minutes at the gates. Reports whether the holder
changed.
================
*/
func (a *Authority) Capture(divisionID string, fortressID uint32, guildID int64, nowMs int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok || guildID == 0 {
		return false
	}
	record.TempGuildID = guildID
	record.EntryOpen = false
	record.EntryClosedUntilMs = nowMs + CaptureWaitMs
	return true
}

/*
================
Holder

CSiegeFortress_GetHolderGuild (61D280): the temporary holder while one
exists, else the occupying guild.
================
*/
func (r Record) Holder() int64 {
	if r.TempGuildID != 0 {
		return r.TempGuildID
	}
	return r.GuildID
}

/*
================
Advance

The siege tick's timers for every fortress: the gates reopen once the
capture wait is over (slot 3).
================
*/
func (a *Authority) Advance(nowMs int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, state := range a.divisions {
		for _, record := range state.records {
			if !record.EntryOpen && record.EntryClosedUntilMs != 0 && nowMs >= record.EntryClosedUntilMs {
				record.EntryOpen = true
				record.EntryClosedUntilMs = 0
			}
		}
	}
}

/*
================
FinishWar

The war ended (_SiegeFortressFinished, result 0x1E): a temporary holder
occupies the fortress, and the war's capture state is cleared. Returns the
occupying guild and whether it changed.
================
*/
func (a *Authority) FinishWar(divisionID string, fortressID uint32) (int64, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return 0, false
	}
	changed := record.TempGuildID != 0 && record.TempGuildID != record.GuildID
	if record.TempGuildID != 0 {
		record.GuildID = record.TempGuildID
	}
	record.TempGuildID = 0
	record.EntryOpen = true
	record.capture = capture{}
	return record.GuildID, changed
}
