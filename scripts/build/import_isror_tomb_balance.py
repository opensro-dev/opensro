"""
===========================================================================

import_isror_tomb_balance.py - Qin-Shi Tomb nest balance from ISRO-R's live table

qinshi_tomb_nests.tsv was recovered from ISRO-R's archived Tab_RefNest_Back.
ISRO-R's active Tab_RefNest holds the same 1,042 nests (same nest ids,
tactics and anchors) with the balance its live server ran:
- radius and generateRadius 20 / 40 on almost every row, where the archive
  had 800-1000 / 300-500 (each Floor 3 camp spread over a median 366 units
  instead of 30; tomb_spawn_fill_test.go measures it);
- respawn delays of 1-2 s and a cap of 5 on most rows, where the archive
  had 6-160 s and caps of 6-28.
Players reported the thin floor and drifting monsters on 2026-11-18; the
owner asked for the live caps and delays too.

Each TSV row is matched to its archived row by region, float32 anchor and
its current balance (two nests can share an anchor), which names the nest
id; the active row with that id supplies the new radius, generateRadius,
delayMinSec, delayMaxSec and maxCount. Every other column is kept.

Rows the active table leaves to a trigger (maxCount 0) while the TSV holds
a restored unique (maxCount 1, the Floor 5-6 and Tomb uniques the floors'
InitTriggers spawn) keep their TSV values. The importer refuses to write
anything if a row has no single match. It reads the backup; it never
restores or modifies a database.

usage: py -3 scripts/build/import_isror_tomb_balance.py <ISRO-R shard .bak>

===========================================================================
"""

import argparse
import pathlib
import struct

from loot_sql_pages import Backup

HERE = pathlib.Path( __file__ ).resolve().parent
NESTS_TSV = HERE.parent.parent / "apps" / "server" / "internal" / "game" / "world" / "monster" / "data" / "qinshi_tomb_nests.tsv"

# Anchors match within this distance: the TSV prints float32 values rounded
# to nine significant figures.
ANCHOR_EPSILON = 0.05
# Tab_RefNest fixed-row offsets (after the 4-byte row header).
NEST_ID, NEST_REGION, NEST_POSITION, NEST_BALANCE = 0, 12, 14, 28
# The tomb floors, regions 0x8002..0x8007.
TOMB_REGIONS = range( 32770, 32776 )
# qinshi_tomb_nests.tsv columns.
COL_REGION, COL_X, COL_Y, COL_Z = 1, 2, 3, 4
COL_RADIUS, COL_GENERATE_RADIUS, COL_DELAY_MIN, COL_DELAY_MAX, COL_MAX_COUNT = 5, 6, 8, 9, 10
BALANCE_COLUMNS = ( COL_RADIUS, COL_GENERATE_RADIUS, COL_DELAY_MIN, COL_DELAY_MAX, COL_MAX_COUNT )
# The archived delays and cap a row still carries before this import (the
# first import, #3874, already replaced its radii).
ARCHIVE_MATCH_COLUMNS = ( COL_DELAY_MIN, COL_DELAY_MAX, COL_MAX_COUNT )
PROVENANCE_PREFIX = "# radius"
PROVENANCE = ( "# radius, generateRadius, delayMinSec, delayMaxSec and maxCount from ISRO-R's active Tab_RefNest;"
			   " restored uniques keep theirs (scripts/build/import_isror_tomb_balance.py)." )

# ================
# read_nests
#
# Tomb rows of one table: [ ( id, region, x, y, z, balance ) ], balance as
# ( radius, generateRadius, delayMin, delayMax, maxCount ).
# ================
def read_nests( backup, table ):
	out = []
	for _, at, page in backup.named_rows( table ):
		fixed = struct.unpack_from( "<H", page, at + 2 )[0]
		raw = page[at + 4: at + fixed]
		if len( raw ) < NEST_BALANCE + 24:
			raise ValueError( "Unsupported %s row layout %d" % ( table, fixed ) )
		region = struct.unpack_from( "<H", raw, NEST_REGION )[0]
		if region not in TOMB_REGIONS:
			continue
		nest_id = struct.unpack_from( "<I", raw, NEST_ID )[0]
		x, y, z = struct.unpack_from( "<fff", raw, NEST_POSITION )
		radius, generate, _champion, delay_min, delay_max, max_count = struct.unpack_from( "<iiiiii", raw, NEST_BALANCE )
		out.append( ( nest_id, region, x, y, z, ( radius, generate, delay_min, delay_max, max_count ) ) )
	return out

# ================
# at_anchor
# ================
def at_anchor( nests, region, x, y, z ):
	return [ n for n in nests if n[1] == region and abs( n[2] - x ) <= ANCHOR_EPSILON and abs( n[3] - y ) <= ANCHOR_EPSILON and abs( n[4] - z ) <= ANCHOR_EPSILON ]

# ================
# archived_nest
#
# The archived nest id for one TSV row, or None. Several nests at one anchor
# are told apart by the archived delays and cap the row still carries, or,
# once imported, by the active balance it now carries.
# ================
def archived_nest( archived, active_by_id, columns ):
	region = int( columns[COL_REGION] )
	x, y, z = float( columns[COL_X] ), float( columns[COL_Y] ), float( columns[COL_Z] )
	found = at_anchor( archived, region, x, y, z )
	if len( found ) <= 1:
		return found[0][0] if found else None
	current = tuple( int( float( columns[c] ) ) for c in BALANCE_COLUMNS )
	archive_key = tuple( int( columns[c] ) for c in ARCHIVE_MATCH_COLUMNS )
	exact = [ n for n in found if n[5][2:] == archive_key or active_by_id.get( n[0] ) == current ]
	return exact[0][0] if len( exact ) == 1 else None

# ================
# restored_unique
#
# The active table leaves this nest to a trigger while the TSV spawns its
# restored unique: keep the TSV's values.
# ================
def restored_unique( columns, active ):
	return active[4] == 0 and int( columns[COL_MAX_COUNT] ) > 0

# ================
# rewrite
#
# The new TSV text, the number of rows whose balance changed and the number
# of restored uniques kept.
# ================
def rewrite( text, archived, active ):
	active_by_id = { n[0]: n[5] for n in active }
	out = []
	changed = kept = 0
	missing = []
	for line in text.split( "\n" ):
		if line.startswith( PROVENANCE_PREFIX ):
			continue
		if not line or line.startswith( "#" ):
			out.append( line )
			continue
		columns = line.split( "\t" )
		nest_id = archived_nest( archived, active_by_id, columns )
		balance = active_by_id.get( nest_id )
		if balance is None:
			missing.append( "%s %s %s,%s,%s" % ( columns[0], columns[COL_REGION], columns[COL_X], columns[COL_Y], columns[COL_Z] ) )
			continue
		if restored_unique( columns, balance ):
			kept += 1
			out.append( line )
			continue
		updated = list( columns )
		for column, value in zip( BALANCE_COLUMNS, balance ):
			updated[column] = str( value )
		if updated != columns:
			changed += 1
		out.append( "\t".join( updated ) )
	if missing:
		raise SystemExit( "no single ISRO-R nest for %d rows, e.g. %s" % ( len( missing ), missing[:5] ) )
	# The provenance line follows the first comment line.
	out.insert( 1, PROVENANCE )
	return "\n".join( out ), changed, kept

# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser( description=__doc__.split( "\n" )[3] )
	parser.add_argument( "backup", type=pathlib.Path, help="ISRO-R SILKROAD_R_SHARD .bak" )
	args = parser.parse_args()
	backup = Backup( args.backup )
	text = NESTS_TSV.read_text( encoding="utf-8" )
	updated, changed, kept = rewrite( text, read_nests( backup, "Tab_RefNest_Back" ), read_nests( backup, "Tab_RefNest" ) )
	NESTS_TSV.write_text( updated, encoding="utf-8", newline="\n" )
	print( "%s: %d rows changed, %d restored uniques kept" % ( NESTS_TSV.name, changed, kept ) )

if __name__ == "__main__":
	main()
