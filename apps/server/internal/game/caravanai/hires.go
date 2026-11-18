/*
===========================================================================

hires.go - hires survive a restart (port-only)

The flush loop mirrors every hire in flight to the store; the roster's boot
puts each one back on its agent. The agent waits in the hire town for the
hirer to reconnect (HireRestoreGraceMs), then teleports to them and rejoins
their party. A hire that cannot resume pays the goods back like !dismiss.

===========================================================================
*/
package caravanai

import (
	"errors"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// HireStore is the optional store port for hires in flight; a MemoryStore
// that also implements it keeps them.
type HireStore interface {
	LoadCaravanHires(division string) ([]domain.CaravanHireRow, error)
	SaveCaravanHire(row domain.CaravanHireRow) error
	DeleteCaravanHire(division, agentName string) error
}

// HireRestoreGraceMs is how long a restored hire waits for its hirer to log
// back in before it ends.
const HireRestoreGraceMs int64 = int64(10 * time.Minute / time.Millisecond)

func (r *Runtime) loadHires(division string) []domain.CaravanHireRow {
	if r.hireStore == nil {
		return nil
	}
	rows, err := r.hireStore.LoadCaravanHires(division)
	if err != nil {
		log.WithError(err).Warn("caravan-ai: hires not restored")
	}
	return rows
}

func hireRow(u *AgentUnit) domain.CaravanHireRow {
	return domain.CaravanHireRow{
		Division: u.Division, AgentName: u.Name, PlayerName: u.HiredBy, HiredAtMs: u.HiredAtMs,
		HireTown: string(u.HireTown), GoodsCostGold: u.GoodsCostGold, CargoStacks: u.CargoStacks,
	}
}

/*
================
restoreHiresLocked

Puts the stored hires back on a freshly spawned roster. A row whose agent
is gone or busy pays its hirer the goods back; either way the next flush
learns every stored row (restoredHires) and deletes the ones not resumed.
================
*/
func (r *Runtime) restoreHiresLocked(division string, rows []domain.CaravanHireRow, nowMs int64) {
	r.restoredHires = append(r.restoredHires, rows...)
	for _, row := range rows {
		unit := r.findAgentLocked(division, row.AgentName)
		if unit == nil || !hireable(unit) {
			gold := row.GoodsCostGold * int64(max(row.CargoStacks, 0)) / 10
			r.outbox = append(r.outbox, outboxItem{player: row.PlayerName, agent: row.AgentName, gold: gold, end: HireAborted})
			continue
		}
		town := TownID(row.HireTown)
		bindHire(unit, row.PlayerName, town, row.GoodsCostGold, row.HiredAtMs)
		if unit.Role == RoleTrader {
			stars := uint8(min(max(row.GoodsCostGold/HiredGoodsPerStarGold, 1), int64(MaxHireStars)))
			unit.CargoStars, unit.CargoStacks, unit.StackValueGold = stars, row.CargoStacks, row.GoodsCostGold/10
		}
		unit.place(r.terminalLocked(town), nowMs)
		unit.leaderMissingAtMs = nowMs + HireRestoreGraceMs - HireLeaderMissingMs
		unit.rejoinParty = true
	}
}

/*
================
flushHires

Writes the hires that changed since the last flush and deletes the ended
ones. Off the tick (Run, Close): store writes take the authority's lock.
================
*/
func (r *Runtime) flushHires() error {
	if r == nil || r.hireStore == nil {
		return nil
	}
	r.hireSaveMu.Lock()
	defer r.hireSaveMu.Unlock()
	r.mu.Lock()
	for _, row := range r.restoredHires {
		r.savedHires[agentKey(row.Division, row.AgentName)] = row
	}
	r.restoredHires = nil
	live := make(map[string]domain.CaravanHireRow)
	for division, units := range r.agents {
		for _, u := range units {
			if u.HiredBy != "" {
				row := hireRow(u)
				row.Division = division
				live[agentKey(division, u.Name)] = row
			}
		}
	}
	r.mu.Unlock()

	var errs []error
	for key, row := range live {
		if saved, ok := r.savedHires[key]; ok && saved == row {
			continue
		}
		if err := r.hireStore.SaveCaravanHire(row); err != nil {
			errs = append(errs, err)
			continue
		}
		r.savedHires[key] = row
	}
	for key, row := range r.savedHires {
		if _, ok := live[key]; ok {
			continue
		}
		if err := r.hireStore.DeleteCaravanHire(row.Division, row.AgentName); err != nil {
			errs = append(errs, err)
			continue
		}
		delete(r.savedHires, key)
	}
	return errors.Join(errs...)
}
