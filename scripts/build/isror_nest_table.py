"""
===========================================================================

isror_nest_table.py - the one ISRO-R nest table the port takes balance from

ISRO-R's shard backup holds two nest tables. The active Tab_RefNest carries
its operator's tuning: radius 20, generate radius 40 (a containment radius
below the spawn radius, so every outer spawn walks home and piles up), cap
5 and 1-2 s respawns, on 5,124 outdoor nests whose archived row holds the
700 / 600 the v1.188 server data shows (#4149). The archived
Tab_RefNest_Back is the native table. Every importer that reads the backup
reads this name, and every later-world population export must record it.

===========================================================================
"""

NATIVE_NEST_TABLE = "Tab_RefNest_Back"


# ================
# require_native_nest_table
#
# Refuses nest data drawn from any other table. what names the source in
# the message (an export path, a generated file).
# ================
def require_native_nest_table(table, what):
	if table != NATIVE_NEST_TABLE:
		raise ValueError(
			f"{what} draws its nests from {table or 'an unnamed table'}, not ISRO-R's native "
			f"{NATIVE_NEST_TABLE} (the active Tab_RefNest is its operator's tuning): re-extract it")
