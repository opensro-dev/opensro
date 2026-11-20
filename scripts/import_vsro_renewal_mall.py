"""
===========================================================================

import_vsro_renewal_mall.py - the later client's renewal Item Mall window

The v1.150 archives ship the classic mall (resinfo/ifitemmall*.txt); the
pinned VSRO client also ships the renewal mall, CIFNewItemMall: its
layouts (resinfo/ifnewitemmall*.txt) and its art (interface/mall,
interface/frame/mall_*). This tool publishes the layouts' controls
(name, ID, class, Rect, texture, caption, font and alignment) and the
English of their caption keys as assets/data/renewal-mall.json, and every picture they or the window
chrome name as PNG under Media_extracted/later/, a folder of its own
so no v1.150 picture of the same name is ever replaced.

A control whose Rect carries a zero width or height takes its picture's
own size, as CIFStatic and CIFButton do for a texture-sized control. The
Rect is the authored position; ClientRect is the text inset.

Not in v1.150: once published, this window is the client's desktop Item
Mall (owner 2026-11-20); without it the classic window stands. The build
runs this step when the later client is configured
(laterClientImports.mjs). No retail art is committed to Git.

===========================================================================
"""
import argparse
import json
import re
import shutil
import struct
import tempfile
from pathlib import Path

import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import decode_table, verify_vsro_archive
from import_vsro_tomb_minimap import ddj_payload
from sro_paths import PUBLIC_ROOT

CATALOG_FORMAT = "sro-renewal-mall-later"
CATALOG_VERSION = 1
CATALOG_NAME = "renewal-mall.json"
IMAGE_PUBLIC_ROOT = "/assets/images/Media_extracted/later/"

# Each published section: the layout file and the Sections it reads, merged
# in order. A goods card's icon slot is created in its own section
# (NewShopSlotIconForPackage), beside the card's Create controls.
LAYOUT_SECTIONS = {
	"window": ("resinfo/ifnewitemmall.txt", ("Create",)),
	# The window's second section places the CIFNewItemMallMyInfo child.
	"windowInfo": ("resinfo/ifnewitemmall.txt", ("MyInfo",)),
	"myInfo": ("resinfo/ifnewitemmallmyinfo.txt", ("Create",)),
	"itemSlot": ("resinfo/ifnewitemmallshopitemslot.txt", ("Create", "NewShopSlotIconForPackage")),
	"mainSlot": ("resinfo/ifnewitemmallshopmainslot.txt", ("Create",)),
	"subSlot": ("resinfo/ifnewitemmallshopsubslot.txt", ("Create",)),
}

# Window art the layouts do not name but the window draws: the outer
# frame and its title piece, the goods card, the rail and sub-category
# tabs, the rail icons, the goods badges and the page arrows. INFERENCE:
# no v1.188 client binary is at hand, so these are the interface/mall
# pictures whose sizes fit the authored slots (a 200x84 card holding the
# 45x45 icon slot, 116x32 rail entries holding the 20x20 icon, 96x24
# sub tabs holding the 93x15 title).
CHROME_PICTURES = (
	"interface/frame/mall_wnd_",
	"interface/frame/mall_wnd_wnd_mid_up.ddj",
	"interface/mall/mall_item_wnd.ddj",
	"interface/mall/mall_item_wnd_focus.ddj",
	"interface/mall/mall_item_tab_on.ddj",
	"interface/mall/mall_item_tab_off.ddj",
	"interface/mall/mall_sub_tab_on.ddj",
	"interface/mall/mall_sub_tab_off.ddj",
	"interface/mall/mall_item_sub_button.ddj",
	"interface/mall/mall_page_next.ddj",
	"interface/mall/mall_page_prev.ddj",
	"interface/mall/mall_icon_new.ddj",
	"interface/mall/mall_icon_popular.ddj",
	"interface/mall/mall_icon_recommendation .ddj",
	"interface/mall/mall_icon_sale.ddj",
	"interface/mall/mall_icon_limited.ddj",
	"interface/mall/mall_consum_icon.ddj",
	"interface/mall/mall_avatar_icon.ddj",
	"interface/mall/mall_pet_icon.ddj",
	"interface/mall/mall_premium_icon.ddj",
	"interface/mall/mall_alchemy_icon.ddj",
	"interface/mall/mall_hot_icon.ddj",
	"interface/mall/mall_choicelist_icon.ddj",
)

TEXT_TABLE = "server_dep/silkroad/textdata/textuisystem.txt"
# Native 791F50 selects English column 8 (textResources.mjs buildTextCatalog).
ENGLISH_COLUMN = 8

# Caption keys the window draws that no layout control names: the title,
# the rail's categories, the card popup and the empty-page notes
# (runtime/ui/hud/renewal-mall-view.ts).
VIEW_KEYS = (
	"UIIT_STT_SILKMALL_TITLE",
	"UIIT_STT_SILKMALL_CONSUMPTION",
	"UIIT_STT_SILKMALL_AVATA",
	"UIIT_STT_SILKMALL_PET",
	"UIIT_STT_SILKMALL_PREMIUM",
	"UIIT_STT_SILKMALL_ARCHEMY",
	"UIIT_STT_SILKMALL_HOT_STORE",
	"UIIT_STT_SILKMALL_SHOPPING_BASKET_LIST",
	"UIIT_STT_BUY",
	"UIIT_STT_SILKMALL_PUT_ON",
	"UIIT_STT_SILKMALL_SHOPPING_BASKET",
	"UIIT_STT_SILKMALL_DEL",
	"UIIT_STT_SILKMALL_ITEM_GIFTSILK",
	"UIIT_STT_SILKMALL_MAIN",
	"UIIT_MSG_SILKMALL_NO_ITEM",
)

FRAME_PARTS = (
	"left_up", "right_up", "right_down", "left_down",
	"left_side", "mid_up", "right_side", "mid_down",
)
BUTTON_STATES = ("", "_focus", "_press", "_disable")
NODE = re.compile(r"^([A-Za-z0-9_]+):([A-Za-z0-9_]+)$")
PROPERTY = re.compile(r'^([A-Za-z0-9_]+)=([A-Z]+),"(.*)"$')
SECTION = re.compile(r'^Section\s*=\s*([^,]+),')
DDS_SIZE = struct.Struct("<II")


def slash(path):
	"""One spelling for a DDJ path: the files mix "\\" and "\\\\"."""
	return re.sub(r"[\\/]+", "/", path).lower()


def public_path(ddj):
	"""The published PNG path of a DDJ (or a frame prefix) under later/."""
	name = slash(ddj)
	if name.endswith(".ddj"):
		name = name[:-4] + ".png"
	return IMAGE_PUBLIC_ROOT + name


def _ints(value, count):
	parts = [part.strip() for part in value.split(",")]
	if len(parts) != count:
		raise ValueError(f"expected {count} integers, got {value!r}")
	return [int(part) for part in parts]


def _color(value):
	# Authored "a,r,g,b" in 0..255; the client takes [r,g,b,a] 0..1.
	a, r, g, b = _ints(value, 4)
	return [r / 255, g / 255, b / 255, a / 255]


def parse_sections(text):
	"""
	{section name: {control name: properties}} of one resinfo file, in
	the authored order. Properties keep their raw string values.
	"""
	sections = {}
	controls = None
	current = None
	for raw in text.splitlines():
		line = raw.strip()
		match = SECTION.match(line)
		if match:
			controls = sections.setdefault(match.group(1).strip(), {})
			current = None
			continue
		match = NODE.match(line)
		if match and controls is not None:
			current = {"class": match.group(2)}
			controls[match.group(1)] = current
			continue
		match = PROPERTY.match(line)
		if match and current is not None:
			current[match.group(1)] = match.group(3)
	return sections


def is_frame(ddj):
	"""A frame names a prefix its eight parts complete (CIFFrame)."""
	return ddj != "" and not slash(ddj).endswith(".ddj")


def publish_control(name, properties, sizes):
	"""
	One control in the published shape. sizes maps a slashed DDJ path to
	its picture's [width, height]; a zero-sized Rect takes it.
	"""
	ddj = properties.get("DDJ", "")
	x, y, width, height = _ints(properties.get("Rect", "0,0,0,0"), 4)
	if ddj and not is_frame(ddj) and (width == 0 or height == 0):
		size = sizes.get(slash(ddj))
		if size is not None:
			width = width or size[0]
			height = height or size[1]
	return {
		"name": name,
		"id": int(properties.get("ID", "0")),
		"class": properties["class"],
		"rect": [x, y, width, height],
		"inset": _ints(properties.get("ClientRect", "0,0,0,0"), 4),
		"texture": public_path(ddj) if ddj else "",
		"frame": is_frame(ddj),
		"text": properties.get("Text", ""),
		"fontIndex": int(properties.get("FontIndex", "0")),
		"fontColor": _color(properties.get("FontColor", "255,255,255,255")),
		"hAlign": int(properties.get("HAlign", "0")),
		"vAlign": int(properties.get("VAlign", "0")),
	}


def picture_names(ddj):
	"""Every DDJ one reference stands for: a frame's parts, a button's states."""
	name = slash(ddj)
	if is_frame(ddj):
		return [name + part + ".ddj" for part in FRAME_PARTS]
	base = name[:-4]
	return [base + state + ".ddj" for state in BUTTON_STATES]


def dds_size(payload):
	"""A DDS's [width, height] (header height at 12, width at 16)."""
	height, width = DDS_SIZE.unpack_from(payload, 12)
	return [width, height]


def layout_strings(text, keys):
	"""
	{key: English} for the caption keys the layouts name, from the later
	client's textuisystem. Keys v1.150 also carries keep the native text
	at the client; these fill only the ones it lacks.
	"""
	wanted = set(keys)
	strings = {}
	for line in text.splitlines():
		columns = line.split("\t")
		if len(columns) <= ENGLISH_COLUMN or columns[0].strip() != "1":
			continue
		key = columns[1].strip()
		if key in wanted:
			strings[key] = columns[ENGLISH_COLUMN].strip().replace("\\n", "\n")
	return strings


def _entry_name(entry):
	name = getattr(entry, "path", None) or entry.name
	return name if isinstance(name, str) else name.decode("cp949")


def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", required=True, help="the pinned VSRO client folder")
	args = parser.parse_args()
	verify_vsro_archive(args.vsro_root, "Media")
	data = sro_pk2.map_archive(Path(args.vsro_root) / "Media.pk2")
	directory = sro_pk2.read_directory(data)
	entries = {}
	for entry in (directory.values() if isinstance(directory, dict) else directory):
		entries[slash(_entry_name(entry))] = entry

	raw_sections = {}
	references = list(CHROME_PICTURES)
	for key, (file_name, names) in LAYOUT_SECTIONS.items():
		entry = entries.get(file_name)
		if entry is None:
			raise SystemExit(f"the later client has no {file_name}")
		parsed = parse_sections(decode_table(sro_pk2.payload(data, entry)))
		controls = {}
		for section in names:
			if not parsed.get(section):
				raise SystemExit(f"{file_name} has no {section} section")
			controls.update(parsed[section])
		raw_sections[key] = controls
		references += [p["DDJ"] for p in controls.values() if p.get("DDJ")]

	names = []
	for reference in references:
		for name in picture_names(reference):
			if name not in names:
				names.append(name)

	sizes = {}
	images = []
	images_root = PUBLIC_ROOT / "assets" / "images" / "Media_extracted" / "later"
	with tempfile.TemporaryDirectory(prefix="sro-renewal-mall-") as scratch:
		for name in names:
			art = entries.get(name)
			if art is None:
				# A button without a focus, press or disable face keeps its
				# base picture for that state; a missing base is an error.
				if name.endswith(("_focus.ddj", "_press.ddj", "_disable.ddj")):
					continue
				raise SystemExit(f"the later client has no {name}")
			payload = ddj_payload(sro_pk2.payload(data, art))
			size = dds_size(payload)
			sizes[name] = size
			target = (Path(scratch) / Path(name).name).with_suffix(".png")
			save_image_payload(payload, target, target)
			path = public_path(name)
			destination = images_root / path[len(IMAGE_PUBLIC_ROOT):]
			destination.parent.mkdir(parents=True, exist_ok=True)
			shutil.move(str(target), destination)
			images.append({"path": path, "size": size})

	sections = {
		key: [publish_control(name, properties, sizes) for name, properties in controls.items()]
		for key, controls in raw_sections.items()
	}
	text_entry = entries.get(TEXT_TABLE)
	if text_entry is None:
		raise SystemExit(f"the later client has no {TEXT_TABLE}")
	keys = [control["text"] for rows in sections.values() for control in rows if control["text"]]
	keys += VIEW_KEYS
	strings = layout_strings(decode_table(sro_pk2.payload(data, text_entry)), keys)
	catalog = PUBLIC_ROOT / "assets" / "data" / CATALOG_NAME
	catalog.parent.mkdir(parents=True, exist_ok=True)
	catalog.write_text(
		json.dumps({
			"format": CATALOG_FORMAT,
			"version": CATALOG_VERSION,
			"sections": sections,
			"strings": strings,
			"images": images,
		}, separators=(",", ":"), ensure_ascii=False),
		encoding="utf-8", newline="\n"
	)
	print(f"{sum(len(s) for s in sections.values())} renewal mall control(s) and {len(images)} image(s) written; "
		"run pnpm assets repack")


if __name__ == "__main__":
	main()
