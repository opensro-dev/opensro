#
# ===========================================================================
#
# encode_icons.py - the catalogue's web icons: PNG in, WebP out
#
# The client's published icons are PNG (converted from the media's DDJ);
# the website wants WebP at the icon's native size. Reads a JSON list of
# [source_png, target_webp] pairs from argv[1] and writes each target,
# lossless, so the icon matches the game pixel for pixel.
#
# ===========================================================================
#
import json, os, sys

from PIL import Image

def main():
	with open( sys.argv[1], encoding="utf-8" ) as listing:
		pairs = json.load( listing )
	written = 0
	for source, target in pairs:
		os.makedirs( os.path.dirname( target ), exist_ok=True )
		with Image.open( source ) as image:
			image.save( target, "WEBP", lossless=True, method=4 )
		written += 1
	print( json.dumps( { "written": written } ) )

if __name__ == "__main__":
	main()
