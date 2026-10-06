"""
===========================================================================

water_bump.py - native 87E020 blue-channel to signed DuDv conversion

PNG stores signed V8U8 bytes biased by 128. The shader restores signed
normalized values after filtering. Source neighbors wrap on both axes.

===========================================================================
"""
import sys
from PIL import Image


# ================
# convert
# ================
def convert(source, destination):
	image = Image.open(source).convert("RGBA")
	width, height = image.size
	pixels = image.load()
	output = Image.new("RGBA", image.size)
	result = output.load()
	for y in range(height):
		for x in range(width):
			u = int((pixels[(x - 1) % width, y][2] - pixels[(x + 1) % width, y][2]) / 2)
			v = int((pixels[x, (y - 1) % height][2] - pixels[x, (y + 1) % height][2]) / 2)
			result[x, y] = (u + 128, v + 128, 0, 255)
	output.save(destination)


if __name__ == "__main__":
	convert(sys.argv[1], sys.argv[2])
