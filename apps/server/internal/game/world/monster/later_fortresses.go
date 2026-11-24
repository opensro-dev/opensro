/*
===========================================================================

later_fortresses.go - the Hotan fortress switch (port-only).

Port-only, not native. SRO_FORT_HOTAN is off by default (#3886). v1.150
ships the fortress's siege world (INS_FORT_HT) and emblem but no
siegefortress row, event zone or structure for it; the importer
(scripts/hotan_fortress_import.py) stages the v1.188 client's while the
switch is on. The fortress line is data-driven, so this file owns only
the switch: enterworld/fortressdata.go asks it before the row joins the
client-facing fortress list.

The Bandit fortress has no switch: it is later-native content the game
ships (#3586), so scripts/bandit_fortress_import.py imports it into
every build and the fortress list carries its row unconditionally.

===========================================================================
*/
package monster

// EnvHotanFortress is the Hotan fortress shard switch (shardsettings,
// nomad var fort_hotan). Off is the native v1.150 shard.
const EnvHotanFortress = "SRO_FORT_HOTAN"

/*
================
HotanFortressEnabled
================
*/
func HotanFortressEnabled() bool { return switchOn(EnvHotanFortress) }
