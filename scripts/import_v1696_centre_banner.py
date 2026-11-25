"""
===========================================================================

import_v1696_centre_banner.py - the centre banner's plate art (#3260)

The later client shows a reinforcement success in its centre banner,
the violet com_center plate family under interface\\ifcommon, where
v1.150 has neither the banner nor the art. This importer reads the
operator's pinned v1.696 Media.pk2 (SRO_V1696_ROOT, pins in
v1696_archive_pins.json) and publishes the three PNGs beside the
v1.150 notice plates. No retail art is committed.

===========================================================================
"""
import argparse
import mmap
import tempfile
from pathlib import Path

import sro_pk2
from convert_images import extract_ddj_payload, save_image_payload
from import_vsro_alexandria import archive_index
from pin_file import require_pinned_archive
from sro_paths import PUBLIC_ROOT

V1696_ENV = "SRO_V1696_ROOT"
PIN_FILE = Path( __file__ ).with_name( "v1696_archive_pins.json" )
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
# The centre banner's three-piece plate, the same corner/edge/edge2
# stack the notice and warning families use.
SOURCES = (
	"interface/ifcommon/com_center_corner.ddj",
	"interface/ifcommon/com_center_edge.ddj",
	"interface/ifcommon/com_center_edge2.ddj",
)
PUBLIC_DIRECTORY = "assets/images/Media_extracted/interface/ifcommon"


# ================
# verify_media
# ================
def verify_media( root ):
	return require_pinned_archive( PIN_FILE, PIN_FORMAT, root, "Media", f"{V1696_ENV} must be the pinned v1.696 client folder" )


# ================
# publish
# ================
def publish( root ):
	media = verify_media( root )
	with media.open( "rb" ) as handle, tempfile.TemporaryDirectory( prefix="sro-centre-banner-" ) as scratch_dir:
		data = mmap.mmap( handle.fileno(), 0, access=mmap.ACCESS_READ )
		try:
			index = archive_index( data )
			target_dir = PUBLIC_ROOT / PUBLIC_DIRECTORY
			target_dir.mkdir( parents=True, exist_ok=True )
			for source in SOURCES:
				entry = index.get( source.lower().replace( "\\", "/" ) )
				if entry is None:
					# Some indices keep backslashes.
					entry = index.get( source.lower() )
				if entry is None:
					raise SystemExit( f"Media.pk2 has no {source}" )
				scratch = Path( scratch_dir ) / Path( source ).name
				scratch.write_bytes( sro_pk2.payload( data, entry ) )
				payload = extract_ddj_payload( scratch )
				target = target_dir / (Path( source ).stem + ".png")
				save_image_payload( payload, target, source_for_fallback=scratch )
				print( f"centre banner: /{PUBLIC_DIRECTORY}/{target.name}" )
		finally:
			data.close()


# ================
# main
# ================
def main( argv=None ):
	parser = argparse.ArgumentParser( description=__doc__ )
	parser.add_argument( "--v1696-root", required=True, help=f"Pinned v1.696 client folder ({V1696_ENV})" )
	args = parser.parse_args( argv )
	publish( args.v1696_root )


if __name__ == "__main__":
	main()
