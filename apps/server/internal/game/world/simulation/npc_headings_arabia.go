/*
===========================================================================

npc_headings_arabia.go - inferred facing for the optional Arabia town

Port-only, not native: these placements exist only while SRO_ARABIA imports
the v1.696 npcpos.txt rows of the RN_ARABIA_* regions (arabia_import.py).
Neither the v1.696 client (npcpos has no direction column) nor any source
the port holds carries their headings; the ISRO-R Tab_RefNest rows that do
are not available, so every value below is an inference from the town's
placed geometry (the v1.696 .o2 placements of the NPC's region and its
neighbours, seasonal layer members excluded), reviewed in game:

	shop   a merchant within 12 m of a shop, stall or storage model faces
	       away from it, toward its customers;
	open   any other NPC faces away from the solid models within 30 m
	       (buildings, compounds, walls, gates, stalls; each weighted by
	       the inverse of its distance), toward the open street;
	watch  a field post with no solid model within 30 m faces away from
	       the town's guards' centroid, as a lookout.

The heading word is the native yaw convention (atan2(dz, dx) over the full
circle, headingWordFromDelta). Join by codename and exact position, as the
other tables do; a key no later rebuild produces simply stops matching.

===========================================================================
*/
package simulation

/*
================
arabiaNPCHeadings

Exact placement keys keep the native roster unchanged when Arabia is off.
================
*/
var arabiaNPCHeadings = map[npcHeadingKey]uint16{
	{"NPC_ANCIENT_SCHOLAR", 22106, 16802, -4333, 18300}:           31782, // shop
	{"NPC_AR_ABSHAD", 22363, 11759, -3939, 1038}:                  13238, // open
	{"NPC_AR_ACCESSORY", 22617, 10275, -4313, 9505}:               33831, // shop
	{"NPC_AR_CASTLESOLDIER_1", 22618, 10727, -2366, 3911}:         59160, // open
	{"NPC_AR_CASTLESOLDIER_2", 22362, 13170, -2366, 18543}:        6390,  // open
	{"NPC_AR_CHOUKRI", 22361, 7214, -4321, 4498}:                  4782,  // shop
	{"NPC_AR_COMMERCE1", 22874, 8469, -4307, 8729}:                59749, // open
	{"NPC_AR_COMMERCE2", 22363, 8458, -3200, 9996}:                46294, // open
	{"NPC_AR_ESHARK", 22107, 1269, -4333, 14737}:                  16660, // open
	{"NPC_AR_FARMER1", 22357, 19149, -2, 12865}:                   9641,  // open
	{"NPC_AR_FARMER2", 22357, 18022, -1, 12435}:                   20764, // open
	{"NPC_AR_FARMER3", 22869, 18516, 0, 12628}:                    47961, // open
	{"NPC_AR_FOUAD", 22875, 9721, -4306, 5487}:                    47489, // open
	{"NPC_AR_GUILD", 22107, 256, -4333, 17290}:                    62153, // shop
	{"NPC_AR_HORSE", 22874, 2894, -4307, 8152}:                    52998, // open
	{"NPC_AR_HUNTER", 22874, 8420, -4307, 5657}:                   14718, // open
	{"NPC_AR_KAREM", 22106, 3495, -4321, 15092}:                   7374,  // open
	{"NPC_AR_KHALED", 22361, 4548, -4323, 12426}:                  15130, // shop
	{"NPC_AR_MACHINE_1", 22864, 3025, -39, 4562}:                  15244, // open
	{"NPC_AR_MACHINE_2", 22607, 6932, -23, 1736}:                  13296, // open
	{"NPC_AR_MACHINE_3", 22094, 17459, 741, 15314}:                206,   // open
	{"NPC_AR_MACHINE_4", 21838, 975, -739, 14293}:                 63545, // open
	{"NPC_AR_MAHMUD", 22363, 16116, -4127, 5326}:                  53828, // watch
	{"NPC_AR_MALAK", 22361, 7222, -4320, 14643}:                   39941, // shop
	{"NPC_AR_MERCHANT", 22617, 15259, -4324, 799}:                 33044, // open
	{"NPC_AR_MUHAMMAD", 22354, 6678, 2999, 397}:                   61181, // open
	{"NPC_AR_NAJIMA", 22361, 9259, -4321, 2651}:                   18206, // shop
	{"NPC_AR_POTION", 22361, 4342, -4319, 16705}:                  53792, // shop
	{"NPC_AR_SHARIAL", 22618, 13179, -2366, 2749}:                 8984,  // open
	{"NPC_AR_SHEHERAZADE", 22618, 13666, -2366, 1297}:             7983,  // open
	{"NPC_AR_SMITH", 22874, 15943, -4308, 12906}:                  38903, // shop
	{"NPC_AR_SOLDIER_1", 22617, 2445, -4323, 5430}:                11268, // open
	{"NPC_AR_SOLDIER_2", 22617, 2437, -4320, 3166}:                60144, // open
	{"NPC_AR_SOLDIER_3", 22106, 15092, -4324, 5596}:               22456, // open
	{"NPC_AR_SOLDIER_4", 22106, 16887, -4324, 5554}:               9424,  // open
	{"NPC_AR_SPECIAL", 22617, 5370, -4326, 12850}:                 14802, // shop
	{"NPC_AR_SPECIAL_INVESTIGATION_1", 20050, 2809, -7308, 14894}: 32370, // open
	{"NPC_AR_SPECIAL_INVESTIGATION_2", 20828, 8433, -1955, 15131}: 21695, // open
	{"NPC_AR_SPECIAL_SOLDIER_1", 22101, 3228, 0, 9288}:            5741,  // open
	{"NPC_AR_SPECIAL_SOLDIER_10", 18769, 13831, -3132, 6958}:      3003,  // open
	{"NPC_AR_SPECIAL_SOLDIER_12", 21594, 10217, -3229, 12998}:     46727, // watch
	{"NPC_AR_SPECIAL_SOLDIER_13", 21594, 11068, -3315, 13113}:     46847, // watch
	{"NPC_AR_SPECIAL_SOLDIER_14", 21079, 6060, -3245, 2493}:       43035, // watch
	{"NPC_AR_SPECIAL_SOLDIER_15", 20566, 13051, -4876, 16481}:     43242, // watch
	{"NPC_AR_SPECIAL_SOLDIER_16", 20828, 8402, -1927, 17361}:      50883, // watch
	{"NPC_AR_SPECIAL_SOLDIER_2", 22101, 3180, 0, 9906}:            8575,  // open
	{"NPC_AR_SPECIAL_SOLDIER_3", 22101, 3756, 0, 9014}:            3349,  // open
	{"NPC_AR_SPECIAL_SOLDIER_4", 22611, 8582, 638, 8196}:          64064, // open
	{"NPC_AR_SPECIAL_SOLDIER_5", 22611, 8793, 606, 8801}:          65349, // open
	{"NPC_AR_SPECIAL_SOLDIER_6", 22611, 8806, 611, 7590}:          62942, // open
	{"NPC_AR_SPECIAL_SOLDIER_7", 20815, 4028, 2026, 6467}:         38152, // watch
	{"NPC_AR_SPECIAL_SOLDIER_8", 20050, 4440, -7032, 17639}:       13981, // open
	{"NPC_AR_SPECIAL_SOLDIER_9", 19279, 6460, -8529, 15905}:       41119, // watch
	{"NPC_AR_SSADATUNE", 22107, 9692, -4333, 15370}:               22855, // open
	{"NPC_AR_THIEF", 22363, 9031, -3199, 13832}:                   746,   // open
	{"NPC_AR_TOWNSOLDIER_1", 22617, 18071, -4325, 3399}:           31017, // open
	{"NPC_AR_TOWNSOLDIER_10", 22620, 2805, -4305, 6183}:           41143, // open
	{"NPC_AR_TOWNSOLDIER_11", 22364, 4492, -4305, 14224}:          303,   // open
	{"NPC_AR_TOWNSOLDIER_12", 22364, 4472, -4305, 12371}:          9901,  // open
	{"NPC_AR_TOWNSOLDIER_13", 22619, 17053, -4305, 4997}:          2831,  // open
	{"NPC_AR_TOWNSOLDIER_14", 22619, 17046, -4305, 3919}:          61469, // open
	{"NPC_AR_TOWNSOLDIER_2", 22617, 18095, -4326, 2417}:           32455, // open
	{"NPC_AR_TOWNSOLDIER_3", 22874, 13379, -4307, 498}:            14444, // open
	{"NPC_AR_TOWNSOLDIER_4", 22874, 15056, -4306, 508}:            35268, // open
	{"NPC_AR_TOWNSOLDIER_5", 22362, 15518, -4333, 3754}:           49282, // open
	{"NPC_AR_TOWNSOLDIER_6", 22362, 16740, -4333, 3736}:           53161, // open
	{"NPC_AR_TOWNSOLDIER_7", 22875, 16091, -4305, 1320}:           52936, // open
	{"NPC_AR_TOWNSOLDIER_8", 22875, 16421, -4305, 131}:            24675, // open
	{"NPC_AR_TOWNSOLDIER_9", 22620, 2538, -4305, 7410}:            28887, // open
	{"NPC_AR_WAREHOUSE", 22106, 14886, -4333, 16828}:              61570, // shop
	{"NPC_AR_YAMAIN", 22106, 12857, -4321, 7692}:                  214,   // open
	{"NPC_AR_ZBAYDA", 22107, 4571, -4333, 16486}:                  42638, // open
	{"NPC_BOSS_DUNGEON_PRIEST", 22106, 16913, -4333, 16288}:       32850, // open
	{"NPC_CH_EVENT_CLOWN_BALLOON", 22106, 13895, -4203, 14884}:    49171, // open
	{"NPC_CH_EVENT_KISAENG1", 22106, 14995, -4324, 14256}:         20308, // open
	{"NPC_CH_EVENT_KISAENG2", 22106, 14518, -4325, 14419}:         22833, // open
	{"NPC_CH_GACHA_MACHINE", 22106, 16962, -4333, 13796}:          36577, // open
	{"NPC_CH_GACHA_OPERATOR", 22106, 17046, -4324, 14215}:         34787, // open
	{"NPC_GACHA_LAMP_GENIE", 22106, 13158, -4333, 14919}:          45947, // open
	{"NPC_LAST_FIELD_PRIEST", 22106, 15258, -4333, 17980}:         62314, // open
	{"NPC_SURVIVAL_ARENA_MANAGER", 22106, 16771, -4333, 16991}:    30380, // open
}
