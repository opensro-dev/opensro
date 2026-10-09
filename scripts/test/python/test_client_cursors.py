"""
===========================================================================

test_client_cursors.py - native cursor DIBs become browser PNGs

A synthetic 8-bpp cursor DIB (palette, bottom-up colour rows, AND mask)
must decode to the right pixels with the masked ones transparent, and the
PNG written for the browser must carry exactly those pixels (BUG-068).

===========================================================================
"""

import pathlib
import struct
import sys
import unittest
import zlib

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
from extract_client_cursors import cursor_png, dib_rgba

RED, BLUE = (255, 0, 0), (0, 0, 255)


# ================
# cursor_dib
#
# A 2x2 8-bpp cursor: top row red, bottom row blue, the top-right pixel
# masked out. Rows are stored bottom-up and padded to 4 bytes.
# ================
def cursor_dib() -> bytes:
	width, height = 2, 2
	header = struct.pack("<IiiHHIIiiII", 40, width, height * 2, 1, 8, 0, 0, 0, 0, 2, 0)
	palette = bytes((RED[2], RED[1], RED[0], 0)) + bytes((BLUE[2], BLUE[1], BLUE[0], 0))
	colour = bytes((1, 1, 0, 0)) + bytes((0, 0, 0, 0))
	mask = bytes((0x00, 0, 0, 0)) + bytes((0x40, 0, 0, 0))
	return header + palette + colour + mask


class ClientCursorTests(unittest.TestCase):

	# ================
	# test_mask_and_palette_decode_top_down
	# ================
	def test_mask_and_palette_decode_top_down(self):
		width, height, rgba = dib_rgba(cursor_dib())
		self.assertEqual((width, height), (2, 2))
		pixels = [tuple(rgba[i:i + 4]) for i in range(0, len(rgba), 4)]
		self.assertEqual(pixels, [RED + (255,), RED + (0,), BLUE + (255,), BLUE + (255,)])

	# ================
	# test_png_carries_the_decoded_pixels
	# ================
	def test_png_carries_the_decoded_pixels(self):
		png = cursor_png(cursor_dib())
		self.assertEqual(png[:8], bytes([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A]))
		width, height, depth, colour_type = struct.unpack(">IIBB", png[16:26])
		self.assertEqual((width, height, depth, colour_type), (2, 2, 8, 6))
		length = struct.unpack(">I", png[33:37])[0]
		self.assertEqual(png[37:41], b"IDAT")
		raw = zlib.decompress(png[41:41 + length])
		rows = [raw[1 + y * 9:1 + y * 9 + 8] for y in range(2)]
		self.assertEqual(b"".join(rows), dib_rgba(cursor_dib())[2])


if __name__ == "__main__":
	unittest.main()
