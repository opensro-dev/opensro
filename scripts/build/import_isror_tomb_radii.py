"""
===========================================================================

import_isror_tomb_radii.py - Qin-Shi Tomb nest radii from ISRO-R's live table

qinshi_tomb_nests.tsv was recovered from ISRO-R's archived Tab_RefNest_Back,
whose tomb rows give every nest a home radius of 500-1000 and a generate
radius of 300-500. ISRO-R's active Tab_RefNest holds the same 1,042 nests
(same nest ids, tactics and anchors) with the radii the live server ran: 20
and 40 on almost every row. The native hatch passes the generate radius to
CMonster_SpawnInstance (0x5F6EB0) as the band each instance is placed in,
and the home radius bounds wandering through
CAITactics_IsWithinAuthoredHomeBoundary (0x545F70). With the archived values
each Floor 3 camp spread over a median 366 units instead of 30 and roamed
800-1000 from it; players reported both on 2026-11-18 (a thin-looking floor,
monsters drifting off their camps). tomb_spawn_fill_test.go measures it.

This rewrites only the radius and generateRadius columns, matching each row
to the active table by region and float32 anchor. Caps and respawn delays
keep their current values. It refuses to write anything if a row has no
match. Reads the backup; it never restores or modifies a database.

usage: py -3 scripts/build/import_isror_tomb_radii.py <ISRO-R shard .bak>

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
NEST_REGION, NEST_POSITION, NEST_RADIUS, NEST_GENERATE_RADIUS = 12, 14, 28, 32
# The tomb floors, regions 0x8002..0x8007.
TOMB_REGIONS = range( 32770, 32776 )
# qinshi_tomb_nests.tsv columns.
COL_REGION, COL_X, COL_Y, COL_Z, COL_RADIUS, COL_GENERATE_RADIUS = 1, 2, 3, 4, 5, 6
PROVENANCE = "# radius and generateRadius from ISRO-R's active Tab_RefNest (scripts/build/import_isror_tomb_radii.py)."

# ================
# read_active_nests
#
# Region -> [ ( x, y, z, radius, generateRadius ) ] from the active table.
# ================
def read_active_nests( path ):
	backup = Backup( path )
	nests = {}
	for _, at, page in backup.named_rows( "Tab_RefNest" ):
		fixed = struct.unpack_from( "<H", page, at + 2 )[0]
		raw = page[at + 4: at + fixed]
		if len( raw ) < NEST_GENERATE_RADIUS + 4:
			raise ValueError( "Unsupported Tab_RefNest row layout %d" % fixed )
		region = struct.unpack_from( "<H", raw, NEST_REGION )[0]
		if region not in TOMB_REGIONS:
			continue
		x, y, z = struct.unpack_from( "<fff", raw, NEST_POSITION )
		radius = struct.unpack_from( "<i", raw, NEST_RADIUS )[0]
		generate = struct.unpack_from( "<i", raw, NEST_GENERATE_RADIUS )[0]
		nests.setdefault( region, [] ).append( ( x, y, z, radius, generate ) )
	return nests

# ================
# match
#
# The active row at this anchor, or None. Two rows at one anchor must agree.
# ================
def match( candidates, x, y, z ):
	found = [ c for c in candidates if abs( c[0] - x ) <= ANCHOR_EPSILON and abs( c[1] - y ) <= ANCHOR_EPSILON and abs( c[2] - z ) <= ANCHOR_EPSILON ]
	if not found:
		return None
	if len( { ( c[3], c[4] ) for c in found } ) != 1:
		raise ValueError( "anchor %s,%s,%s has conflicting radii %s" % ( x, y, z, found ) )
	return found[0]

# ================
# rewrite
#
# The new TSV text and the number of rows whose radii changed.
# ================
def rewrite( text, nests ):
	out = []
	changed = 0
	missing = []
	for line in text.split( "\n" ):
		if line == PROVENANCE:
			continue
		if not line or line.startswith( "#" ):
			out.append( line )
			continue
		columns = line.split( "\t" )
		region = int( columns[COL_REGION] )
		x, y, z = float( columns[COL_X] ), float( columns[COL_Y] ), float( columns[COL_Z] )
		row = match( nests.get( region, [] ), x, y, z )
		if row is None:
			missing.append( "%s %d %s,%s,%s" % ( columns[0], region, columns[COL_X], columns[COL_Y], columns[COL_Z] ) )
			continue
		radius, generate = str( row[3] ), str( row[4] )
		if ( columns[COL_RADIUS], columns[COL_GENERATE_RADIUS] ) != ( radius, generate ):
			changed += 1
		columns[COL_RADIUS], columns[COL_GENERATE_RADIUS] = radius, generate
		out.append( "\t".join( columns ) )
	if missing:
		raise SystemExit( "no active Tab_RefNest row for %d nests, e.g. %s" % ( len( missing ), missing[:5] ) )
	# The provenance line follows the first comment line.
	out.insert( 1, PROVENANCE )
	return "\n".join( out ), changed

# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser( description=__doc__.split( "\n" )[3] )
	parser.add_argument( "backup", type=pathlib.Path, help="ISRO-R SILKROAD_R_SHARD .bak" )
	args = parser.parse_args()
	text = NESTS_TSV.read_text( encoding="utf-8" )
	updated, changed = rewrite( text, read_active_nests( args.backup ) )
	NESTS_TSV.write_text( updated, encoding="utf-8", newline="\n" )
	print( "%s: %d rows changed" % ( NESTS_TSV.name, changed ) )

if __name__ == "__main__":
	main()
