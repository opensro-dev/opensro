#
# ===========================================================================
# pet_item_icons.py - export owned pet gear artwork to native 32px item icons.
#
# Each 3x3 atlas covers degrees 1–9 in row order. Degree 10 has separate art.
# The normal publisher owns all generated outputs.
# ===========================================================================
#
import base64
import io
import json
from pathlib import Path

from PIL import Image


#
# ================
# encode_icon
# ================
#
def encode_icon(source, slot, degree):
	icon = source.resize((32, 32), Image.Resampling.LANCZOS)
	output = io.BytesIO()
	icon.save(output, format="PNG")
	return {"slot": slot, "degree": degree, "png": base64.b64encode(output.getvalue()).decode("ascii")}


#
# ================
# export_icons
# ================
#
def export_icons():
	icons = []
	sources = Path(__file__).resolve().parents[3] / "docs/pet-expansion/assets"
	for slot in ("collar", "harness", "charm"):
		with Image.open(sources / f"pet-gear-{slot}.png") as source:
			atlas = source.convert("RGBA")
			width, height = atlas.size
			if width != height or width < 96:
				raise ValueError(f"Pet {slot} icons need a square 3x3 source atlas")
			for degree in range(1, 10):
				row, col = divmod(degree - 1, 3)
				bounds = (round(col * width / 3), round(row * height / 3),
					round((col + 1) * width / 3), round((row + 1) * height / 3))
				icons.append(encode_icon(atlas.crop(bounds), slot, degree))
		with Image.open(sources / f"pet-gear-{slot}-10.png") as source:
			icons.append(encode_icon(source.convert("RGBA"), slot, 10))
	return icons


if __name__ == "__main__":
	print(json.dumps(export_icons()))
