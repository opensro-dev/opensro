"""
===========================================================================

test_native_texture_mips.py - the portable mip generator keeps colour under alpha 0

The generated small levels of a texture whose artist left alpha at 0 over valid
colour must keep that colour, as the D3DX chain does, and the alpha channel
must stay as authored. Premultiplied resizing turned such texels black.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import struct
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image

import native_texture_mips as mips  # noqa: E402

SIDE = 16
COLOUR = (200, 100, 50)
COLOUR_TOLERANCE = 12
DDS_FLAGS = 0x1 | 0x2 | 0x4 | 0x1000
DDPF_FOURCC = 0x4


# ================
# ddj_bytes
#
# A DDJ wrapper around a one-level DDS of the given RGBA image in DXT3.
# ================
def ddj_bytes(image):
	header = bytearray(mips.DDS_HEADER_SIZE)
	header[0:4] = mips.DDS_SIGNATURE
	struct.pack_into("<II", header, 4, 124, DDS_FLAGS)
	struct.pack_into("<II", header, mips.DDS_HEIGHT_WIDTH_OFFSET, image.height, image.width)
	struct.pack_into("<I", header, mips.DDS_MIP_COUNT_OFFSET, 0)
	struct.pack_into("<I", header, 76, 32)
	struct.pack_into("<II", header, mips.DDS_PIXEL_FORMAT_OFFSET, mips.DDPF_FOURCC, mips.D3DFMT_DXT3)
	return mips.DDJ_SIGNATURE + bytes(8) + bytes(header) + mips.encode(mips.D3DFMT_DXT3, image)


# ================
# generated_levels
# ================
def generated_levels(image):
	with tempfile.TemporaryDirectory() as directory:
		source, target = Path(directory) / "t.ddj", Path(directory) / "t.texture"
		source.write_bytes(ddj_bytes(image))
		mips.build_file(source, target)
		data = target.read_bytes()
	_, width, height, fmt, count = struct.unpack_from("<5I", data)
	levels, offset = [], 20
	for _ in range(count):
		size = mips.level_size(fmt, width, height)
		levels.append(mips.decode(fmt, data[offset:offset + size], width, height))
		offset += size
		width, height = max(1, width // 2), max(1, height // 2)
	return levels


class MipTests(unittest.TestCase):
	# ================
	# test_colour_under_alpha_zero_survives_every_small_level
	# ================
	def test_colour_under_alpha_zero_survives_every_small_level(self):
		levels = generated_levels(Image.new("RGBA", (SIDE, SIDE), (*COLOUR, 0)))
		self.assertGreater(len(levels), 3)
		for level in levels[1:-1]:
			pixel = level.getpixel((0, 0))
			for got, want in zip(pixel[:3], COLOUR):
				self.assertAlmostEqual(got, want, delta=COLOUR_TOLERANCE)
			self.assertEqual(pixel[3], 0)

	# ================
	# test_opaque_texels_keep_their_colour_and_alpha
	# ================
	def test_opaque_texels_keep_their_colour_and_alpha(self):
		levels = generated_levels(Image.new("RGBA", (SIDE, SIDE), (*COLOUR, 255)))
		pixel = levels[1].getpixel((0, 0))
		for got, want in zip(pixel[:3], COLOUR):
			self.assertAlmostEqual(got, want, delta=COLOUR_TOLERANCE)
		self.assertEqual(pixel[3], 255)

	# ================
	# test_authored_level_is_copied_verbatim
	# ================
	def test_authored_level_is_copied_verbatim(self):
		image = Image.new("RGBA", (SIDE, SIDE), (*COLOUR, 0))
		self.assertEqual(generated_levels(image)[0].tobytes(), mips.decode(
			mips.D3DFMT_DXT3, mips.encode(mips.D3DFMT_DXT3, image), SIDE, SIDE).tobytes())


if __name__ == "__main__":
	unittest.main()
