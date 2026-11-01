# ===========================================================================
#
# ui_icon_atlas.py - the UI icon family as one edge-padded atlas sheet
#
# Port-only, not native. Every published interface icon
# (assets/images/Media_extracted/icon/**, 2.6k PNGs of 2.7 Mpx) packs into
# one RGBA sheet beside its per-icon files: the interface's image owner
# then binds one texture for the whole family instead of one per icon,
# and the quad packer remaps each icon's uv into its cell (device/ui.ts;
# the sheet's manifest is foundation/ui/icon-atlas.ts's).
#
# Each cell carries a two-texel gutter that repeats the icon's own edge
# texels, so a linearly filtered sample at the icon's edge blends the same
# texel it blended as a standalone texture: the sheet's frames are
# byte-identical to the per-icon frames. The pack is a deterministic
# shelf layout in path order; a family that outgrows the sheet's height
# starts the next sheet.
#
# The per-icon files stay published: an older client reads them, and a
# client without the manifest falls back to them unchanged.
#
#	node-style wrapper: scripts/build/uiIconAtlas.mjs (resourceBuild step)
#	python scripts/build/ui_icon_atlas.py --out DIR   (a private build tree)
#
# ===========================================================================

import argparse
import json
import sys
from pathlib import Path

from PIL import Image

sys.path.insert( 0, str( Path( __file__ ).resolve().parents[1] ) )
from sro_paths import GENERATED_ROOT  # noqa: E402

ICON_ROOT = "assets/images/Media_extracted/icon"
ATLAS_ROOT = "assets/ui-atlas"
SHEET_WIDTH = 2048
SHEET_HEIGHT_LIMIT = 4096
# A linear tap at the icon's edge reaches one texel outside; two repeated
# texels hold it and the shader's area sampler (AREA_TAPS 3) besides.
GUTTER = 2
MANIFEST_VERSION = 1


# ================
# cell
# The icon with its own edge texels repeated around it.
# ================
def cell( icon: Image.Image ) -> Image.Image:
	width, height = icon.size
	out = Image.new( "RGBA", ( width + GUTTER * 2, height + GUTTER * 2 ), ( 0, 0, 0, 0 ) )
	out.paste( icon, ( GUTTER, GUTTER ) )
	nearest = Image.NEAREST
	out.paste( icon.crop( ( 0, 0, 1, height ) ).resize( ( GUTTER, height ), nearest ), ( 0, GUTTER ) )
	out.paste(
		icon.crop( ( width - 1, 0, width, height ) ).resize( ( GUTTER, height ), nearest ),
		( GUTTER + width, GUTTER )
	)
	out.paste( icon.crop( ( 0, 0, width, 1 ) ).resize( ( width, GUTTER ), nearest ), ( GUTTER, 0 ) )
	out.paste(
		icon.crop( ( 0, height - 1, width, height ) ).resize( ( width, GUTTER ), nearest ),
		( GUTTER, GUTTER + height )
	)
	out.paste( icon.crop( ( 0, 0, 1, 1 ) ).resize( ( GUTTER, GUTTER ), nearest ), ( 0, 0 ) )
	out.paste(
		icon.crop( ( width - 1, 0, width, 1 ) ).resize( ( GUTTER, GUTTER ), nearest ), ( GUTTER + width, 0 )
	)
	out.paste(
		icon.crop( ( 0, height - 1, 1, height ) ).resize( ( GUTTER, GUTTER ), nearest ), ( 0, GUTTER + height )
	)
	out.paste(
		icon.crop( ( width - 1, height - 1, width, height ) ).resize( ( GUTTER, GUTTER ), nearest ),
		( GUTTER + width, GUTTER + height )
	)
	return out


# ================
# pack
# The shelf layout: rows of cells in path order. Each sheet is composed at
# its height limit and cropped to what it used, so a cell never pastes
# outside its sheet. Returns the sheet images and each icon's inner rect
# in sheet pixels.
# ================
def pack( icons: list[tuple[str, Image.Image]] ):
	sheets: list[Image.Image] = []
	placed: dict[str, tuple[int, int, int, int, int]] = {}
	canvas: Image.Image | None = None
	x = 0
	y = 0
	row_height = 0
	for path, icon in icons:
		padded = cell( icon )
		if x + padded.width > SHEET_WIDTH:
			x = 0
			y += row_height
			row_height = 0
		if canvas is None or y + padded.height > SHEET_HEIGHT_LIMIT:
			if canvas is not None:
				sheets.append( canvas.crop( ( 0, 0, SHEET_WIDTH, y + row_height ) ) )
			canvas = Image.new( "RGBA", ( SHEET_WIDTH, SHEET_HEIGHT_LIMIT ), ( 0, 0, 0, 0 ) )
			x = 0
			y = 0
			row_height = 0
		canvas.paste( padded, ( x, y ) )
		placed["/" + path] = ( len( sheets ), x + GUTTER, y + GUTTER, icon.width, icon.height )
		x += padded.width
		row_height = max( row_height, padded.height )
	if canvas is not None:
		sheets.append( canvas.crop( ( 0, 0, SHEET_WIDTH, y + row_height ) ) )
	return sheets, placed


# ================
# build
# ================
def build( public_root: Path, out_root: Path ) -> dict:
	icon_root = public_root / ICON_ROOT
	paths = sorted( p for p in icon_root.rglob( "*.png" ) )
	if not paths:
		raise SystemExit( f"no icons below {icon_root}" )
	icons = []
	for path in paths:
		image = Image.open( path ).convert( "RGBA" )
		if image.width > SHEET_WIDTH - GUTTER * 2 or image.height > SHEET_HEIGHT_LIMIT - GUTTER * 2:
			raise SystemExit( f"{path.name} ({image.width}x{image.height}) cannot fit a sheet" )
		icons.append( ( path.relative_to( public_root ).as_posix(), image ) )
	sheets, placed = pack( icons )
	atlas_root = out_root / ATLAS_ROOT
	atlas_root.mkdir( parents=True, exist_ok=True )
	sheet_paths = []
	sheet_sizes = []
	for index, sheet in enumerate( sheets ):
		name = f"sheet-{index}.png"
		sheet.save( atlas_root / name, optimize=True )
		sheet_paths.append( f"/{ATLAS_ROOT}/{name}" )
		sheet_sizes.append( [ sheet.width, sheet.height ] )
	manifest = {
		"version": MANIFEST_VERSION,
		"sheets": sheet_paths,
		# The uv rect of an icon divides its pixel rect by its sheet's size,
		# so the manifest carries each sheet's dimensions beside its path.
		"sheetSize": sheet_sizes,
		"icons": placed
	}
	( atlas_root / "manifest.json" ).write_text( json.dumps( manifest, sort_keys=True ), encoding="utf-8" )
	dimensions = ", ".join( f"{sheet.width}x{sheet.height}" for sheet in sheets )
	print( f"ui icon atlas: {len( icons )} icons, {len( sheets )} sheet(s) [{dimensions}]" )
	return manifest


# ================
# main
# ================
def main() -> None:
	parser = argparse.ArgumentParser( description=__doc__ )
	parser.add_argument( "--out", type=Path, default=None, help="build below DIR instead of the shared tree" )
	arguments = parser.parse_args()
	public_root = GENERATED_ROOT / "client-public"
	out_root = arguments.out if arguments.out else public_root
	build( public_root, out_root )


if __name__ == "__main__":
	main()
