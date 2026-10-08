# ===========================================================================
#
# native_texture_mips.py - portable native mip generation (non-Windows hosts)
#
# Windows builds load the 32-bit d3dx9_39.dll through native_lens_resources.ps1
# and reproduce the original mip bytes exactly. Elsewhere this generator writes
# the same NTX1 container (NativeLensResources.cs SerializeTexture): authored
# levels are copied verbatim and only the missing suffix of the mip chain is
# generated (box filter, Pillow BCn encoders). Generated levels are visually
# equivalent, not byte-identical.
#
# Same arguments as the PowerShell helper:
#   -SourceRoot <dir> -OutputRoot <dir>   the eight lens*.ddj -> lens*.texture
#   -Manifest <jobs.json>                 [{ "source", "target" }, ...]
#
# ===========================================================================

import argparse
import json
import struct
from pathlib import Path

from PIL import Image


DDJ_SIGNATURE = b"JMXVDDJ 1000"
DDJ_HEADER_SIZE = 20
DDS_SIGNATURE = b"DDS "
DDS_HEADER_SIZE = 128
DDS_HEIGHT_WIDTH_OFFSET = 12  # u32 height, u32 width
DDS_MIP_COUNT_OFFSET = 28
DDS_PIXEL_FORMAT_OFFSET = 80  # flags, fourCC, bit count, R/G/B/A masks
DDPF_FOURCC = 0x4
DDPF_RGB = 0x40
ARGB_MASKS = (0xFF0000, 0xFF00, 0xFF)
ARGB_BITS = 32
MAX_DDJ_BYTES = 16 * 1024 * 1024

NTX_MAGIC = 0x3158544E
D3DFMT_A8R8G8B8 = 21
D3DFMT_DXT1 = 0x31545844
D3DFMT_DXT3 = 0x33545844
D3DFMT_DXT5 = 0x35545844
PILLOW_BCN = {D3DFMT_DXT1: 1, D3DFMT_DXT3: 2, D3DFMT_DXT5: 3}

BLOCK_SIDE = 4
BLOCK_PIXELS = BLOCK_SIDE * BLOCK_SIDE
DXT1_BLOCK_BYTES = 8
DXT_BLOCK_BYTES = 16
ARGB_PIXEL_BYTES = 4
ALPHA_NIBBLE_MAX = 15
ALPHA_BYTE_MAX = 255

LENS_COUNT = 8


# ================
# level_size
# Bytes of one mip level, matching SerializeTexture's pitch-free rows.
# ================
def level_size(fmt, width, height):
	if fmt == D3DFMT_A8R8G8B8:
		return width * height * ARGB_PIXEL_BYTES
	blocks = ((width + BLOCK_SIDE - 1) // BLOCK_SIDE) * ((height + BLOCK_SIDE - 1) // BLOCK_SIDE)
	return blocks * (DXT1_BLOCK_BYTES if fmt == D3DFMT_DXT1 else DXT_BLOCK_BYTES)


# ================
# read_dds
# Validate the input (as ReadDds does) and the DDS pixel format. Two source
# shapes reach the encoder: DDJ-wrapped textures ("JMXVDDJ 1000" + DDS) and
# the bare DDS payloads embedded in MAPT terrain sectors.
# ================
def read_dds(source):
	# Signature validation first and explicit: a malformed source must fail
	# loudly as a bad input, never reach the encoder as garbage bytes.
	data = Path(source).read_bytes()
	if len(data) > MAX_DDJ_BYTES:
		raise ValueError(f"DDJ/DDS input exceeds size budget: {source}")
	if data.startswith(DDJ_SIGNATURE):
		if len(data) < DDJ_HEADER_SIZE + DDS_HEADER_SIZE:
			raise ValueError(f"Truncated DDJ wrapper: {source}")
		dds = data[DDJ_HEADER_SIZE:]
	elif data.startswith(DDS_SIGNATURE):
		if len(data) < DDS_HEADER_SIZE:
			raise ValueError(f"Truncated DDS header: {source}")
		dds = data
	else:
		raise ValueError(f"Not a DDJ or DDS source (bad signature): {source}")
	if dds[: len(DDS_SIGNATURE)] != DDS_SIGNATURE:
		raise ValueError(f"DDJ wrapper does not contain a DDS payload: {source}")
	if struct.unpack_from("<I", dds, 4)[0] != 124:
		raise ValueError(f"Unsupported DDS header size in {source}")
	height, width = struct.unpack_from("<II", dds, DDS_HEIGHT_WIDTH_OFFSET)
	if width < 1 or height < 1 or width > 8192 or height > 8192:
		raise ValueError(f"DDS dimensions out of range in {source}")
	levels = max(1, struct.unpack_from("<I", dds, DDS_MIP_COUNT_OFFSET)[0])
	flags, fourcc, bits, red, green, blue = struct.unpack_from("<6I", dds, DDS_PIXEL_FORMAT_OFFSET)
	if flags & DDPF_FOURCC and fourcc in PILLOW_BCN:
		fmt = fourcc
	elif flags & DDPF_RGB and bits == ARGB_BITS and (red, green, blue) == ARGB_MASKS:
		fmt = D3DFMT_A8R8G8B8
	else:
		raise ValueError(f"Unsupported native texture format in {source}")
	return dds, width, height, fmt, levels


# ================
# decode
# ================
def decode(fmt, raw, width, height):
	if fmt == D3DFMT_A8R8G8B8:
		return Image.frombytes("RGBA", (width, height), raw, "raw", "BGRA")
	return Image.frombytes("RGBA", (width, height), raw, "bcn", PILLOW_BCN[fmt])


# ================
# encode_dxt3
# Pillow 12.3's BC2 encoder packs the explicit alpha nibbles wrongly, so the
# alpha half of each block is written here and Pillow supplies the BC1 colour
# half. Edge blocks clamp to the last row and column.
# ================
def encode_dxt3(image):
	width, height = image.size
	color = image.convert("RGB").convert("RGBA").tobytes("bcn", PILLOW_BCN[D3DFMT_DXT1])
	alpha = image.getchannel("A").load()
	blocks_x = (width + BLOCK_SIDE - 1) // BLOCK_SIDE
	out = bytearray()
	for block_y in range((height + BLOCK_SIDE - 1) // BLOCK_SIDE):
		for block_x in range(blocks_x):
			nibbles = []
			for y in range(BLOCK_SIDE):
				for x in range(BLOCK_SIDE):
					px = min(block_x * BLOCK_SIDE + x, width - 1)
					py = min(block_y * BLOCK_SIDE + y, height - 1)
					nibbles.append((alpha[px, py] * ALPHA_NIBBLE_MAX + ALPHA_BYTE_MAX // 2) // ALPHA_BYTE_MAX)
			out += bytes(nibbles[i] | (nibbles[i + 1] << 4) for i in range(0, BLOCK_PIXELS, 2))
			start = (block_y * blocks_x + block_x) * DXT1_BLOCK_BYTES
			out += color[start:start + DXT1_BLOCK_BYTES]
	return bytes(out)


# ================
# encode
# ================
def encode(fmt, image):
	if fmt == D3DFMT_A8R8G8B8:
		return image.tobytes("raw", "BGRA")
	if fmt == D3DFMT_DXT3:
		return encode_dxt3(image)
	return image.tobytes("bcn", PILLOW_BCN[fmt])


# ================
# build_file
# Full chain down to 1x1: authored levels verbatim, then each missing level
# box-filtered from the one above it.
# ================
def build_file(source, target):
	dds, width, height, fmt, authored = read_dds(source)
	full = max(width, height).bit_length()
	authored = min(authored, full)
	out = bytearray(struct.pack("<5I", NTX_MAGIC, width, height, fmt, full))
	offset = DDS_HEADER_SIZE
	level_width, level_height = width, height
	previous = None
	for level in range(full):
		size = level_size(fmt, level_width, level_height)
		if level < authored:
			raw = dds[offset:offset + size]
			if len(raw) != size:
				raise ValueError(f"Truncated authored mip {level} in {source}")
			offset += size
			if level == authored - 1:
				previous = decode(fmt, raw, level_width, level_height)
		else:
			previous = previous.resize((level_width, level_height), Image.BOX)
			raw = encode(fmt, previous)
			if len(raw) != size:
				raise ValueError(f"Encoder wrote {len(raw)} bytes for {level_width}x{level_height}, expected {size}")
		out += raw
		level_width, level_height = max(1, level_width // 2), max(1, level_height // 2)
	Path(target).parent.mkdir(parents=True, exist_ok=True)
	Path(target).write_bytes(bytes(out))


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description="Portable NTX1 mip generation (native_lens_resources.ps1 arguments).")
	parser.add_argument("-SourceRoot")
	parser.add_argument("-OutputRoot")
	parser.add_argument("-Manifest")
	options = parser.parse_args()
	if options.Manifest:
		jobs = json.loads(Path(options.Manifest).read_text(encoding="utf-8"))
		pairs = [(job["source"], job["target"]) for job in jobs]
	elif options.SourceRoot and options.OutputRoot:
		pairs = [
			(Path(options.SourceRoot) / f"lens{index}.ddj", Path(options.OutputRoot) / f"lens{index}.texture")
			for index in range(1, LENS_COUNT + 1)
		]
	else:
		parser.error("supply -SourceRoot and -OutputRoot, or -Manifest")
	for source, target in pairs:
		build_file(source, target)


if __name__ == "__main__":
	main()
