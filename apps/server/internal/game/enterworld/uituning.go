/*
===========================================================================

uituning.go - the client tuning a world-entry bootstrap carries

Port-only, not native. Three client values come from the tunables registry
(class Entry): the Experimental "Short notices" banner's time shown and
fade, and the client's own wait for a chat confirmation. They are read
from the snapshot at each bootstrap, so an approved change reaches each
player at their next world entry. A shard at every default sends nothing,
so its bootstrap is the one an older build sent; the client (ui-tuning.ts)
checks each value against the same bounds and keeps its built-in one
otherwise.

===========================================================================
*/
package enterworld

import "opensro.online/server/internal/game/tunables"

/*
================
UiTuning
================
*/
type UiTuning struct {
	ShortNoticeShowMs int64 `json:"shortNoticeShowMs"`
	ShortNoticeFadeMs int64 `json:"shortNoticeFadeMs"`
	ChatAckTimeoutMs  int64 `json:"chatAckTimeoutMs"`
}

/*
================
UiTuningFrom

The tuning of one snapshot, or nil while every value is its default.
================
*/
func UiTuningFrom(s *tunables.Settings) *UiTuning {
	keys := []string{tunables.ShortNoticeShowMs, tunables.ShortNoticeFadeMs, tunables.ChatAckTimeoutMs}
	tuned := false
	for _, key := range keys {
		setting, _ := tunables.Lookup(key)
		if float64(s.Int(key)) != setting.Default {
			tuned = true
		}
	}
	if !tuned {
		return nil
	}
	return &UiTuning{
		ShortNoticeShowMs: s.Int(tunables.ShortNoticeShowMs),
		ShortNoticeFadeMs: s.Int(tunables.ShortNoticeFadeMs),
		ChatAckTimeoutMs:  s.Int(tunables.ChatAckTimeoutMs),
	}
}
