/*
===========================================================================

later_fortresses.go - the Bandit and Hotan fortress switches (port-only).

Port-only, not native. SRO_FORT_BIJEOKDAN and SRO_FORT_HOTAN are off by
default (#3886). v1.150 ships both fortresses' siege worlds (INS_FORT_BJ,
INS_FORT_HT) and emblems but no siegefortress row, event zone or
structure for them; the importers (scripts/bandit_fortress_import.py,
scripts/hotan_fortress_import.py) stage the v1.188 client's while a
switch is on. As with Constantinople the fortress line is data-driven,
so this file owns only the switches: enterworld/fortressdata.go asks
them before a row joins the client-facing fortress list.

===========================================================================
*/
package monster

// EnvBanditFortress is the Bandit fortress shard switch (shardsettings,
// nomad var fort_bijeokdan). Off is the native v1.150 shard.
const EnvBanditFortress = "SRO_FORT_BIJEOKDAN"

// EnvHotanFortress is the Hotan fortress shard switch (shardsettings,
// nomad var fort_hotan). Off is the native v1.150 shard.
const EnvHotanFortress = "SRO_FORT_HOTAN"

/*
================
BanditFortressEnabled
================
*/
func BanditFortressEnabled() bool { return switchOn(EnvBanditFortress) }

/*
================
HotanFortressEnabled
================
*/
func HotanFortressEnabled() bool { return switchOn(EnvHotanFortress) }
