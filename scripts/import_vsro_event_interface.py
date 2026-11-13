"""
===========================================================================

import_vsro_event_interface.py - the later client's event window layouts

The v1.150 archives ship event_interface.txt alone (closed on this
version); the pinned VSRO client also has the PC-room and winter layouts,
both open. This tool publishes them in the same shape the asset pipeline
publishes the native gate (textResources.mjs buildEventInterfaceLayout)
plus the PC-room window art, so CIFEventInterface's type-1 window renders
exactly as the later client renders it. The winter layout (type 2) rides
along; no v1.150 caller passes type 2, so nothing shows it.
Run `pnpm assets repack` afterwards: the client loads published assets only.
No retail art is committed to Git.

===========================================================================
"""
import argparse
import json
import re
import shutil
import tempfile
from pathlib import Path

import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import decode_table, verify_vsro_archive
from import_vsro_tomb_minimap import ddj_payload
from sro_paths import PUBLIC_ROOT

CATALOG_FORMAT = "sro-event-interface-later"
CATALOG_VERSION = 1
CATALOG_NAME = "event-interface-later.json"
IMAGE_PUBLIC_ROOT = "/assets/images/Media_extracted/"
EVENT_DIRECTORY = "server_dep/silkroad/event/"
LAYOUT_FILES = {
	"pcroom": "pcroom_event_interface.txt",
	"winter": "winter_event_interface.txt",
}
KEY_VALUE = re.compile(r'^([A-Za-z0-9_]+)\s*=\s*"([^"]*)"')


def parse_fields(text):
	"""The layout file's Key = "value" fields (546B50's line grammar)."""
	fields = {}
	for raw_line in text.splitlines():
		line = raw_line.strip()
		if not line or line.startswith("//"):
			continue
		match = KEY_VALUE.match(line)
		if match:
			fields[match.group(1)] = match.group(2)
	return fields


def _integer(value, fallback):
	try:
		return int(str(value).strip())
	except (TypeError, ValueError):
		return fallback


def _ints(value, count, fallback):
	if not value:
		return fallback
	parts = [_integer(part, None) for part in value.split(",")]
	if len(parts) != count or any(part is None for part in parts):
		return fallback
	return parts


def _color(value):
	# Authored "a,r,g,b" in 0..255; the client takes [r,g,b,a] 0..1.
	parts = _ints(value, 4, None)
	if parts is None:
		return [1, 1, 1, 1]
	return [parts[1] / 255, parts[2] / 255, parts[3] / 255, parts[0] / 255]


def _escapes(value):
	return value.replace("\\n", "\n").replace("\\N", "\n") \
		.replace("\\t", "\t").replace("\\T", "\t")


def _slash(path):
	# The layout files mix "interface\event" and "interface\\event".
	return re.sub(r"[\\/]+", "/", path).lower()


def image_public_path(ddj_path):
	"""The published PNG path of a DDJ under Media_extracted."""
	name = _slash(ddj_path)
	if name.endswith(".ddj"):
		name = name[:-4] + ".png"
	return IMAGE_PUBLIC_ROOT + name


def parse_event_layout(text):
	"""One layout file in the published shape (buildEventInterfaceLayout)."""
	fields = parse_fields(text)
	layout = {
		"open": fields.get("Open", "1") == "1",
		"titleFont": _integer(fields.get("TitleFont"), 0),
		"titleColor": _color(fields.get("TitleColor")),
		"titlePosition": _ints(fields.get("TitlePosition"), 2, [0, 0]),
		"title": _escapes(fields.get("TitleString", "")),
		"titleCode": fields.get("TitleStringCode", ""),
		"textFont": _integer(fields.get("TextFont"), 0),
		"textColor": _color(fields.get("TextColor")),
		"textPosition": _ints(fields.get("TextPosition"), 2, [0, 0]),
		"textMargin": _integer(fields.get("TextMargin"), 5),
		"text": _escapes(fields.get("TextString", "")),
		"textCode": fields.get("TextStringCode", ""),
	}
	if fields.get("PicBG"):
		layout["picBg"] = image_public_path(fields["PicBG"])
	if fields.get("PicButton"):
		layout["picButton"] = image_public_path(fields["PicButton"])
	rect = _ints(fields.get("ButtonPosition"), 4, None)
	if rect is not None:
		layout["buttonRect"] = rect
	return layout


def _button_variants(button_ddj):
	"""The close button's native state images beside its base DDJ."""
	base = button_ddj[:-4] if button_ddj.lower().endswith(".ddj") else button_ddj
	return [base + suffix + ".ddj" for suffix in ("", "_focus", "_press")]


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
		entries[_slash(_entry_name(entry))] = entry

	layouts = {}
	images_written = 0
	images_root = PUBLIC_ROOT / "assets" / "images" / "Media_extracted"
	with tempfile.TemporaryDirectory(prefix="sro-event-interface-") as scratch:
		for key, file_name in LAYOUT_FILES.items():
			entry = entries.get((EVENT_DIRECTORY + file_name).lower())
			if entry is None:
				raise SystemExit(f"the later client has no {file_name}")
			text = decode_table(sro_pk2.payload(data, entry))
			fields = parse_fields(text)
			layouts[key] = parse_event_layout(text)
			if key != "pcroom":
				# The winter window's art is the native mayevent set, which
				# the asset pipeline already publishes from v1.150.
				continue
			bg_ddj = fields.get("PicBG")
			button_ddj = fields.get("PicButton")
			if not bg_ddj or not button_ddj:
				raise SystemExit("pcroom_event_interface.txt names no pictures")
			names = [bg_ddj] + _button_variants(button_ddj)
			for name in names:
				art = entries.get(_slash(name))
				if art is None:
					if name == names[0] or name == names[1]:
						raise SystemExit(f"the later client has no {name}")
					continue
				target = Path(scratch) / Path(_slash(name)).name
				target = target.with_suffix(".png")
				save_image_payload(ddj_payload(sro_pk2.payload(data, art)), target, target)
				destination = images_root / Path(
					image_public_path(name)[len(IMAGE_PUBLIC_ROOT):]
				)
				destination.parent.mkdir(parents=True, exist_ok=True)
				shutil.move(str(target), destination)
				images_written += 1
	catalog = PUBLIC_ROOT / "assets" / "data" / CATALOG_NAME
	catalog.write_text(
		json.dumps({
			"format": CATALOG_FORMAT,
			"version": CATALOG_VERSION,
			"layouts": layouts
		}, separators=(",", ":"), ensure_ascii=False),
		encoding="utf-8", newline="\n"
	)
	print(f"{len(layouts)} later event layout(s) and {images_written} image(s) written; "
		"run pnpm assets repack")


if __name__ == "__main__":
	main()
