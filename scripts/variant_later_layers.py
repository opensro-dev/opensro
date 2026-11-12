"""
===========================================================================

variant_later_layers.py - the later sources' optional world layers, in order

Port-only, not native. The Alexandria variant build stacks a run of
independently optional layers on top of its own stage: each reads a
later source (the v1.696 client, the ISRO-R nest extraction), has its
own switch, and on its off path reaps what a previous on build of a
reused variant root left behind. Each layer owns its import module;
this module owns only their order and the variant manifest key each
one's summary lands under, so the builder runs them in one call and a
new layer is one row here, not another block in the builder.

The order is load-bearing: the Sky Temple's identity space and the
catalogue merges come first, and every later layer merges after them.

===========================================================================
"""
import arabia_import
import constantinople_import
import gears_stone_import
import hunting2_import
import jupiter_import
import muhan_import
import skytemple_import

# (variant manifest key, layer entry point taking (native, out)), in
# build order.
LATER_LAYERS = (
	# The Sky Temple fields: their own switch and source, after the
	# populations whose identity space they share.
	("skyTemple", skytemple_import.add_skytemple),
	# The Arabia continent: after the Sky Temple fields whose identity
	# and catalogue merges it follows.
	("arabia", arabia_import.add_arabia),
	("jupiter", jupiter_import.add_jupiter),
	("muhan", muhan_import.add_muhan),
	("hunting2", hunting2_import.add_hunting2),
	# The Constantinople fortress: its own switch and source (the v1.696
	# client), on the merge machinery the Sky Temple shares.
	("constantinople", constantinople_import.add_constantinople),
	# The gears magic stones ride the mall's own switch
	# (SRO_MALL_CATEGORIES): on stages their item shard beside the mall
	# layout that lists them.
	("gearsStones", gears_stone_import.add_stones),
)


# ================
# add_later_layers
#
# Every later layer in order; each one's summary under its manifest
# key. A layer that is off returns its own off summary (and has reaped
# a previous on build), so the result always names every layer.
# ================
def add_later_layers(native, out):
	return {key: add(native, out) for key, add in LATER_LAYERS}
