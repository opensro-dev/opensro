/*
===========================================================================

forge.go - the fortress smith's and trainer's production orders

A fortress holds at most one order per staff member. Which member an item
belongs to is the caller's classification (61DAC0 scans the fortress's
order list with a RefItem TypeID test), so every method takes it as a
predicate rather than this package knowing the item references. The
authority checks the war period, the fortress and the running order; the
store's commits check the holder, the actor and the price.

Orders are keyed by fortress, not by guild (_SiegeFortressItemForge has no
guild column), so a capture leaves them in place for the new holder.
INFERENCE: neither binary shows a capture clearing them.

===========================================================================
*/
package fortress

import (
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
ForgeKind

The staff member an item belongs to: true for the member asked about.
================
*/
type ForgeKind func(itemRefID uint32) bool

/*
================
findForgeLocked
================
*/
func (r *Record) findForgeLocked(kind ForgeKind) int {
	for i, forge := range r.forges {
		if kind(forge.ItemRefID) {
			return i
		}
	}
	return -1
}

/*
================
ItemForge

The fortress's holder and its order for one staff member; ok is false for
a fortress this shard does not serve. Reading has no period restriction:
the query (6324A0) answers during a war too.
================
*/
func (a *Authority) ItemForge(divisionID string, fortressID uint32, kind ForgeKind) (holder int64, forge domain.FortressItemForgeRecord, present bool, ok bool) {
	if a == nil {
		return 0, domain.FortressItemForgeRecord{}, false, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return 0, domain.FortressItemForgeRecord{}, false, false
	}
	i := record.findForgeLocked(kind)
	if i < 0 {
		return record.Holder(), domain.FortressItemForgeRecord{}, false, true
	}
	return record.Holder(), record.forges[i], true, true
}

/*
================
forgeStoreLocked
================
*/
func (a *Authority) forgeStoreLocked() (domain.FortressItemForgeStore, error) {
	store, ok := a.store.(domain.FortressItemForgeStore)
	if !ok {
		return nil, fmt.Errorf("fortress: production transaction unavailable")
	}
	return store, nil
}

/*
================
StartItemForge

Rechecks the period, the fortress and the free staff slot under the lock
(632660: 0x2818, 3, 0x280A), then commits the payment and the order.
================
*/
func (a *Authority) StartItemForge(divisionID string, kind ForgeKind, start domain.FortressItemForgeStart) (uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.divisionLocked(divisionID)
	if state.warActive {
		return domain.FortressForgeErrWar, nil
	}
	record, ok := state.records[start.Forge.FortressID]
	if !ok {
		return domain.FortressForgeErrUnknown, nil
	}
	if record.findForgeLocked(kind) >= 0 {
		return domain.FortressForgeErrBusy, nil
	}
	store, err := a.forgeStoreLocked()
	if err != nil {
		return domain.FortressForgeErrFailure, err
	}
	start.Holder = record.Holder()
	code, err := store.StartFortressItemForge(divisionID, start)
	if code == 0 && err == nil {
		record.forges = append(record.forges, start.Forge)
	}
	return code, err
}

/*
================
CancelItemForge

Removes the order; nothing is refunded (621D40 issues only the remove).
================
*/
func (a *Authority) CancelItemForge(divisionID string, fortressID uint32, kind ForgeKind, itemRefID uint32) (uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[fortressID]
	if !ok {
		return domain.FortressForgeErrUnknown, nil
	}
	i := record.findForgeLocked(kind)
	if i < 0 {
		return domain.FortressForgeErrNone, nil
	}
	if record.forges[i].ItemRefID != itemRefID {
		return domain.FortressForgeErrUnknown, nil
	}
	store, err := a.forgeStoreLocked()
	if err != nil {
		return domain.FortressForgeErrFailure, err
	}
	if err := store.SaveFortressItemForge(divisionID, record.forges[i], false); err != nil {
		return domain.FortressForgeErrFailure, err
	}
	record.forges = append(record.forges[:i], record.forges[i+1:]...)
	return 0, nil
}

/*
================
CollectItemForge

Commits the collected bag row with the order's remaining count; the last
collection removes the order (CSiegeFortress_ConsumeItemForgeCount 627FF0,
then RemoveItemForge 62A220 at zero). The caller has admitted the count.
================
*/
func (a *Authority) CollectItemForge(divisionID string, kind ForgeKind, collect domain.FortressItemForgeCollect, count uint16) (uint8, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.divisionLocked(divisionID).records[collect.Forge.FortressID]
	if !ok {
		return domain.FortressForgeErrUnknown, nil
	}
	i := record.findForgeLocked(kind)
	if i < 0 {
		return domain.FortressForgeErrNone, nil
	}
	current := record.forges[i]
	if current.ItemRefID != collect.Forge.ItemRefID {
		return domain.FortressForgeErrUnknown, nil
	}
	if !current.Done {
		return domain.FortressForgeErrNotDone, nil
	}
	if count == 0 || count > current.Count {
		return domain.FortressForgeErrQuantity, nil
	}
	store, err := a.forgeStoreLocked()
	if err != nil {
		return domain.FortressForgeErrFailure, err
	}
	collect.Forge = current
	collect.Forge.Count -= count
	collect.Holder = record.Holder()
	code, err := store.CollectFortressItemForge(divisionID, collect)
	if code != 0 || err != nil {
		return code, err
	}
	if collect.Forge.Count == 0 {
		record.forges = append(record.forges[:i], record.forges[i+1:]...)
	} else {
		record.forges[i] = collect.Forge
	}
	return 0, nil
}

/*
================
AdvanceItemForges

CSiegeFortress_TickItemForge (61E5C0, from CGameWorld_Siege_Tick): an
order whose end has passed is marked done and its row rewritten
(QUERY_SIEGE_ITEM_FORGE_UPDATE). A failed write keeps the order running,
so the next tick tries again.
================
*/
func (a *Authority) AdvanceItemForges(nowMs int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	store, err := a.forgeStoreLocked()
	for divisionID, state := range a.divisions {
		for _, record := range state.records {
			for i := range record.forges {
				forge := record.forges[i]
				if forge.Done || nowMs < forge.EndsAtMs {
					continue
				}
				forge.Done = true
				if err == nil && store.SaveFortressItemForge(divisionID, forge, true) != nil {
					continue
				}
				record.forges[i] = forge
			}
		}
	}
}

/*
================
restoreItemForgesLocked

Restore's half for the production rows, when the store keeps them.
================
*/
func (a *Authority) restoreItemForgesLocked(divisionID string, state *division, store domain.FortressStore) error {
	forges, ok := store.(domain.FortressItemForgeStore)
	if !ok {
		return nil
	}
	rows, err := forges.FortressItemForges(divisionID)
	if err != nil {
		return fmt.Errorf("fortress: loading %s production: %w", divisionID, err)
	}
	for _, row := range rows {
		record, ok := state.records[row.FortressID]
		if !ok {
			return fmt.Errorf("fortress: stored production for fortress %d is not served", row.FortressID)
		}
		if len(record.forges) == 2 {
			return fmt.Errorf("fortress: fortress %d holds more than two production orders", row.FortressID)
		}
		record.forges = append(record.forges, row)
	}
	return nil
}
