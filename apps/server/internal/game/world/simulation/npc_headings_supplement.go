/*
===========================================================================

npc_headings_supplement.go - reviewed missing v1.150 NPC placement headings

ISRO-R shard SHA256:
89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45
Twelve exact placements agree in Tab_RefNest and Tab_RefNest_Back. Five moved
placements below are explicit inferences, keyed to the original v1.150 position.
SQL SMALLINT headings retain their unsigned wire bits, including negative SQL
values. See docs/NPC_FACING_2026-10-01.md for joins, offsets and decisions.

===========================================================================
*/
package simulation

/*
================
supplementalNPCHeadings

Do not replace this with codename-only or nearest-neighbour matching. Reviewed
relocations apply only to these exact original placements; future moves must
receive their own evidence and pass the exhaustive published-roster test.
================
*/
var supplementalNPCHeadings = map[npcHeadingKey]uint16{
	// Soldier Sangnam [Teleport]: exact ISRO-R placement, nest 39.
	{"NPC_CH_SOLDIER_EA2", 25001, 13872, -1, 17650}: 32767,
	// Soldier Choiyoung [Teleport]: exact ISRO-R placement, nest 50.
	{"NPC_CH_SOLDIER_EM1", 25000, 10095, 0, 19026}: 49333,
	// Soldier Hogang [Teleport]: exact ISRO-R placement, nest 4375.
	{"NPC_CH_SOLDIER_WE1", 25255, 3350, -3, 342}: 0,

	// Inference: the same thief moved (+8.46, -4.23) in X/Z. vSRO and both ISRO-R tables agree on 8191;
	// retain the facing without moving him.
	{"NPC_TD_THIEF_D", 24758, 6275, 194, 4015}: 8191,
	// Soldier Jingyo [Teleport]: exact ISRO-R placement, nest 4567.
	{"NPC_CH_SOLDIER_SO1", 25000, 9301, -4, 307}: 16019,
	// Specialty Trader Payi: exact ISRO-R placement, nest 11107.
	{"NPC_TK_SPECIAL", 26753, 1314, 1131, 14049}: 13653,
	// Specialty Trader Seopok: exact ISRO-R placement, nest 11108.
	{"NPC_CH_SPECIAL2", 23712, 4110, 13833, 15159}: 52792,
	// Specialty Trader Hounah: exact ISRO-R placement, nest 11109.
	{"NPC_WC_SPECIAL2", 23445, 11324, 1930, 8337}: 32767,

	// Inference: Saha moved +9 in X only. vSRO and both ISRO-R tables agree on 10922; the small station
	// translation does not justify a new facing.
	{"NPC_CA_ACCESSORY", 27243, 16358, 1800, 14604}: 10922,
	// Specialty Trader Toson: exact ISRO-R placement, nest 11137.
	{"NPC_CA_SPECIAL", 27244, 8371, 1800, 18233}: 16201,
	// Specialty Trader Osaman: exact ISRO-R placement, nest 14822.
	{"NPC_RM_SPECIAL", 23411, 3771, 26288, 1049}: 54430,
	// Guide Raffy: exact ISRO-R placement, nest 19475.
	{"NPC_EU_ADVICE2", 26957, 16502, 839, 13325}: 16565,
	// Guide Lipria: exact ISRO-R placement, nest 19476.
	{"NPC_EU_ADVICE", 27471, 13493, 827, 4127}: 24575,

	// Inference: Riise moved (-6, +22) in X/Z at the same elevation. Both later tables retain 21845;
	// preserve that facing at his v1.150 station.
	{"NPC_EU_ADVICE3", 26959, 5689, 836, 11166}: 21845,
	// Specialty Trader Tina: exact ISRO-R placement, nest 20986.
	{"NPC_EU_SPECIAL", 26959, 3510, 804, 2282}: 16383,

	// Inference: Constantinople So-Ok moved (+11, +25) in X/Z. vSRO and the ISRO-R archive agree on 24757;
	// retain this station facing.
	{"NPC_CH_EVENT_KISAENG1", 26959, 8328, 837, 11130}: 24757,

	// Inference: Samarkand So-Ok uses archived ISRO-R 16201 at the same Y/Z, X +21. Prefer this placement
	// evidence over the vSRO nest at different X/Z (heading 0). Both have radius 90; this is inferred.
	{"NPC_CH_EVENT_KISAENG1", 26265, 9113, -1068, 15696}: 16201,
}
