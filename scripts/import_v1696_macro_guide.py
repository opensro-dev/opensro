"""
===========================================================================

import_v1696_macro_guide.py - CIFMacroSystemGuide's icon (#3907)

The v1.696 under-minimap macro button (CIFMacroSystemGuide at 0xab8fe0)
loads icon\\etc\\mecro_0.ddj. v1.150 has neither the class nor the art.
This importer reads the operator's pinned v1.696 Media.pk2
(SRO_V1696_ROOT, pins in v1696_archive_pins.json) and publishes the PNG
beside the other Media_extracted icons. No retail art is committed.

===========================================================================
"""
import argparse
import mmap
from pathlib import Path

import sro_pk2
from convert_images import extract_ddj_payload, save_image_payload
from import_vsro_alexandria import archive_index
from pin_file import require_pinned_archive
from sro_paths import PUBLIC_ROOT

V1696_ENV = "SRO_V1696_ROOT"
PIN_FILE = Path( __file__ ).with_name( "v1696_archive_pins.json" )
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
# OnCreate at 0xab8fe0 pushes this one path; no mecro_1 exists in Media.pk2.
SOURCE = "icon/etc/mecro_0.ddj"
PUBLIC_RELATIVE = "assets/images/Media_extracted/icon/etc/mecro_0.png"
PUBLIC = PUBLIC_ROOT / PUBLIC_RELATIVE
PUBLIC_PATH = "/" + PUBLIC_RELATIVE.replace( "\\", "/" )


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
	with media.open( "rb" ) as handle, tempfile_ddj() as scratch:
		data = mmap.mmap( handle.fileno(), 0, access=mmap.ACCESS_READ )
		try:
			index = archive_index( data )
			entry = index.get( SOURCE.lower().replace( "\\", "/" ) )
			if entry is None:
				# Some indices keep backslashes.
				entry = index.get( SOURCE.lower() )
			if entry is None:
				raise SystemExit( f"Media.pk2 has no {SOURCE}" )
			scratch.write_bytes( sro_pk2.payload( data, entry ) )
			payload = extract_ddj_payload( scratch )
			PUBLIC.parent.mkdir( parents=True, exist_ok=True )
			save_image_payload( payload, PUBLIC, source_for_fallback=scratch )
		finally:
			data.close()
	print( f"macro guide icon: {PUBLIC_PATH}" )


# ================
# tempfile_ddj
# ================
class tempfile_ddj:
	def __enter__( self ):
		import tempfile
		self._dir = tempfile.TemporaryDirectory( prefix="sro-macro-guide-" )
		self.path = Path( self._dir.name ) / "mecro_0.ddj"
		return self.path

	def __exit__( self, *exc ):
		self._dir.cleanup()


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
