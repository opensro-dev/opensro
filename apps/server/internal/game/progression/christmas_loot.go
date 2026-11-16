/*
===========================================================================

christmas_loot.go - the winter event's snow flakes: who drops them and
what So-ok gives for them

The v1.188 server's WINTER_EVENT_2009, ported (SRO_CHRISTMAS_EVENT in
mode winter). A Snow Slave's death drops one Snow flake
(ITEM_ETC_E091216_SNOWFLAKE, imported from the v1.696 client by
scripts/winter_event_import.py) from the quest's own drop hook,
outside the loot tables: natively half the time (rate 50.0), here every
time (owner, 2026-11-16). So-ok
(NPC_CH_EVENT_KISAENG1, talk SN_TALK_QEV_SERVER_EVENT_QUEST_A..E) trades
flakes for gifts while the bag has room, every exchange of the click at
once, each gift rolled from the native table.

Port-only, owner decisions 2026-11-16:
  - the native exchange also takes one Pine (ITEM_ETC_E091216_PINE) per
    gift; no source of the pine survives in the server, and the client's
    own tooltip asks only "Bring 10 snow flakes to So-Ok", so the pine
    is dropped: ten flakes buy a gift;
  - the native table names v1.188 items the v1.150 data lacks; each
    present item keeps its native weight under its v1.150 name (the
    ITEM_EVENT_ copies are the MALL_ and COS_ rows), and the absent ones
    (the 2010 Halloween avatars, the degree 11 weapons and the four
    balloons, 20.905% together) are left out, their weight spread over
    the rest by the roll's normalisation.

===========================================================================
*/
package progression

const (
	// ChristmasSnowflakeCodename is the winter event's token.
	ChristmasSnowflakeCodename = "ITEM_ETC_E091216_SNOWFLAKE"

	// ChristmasSnowflakesPerGift is one gift's price (the native ten;
	// the pine is not asked, see the header).
	ChristmasSnowflakesPerGift = 10

	// ChristmasSnowflakeDropPercent is a Snow Slave's chance to drop its
	// flake. Native is 50 (the quest's rate 50.0); port-only, the owner's
	// call (2026-11-16): every Snow Slave drops its flake.
	ChristmasSnowflakeDropPercent = 100

	// ChristmasSnowSlaveCodename is the hunted Snow Slave.
	ChristmasSnowSlaveCodename = "MOB_EV_WINTER_SNOWMAN"

	// ChristmasSockCodename is the 2007 event's Red Santa Socks, which
	// the winter event handed out before it moved to the native flakes.
	ChristmasSockCodename = "ITEM_ETC_E071225_SANTASOCKS"
)

// christmasGifts is the native table's v1.150 half, at the native
// weights (percent of the full native table).
var christmasGifts = []HalloweenGift{
	{Codename: "ITEM_MALL_DAMAGE_ABS_20P_SCROLL", Count: 1, Weight: 5},
	{Codename: "ITEM_MALL_DAMAGE_INC_20P_SCROLL", Count: 1, Weight: 5},
	{Codename: "ITEM_MALL_GLOBAL_CHATTING", Count: 1, Weight: 5},
	{Codename: "ITEM_MALL_REVERSE_RETURN_SCROLL", Count: 1, Weight: 4.095},
	{Codename: "ITEM_MALL_RETURN_SCROLL_HIGH_SPEED", Count: 1, Weight: 15},
	{Codename: "ITEM_MALL_REPAIR_HAMMER", Count: 1, Weight: 15},
	{Codename: "ITEM_COS_C_SCARABAEUS_SCROLL", Count: 1, Weight: 6},
	{Codename: "ITEM_COS_C_WILDPIG_SCROLL", Count: 1, Weight: 6},
	{Codename: "ITEM_COS_C_OSTRICH_SCROLL", Count: 1, Weight: 6},
	{Codename: "ITEM_COS_C_PEGASUS_SCROLL", Count: 1, Weight: 6},
	{Codename: "ITEM_COS_C_RUDOLPH", Count: 1, Weight: 6},
}

/*
================
ChristmasGifts

The gifts So-ok rolls for ten snow flakes.
================
*/
func ChristmasGifts() []HalloweenGift {
	return christmasGifts
}
