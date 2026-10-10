#
# ===========================================================================
#
# encode_tiles.py - the catalogue's atlas tiles: minimap art in, WebP out
#
# The client publishes each region's minimap either as a PNG or as an NTX1
# texture: a 20-byte header (magic "NTX1", u32 width, u32 height, fourcc,
# u32 mip count) followed by the top mip's block-compressed data. Pillow
# reads block compression through its DDS decoder, so an NTX1 tile is
# rewrapped as a DDS of the same fourcc. Reads a JSON list of [source,
# target_webp] pairs from argv[1]; tiles are photographic, so they are
# written lossy at TILE_QUALITY.
#
# ===========================================================================
#
import io, json, os, struct, sys

from PIL import Image

NTX_MAGIC = b"NTX1"
NTX_HEADER_BYTES = 20
TILE_QUALITY = 82
DDS_HEADER_BYTES = 124
DDSD_CAPS_HEIGHT_WIDTH_PIXELFORMAT_LINEARSIZE = 0x1 | 0x2 | 0x4 | 0x1000 | 0x80000
DDPF_FOURCC = 0x4
DDS_PIXELFORMAT_BYTES = 32
DDSCAPS_TEXTURE = 0x1000
BLOCK_BYTES = { b"DXT1": 8, b"DXT3": 16, b"DXT5": 16 }

#
# ================
# ntx_image
#
# The NTX1 texture's top mip as a Pillow image, through a DDS wrapper.
# ================
#
def ntx_image( payload ):
	if payload[:4] != NTX_MAGIC:
		raise ValueError( "not an NTX1 texture" )
	width, height, fourcc = struct.unpack_from( "<II4s", payload, 4 )
	block = BLOCK_BYTES.get( fourcc )
	if block is None:
		raise ValueError( "unsupported NTX1 format %r" % fourcc )
	size = max( 1, ( width + 3 ) // 4 ) * max( 1, ( height + 3 ) // 4 ) * block
	data = payload[NTX_HEADER_BYTES:NTX_HEADER_BYTES + size]
	if len( data ) != size:
		raise ValueError( "truncated NTX1 texture" )
	pixel_format = struct.pack( "<II4s5I", DDS_PIXELFORMAT_BYTES, DDPF_FOURCC, fourcc, 0, 0, 0, 0, 0 )
	header = struct.pack( "<7I44x", DDS_HEADER_BYTES, DDSD_CAPS_HEIGHT_WIDTH_PIXELFORMAT_LINEARSIZE, height, width, size, 0, 1 )
	header += pixel_format + struct.pack( "<5I", DDSCAPS_TEXTURE, 0, 0, 0, 0 )
	image = Image.open( io.BytesIO( b"DDS " + header + data ) )
	image.load()
	return image

#
# ================
# main
# ================
#
def main():
	with open( sys.argv[1], encoding="utf-8" ) as listing:
		pairs = json.load( listing )
	written = 0
	for source, target in pairs:
		os.makedirs( os.path.dirname( target ), exist_ok=True )
		with open( source, "rb" ) as tile:
			payload = tile.read()
		image = ntx_image( payload ) if payload[:4] == NTX_MAGIC else Image.open( io.BytesIO( payload ) )
		image.convert( "RGB" ).save( target, "WEBP", quality=TILE_QUALITY, method=4 )
		written += 1
	print( json.dumps( { "written": written } ) )

if __name__ == "__main__":
	main()
