# ===========================================================================
#
# test_ui_icon_atlas.py - the icon sheet's packing and pixel parity
#
# Synthetic icons with known gradients pack into a sheet (a private build
# tree): every icon's inner rect holds the source bytes exactly, the
# two-texel gutters repeat the icon's own edge texels (a linear tap at the
# icon's edge blends what it blended as a standalone texture), the shelf
# layout stays deterministic in path order, and a family taller than one
# sheet starts the next one.
#
# ===========================================================================

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image

sys.path.insert( 0, str( Path( __file__ ).resolve().parents[2] / "build" ) )
sys.path.insert( 0, str( Path( __file__ ).resolve().parents[2] ) )
from ui_icon_atlas import GUTTER, SHEET_WIDTH, cell, pack  # noqa: E402


def gradient( width: int, height: int, seed: int ) -> Image.Image:
	image = Image.new( "RGBA", ( width, height ) )
	for y in range( height ):
		for x in range( width ):
			image.putpixel( ( x, y ), ( ( x * 7 + seed ) % 256, ( y * 13 + seed ) % 256, seed % 256, 64 + ( x + y ) % 128 ) )
	return image


class UiIconAtlasTest( unittest.TestCase ):
	def setUp( self ) -> None:
		self.root = Path( tempfile.mkdtemp() )

	def tearDown( self ) -> None:
		shutil.rmtree( self.root, ignore_errors=True )

	def test_inner_rect_is_byte_exact_and_gutters_repeat_edges( self ) -> None:
		icon = gradient( 9, 5, 3 )
		sheet = cell( icon )
		self.assertEqual( sheet.size, ( 9 + GUTTER * 2, 5 + GUTTER * 2 ) )
		self.assertEqual( sheet.crop( ( GUTTER, GUTTER, GUTTER + 9, GUTTER + 5 ) ).tobytes(), icon.tobytes() )
		for offset in range( GUTTER ):
			self.assertEqual(
				sheet.getpixel( ( offset, GUTTER ) ), icon.getpixel( ( 0, 0 ) ), "left gutter repeats the edge texel"
			)
			self.assertEqual(
				sheet.getpixel( ( GUTTER + 9 + offset, GUTTER + 4 ) ), icon.getpixel( ( 8, 4 ) ), "right gutter"
			)
			self.assertEqual(
				sheet.getpixel( ( GUTTER, offset ) ), icon.getpixel( ( 0, 0 ) ), "top gutter"
			)
			self.assertEqual(
				sheet.getpixel( ( GUTTER + 8, GUTTER + 5 + offset ) ), icon.getpixel( ( 8, 4 ) ), "bottom gutter"
			)

	def test_pack_is_deterministic_and_records_inner_rects( self ) -> None:
		icons = [ ( f"a/{name}.png", gradient( 32, 32, index ) ) for index, name in enumerate( [ "c", "b", "a" ] ) ]
		first_sheets, first_placed = pack( icons )
		second_sheets, second_placed = pack( icons )
		self.assertEqual( [ s.tobytes() for s in first_sheets ], [ s.tobytes() for s in second_sheets ] )
		self.assertEqual( first_placed, second_placed )
		self.assertEqual( sorted( first_placed ), [ "/a/a.png", "/a/b.png", "/a/c.png" ] )
		for name, source in [ ( "a", icons[2][1] ), ( "b", icons[1][1] ), ( "c", icons[0][1] ) ]:
			index, x, y, width, height = first_placed[ f"/a/{name}.png" ]
			self.assertEqual( ( width, height ), ( 32, 32 ) )
			self.assertEqual(
				first_sheets[index].crop( ( x, y, x + width, y + height ) ).tobytes(),
				source.tobytes(),
				f"icon {name} is byte-exact in its cell"
			)
		# The family packs in the given order (c first here); each cell sits
		# its full padded width right of the one before it.
		index_a, x_a, y_a, _, _ = first_placed["/a/a.png"]
		index_c, x_c, y_c, _, _ = first_placed["/a/c.png"]
		self.assertEqual( ( x_c, y_c ), ( GUTTER, GUTTER ) )
		self.assertEqual( y_a, y_c )
		self.assertEqual( x_a, x_c + 2 * ( 32 + GUTTER * 2 ) )

	def test_a_row_that_overflows_the_sheet_width_wraps( self ) -> None:
		wide = gradient( SHEET_WIDTH - GUTTER * 2, 8, 1 )
		icons = [ ( "wide.png", wide ), ( "next.png", gradient( 8, 8, 2 ) ) ]
		sheets, placed = pack( icons )
		self.assertEqual( placed["/wide.png"], ( 0, GUTTER, GUTTER, wide.width, 8 ) )
		index, x, y, width, height = placed["/next.png"]
		# The wide cell's padded height (8 + two gutters) is the row the next
		# icon starts below.
		self.assertEqual( ( index, x, y ), ( 0, GUTTER, GUTTER + 8 + GUTTER * 2 ) )
		self.assertEqual( sheets[0].height, y + height + GUTTER )


if __name__ == "__main__":
	unittest.main()
