#
# ===========================================================================
# pet_item_icons.py - export the owned 3x3 pet icon sheets at native32x32 size
#
# Mechanical sprite extraction only: source art remains editable at full size.
# No color variants or degree labels are synthesized from a shared icon.
# ===========================================================================
#
import base64
import io
import json
from pathlib import Path

from PIL import Image, ImageDraw

SOURCE = Path(__file__).resolve().parent / "pet-item-icons"


#
# ================
# export_icons
# ================
#
def export_icons(source=SOURCE):
	icons = []
	for slot in ("collar", "harness", "charm"):
		with Image.open(source / (slot + "-degrees.png")) as image:
			if image.width < 768 or image.height < 768 or image.width != image.height:
				raise ValueError("Pet icon source must be a square3x3 sheet, at least768px")
			for index in range(9):
				x, y = index % 3, index // 3
				box = (round(x * image.width / 3), round(y * image.height / 3), round((x + 1) * image.width / 3), round((y + 1) * image.height / 3))
				icon = image.crop(box).convert("RGBA").resize((32, 32), Image.Resampling.LANCZOS)
				ImageDraw.Draw(icon).rectangle((0, 0, 31, 31), outline=(51, 61, 77, 255))
				output = io.BytesIO()
				icon.save(output, format="PNG")
				icons.append({"slot": slot, "degree": index + 1, "png": base64.b64encode(output.getvalue()).decode("ascii")})
	return icons


if __name__ == "__main__":
	print(json.dumps(export_icons()))
