#
# ===========================================================================
#
# encode_icons.py - the catalogue's web icons: PNG in, WebP out
#
# The client's published icons are PNG (converted from the media's DDJ);
# the website wants WebP at the icon's native size. Reads a JSON list of
# [source_png, target_webp] pairs from argv[1] and writes each target,
# lossless, so the icon matches the game pixel for pixel. argv[2], when
# given, is the content cache (webp_pool.py).
#
# ===========================================================================
#
import io, json, sys

from PIL import Image

from webp_pool import encode_pairs

# ICON_SETTINGS names the encode for the cache: a change here re-encodes.
ICON_SETTINGS = "icon lossless method=4"

#
# ================
# encode_icon
# ================
#
def encode_icon( payload, target ):
	with Image.open( io.BytesIO( payload ) ) as image:
		image.save( target, "WEBP", lossless=True, method=4 )

#
# ================
# main
# ================
#
def main():
	with open( sys.argv[1], encoding="utf-8" ) as listing:
		pairs = json.load( listing )
	cache = sys.argv[2] if len( sys.argv ) > 2 else None
	print( json.dumps( encode_pairs( pairs, encode_icon, ICON_SETTINGS, cache ) ) )

if __name__ == "__main__":
	main()
