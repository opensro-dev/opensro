/*
===========================================================================

capture.go - how a fortress changes hands during its war

The fort stone is guarded twice. While any guard tower stands, the
fortress's +0x0C flag (the towers-standing flag) keeps the stone from
being struck (0x3040). The last tower's fall clears it and starts a
three-minute countdown (the siege world's end countdown, 0xB4 s) during
which the stone still refuses (0x3046). Breaking the stone hands the
fortress to the guild that dealt it the most damage (the stone's
top-damage guild) as its TempGuildID, shuts the gates to attackers for
five minutes and reinstalls every structure for the new holder. The
holder at the war's end occupies the fortress (_SiegeFortressFinished).

===========================================================================
*/
package fortress

const (
	// CountdownMs is the stone's guard after the last tower falls: the
	// siege tick's slot 5, 0x2BF20 ms.
	CountdownMs = 180000
	// CaptureWaitMs keeps attackers out after a capture: slot 3, 0x493E0 ms.
	CaptureWaitMs = 300000

	// Fort stone refusals of the native can-attack-target check.
	StoneRefusedTowersStand uint16 = 0x3040
	StoneRefusedCountdown   uint16 = 0x3046
)

/*
================
capture

The reference server's per-war fortress state (+0x0C) and its siege world
(+0x84, +0x8C) keep: whether guard towers still stand, when the stone's
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

The native war start for one fortress: the gates open, no countdown runs,
and the stone is guarded while any tower stands.
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

The last guard tower fell (the guard-towers-fallen transition): the
stone's tower guard ends and its countdown starts. Reports whether
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
	record.StaffFlags = 0 // The successful temporary capture clears employment.
	record.EntryOpen = false
	record.EntryClosedUntilMs = nowMs + CaptureWaitMs
	_ = a.saveRecordLocked(divisionID, record)
	return true
}

/*
================
Holder

The native holder query: the temporary holder while one exists, else the
occupying guild.
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
occupies the fortress, and the war's capture state and requests are
cleared. Returns the occupying guild and whether it changed.

INFERENCE: requests belong to one war, so they end with it; the next
request period takes new applications.
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
		record.StaffFlags = 0 // The war-end settle clears flags even if the former holder recaptures.
		record.GuildID = record.TempGuildID
	}
	record.TempGuildID = 0
	record.EntryOpen = true
	record.capture = capture{}
	_ = a.saveRecordLocked(divisionID, record)
	for guild, kind := range record.Applicants {
		_ = a.saveRequestLocked(divisionID, fortressID, guild, kind, false)
	}
	record.Applicants = nil
	return record.GuildID, changed
}
