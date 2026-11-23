"""
===========================================================================

res_ui_layout.py - the later client's res_ui/*.2dt window layouts as data

The v1.188 and v1.696 clients describe their newer windows (the quest
list, the mini quest tracker) in binary res_ui/*.2dt files instead of
the v1.150 resinfo text. This module owns reading one: a u32 record
count, then fixed 976-byte control records. Each record names its class,
art, default text, control type, its id with the ids of its ancestors,
a colour and a screen rect (#4245). The ids repeat under every parent
(each quest slot holds its own control 7), so a control is keyed by its
id path below the root: "115/56" is control 56 of slot 115, "115" the
slot, and the root window keeps its own id ("173").

Offsets were read from the files themselves (questlist.2dt holds 84
records, 4 + 84 * 976 = 81988 bytes; questminilist.2dt 10). The field
roles were checked against the controls the quest list draws: slot ids
5/77/85/93/101, their children 7, 8, 11, 16, 18, 19 and 75, the
tracker's root 173 at y 267. Fields whose meaning is not settled
(flags, the root word) are exported raw, never interpreted here.

===========================================================================
"""

import struct

LAYOUT_FORMAT = "res-ui-layout"
LAYOUT_VERSION = 1

RECORD_BYTES = 976
HEADER_BYTES = 4

CLASS_OFFSET, CLASS_BYTES = 0, 64
FRAME_OFFSET, FRAME_BYTES = 64, 256
TEXTURE_OFFSET, TEXTURE_BYTES = 320, 256
NAME_OFFSET, NAME_BYTES = 704, 64
TEXT_OFFSET, TEXT_BYTES = 768, 64
TYPE_OFFSET = 832
PATH_OFFSET, PATH_DEPTH = 836, 5
COLOR_OFFSET = 856
RECT_OFFSET = 860
ROOT_WORD_OFFSET = 916
FLAGS_OFFSET = 972

# Designer strings are in the Korean code page the clients were authored in.
TEXT_CODEC = "cp949"


"""
================
c_string
================
"""
def c_string(data, offset, size, codec="latin-1"):
	return bytes(data[offset:offset + size]).split(b"\0", 1)[0].decode(codec, "replace")


"""
================
archive_path

A record's art path as the archive spells it, folded to the forward
slashes and lower case the published tree uses; "" when it has none.
================
"""
def archive_path(text):
	return text.replace("\\", "/").lower()


"""
================
read_layout

One .2dt file as a layout: the root id, and every control in file order
with its key, its parent's key, its children's keys and its fields.
Refuses a file whose size does not match its record count, a record with
no id, a control whose parent comes later or not at all, or a second
root.
================
"""
def read_layout(name, data):
	if len(data) < HEADER_BYTES:
		raise ValueError(f"{name}: no record count")
	count = struct.unpack_from("<I", data, 0)[0]
	if len(data) != HEADER_BYTES + count * RECORD_BYTES:
		raise ValueError(f"{name}: {len(data)} bytes do not hold {count} records of {RECORD_BYTES}")
	root = None
	controls = []
	by_key = {}
	for index in range(count):
		at = HEADER_BYTES + index * RECORD_BYTES
		ids = [value for value in struct.unpack_from(f"<{PATH_DEPTH}i", data, at + PATH_OFFSET)]
		depth = ids.index(0) if 0 in ids else PATH_DEPTH
		ids = ids[:depth]
		if not ids:
			raise ValueError(f"{name}: record {index} has no id")
		# ids run from the control up to the root; the key drops the root.
		below_root = list(reversed(ids[:-1]))
		if len(ids) == 1:
			if root is not None:
				raise ValueError(f"{name}: a second root {ids[0]} after {root}")
			root = ids[0]
			key, parent = str(root), None
		else:
			if ids[-1] != root:
				raise ValueError(f"{name}: record {index} hangs under {ids[-1]}, not the root {root}")
			key = "/".join(str(value) for value in below_root)
			parent = "/".join(str(value) for value in below_root[:-1]) or str(root)
			if parent not in by_key:
				raise ValueError(f"{name}: control {key} comes before its parent {parent}")
		if key in by_key:
			raise ValueError(f"{name}: control {key} appears twice")
		control = {
			"key": key,
			"id": ids[0],
			"parent": parent,
			"children": [],
			"type": struct.unpack_from("<i", data, at + TYPE_OFFSET)[0],
			"className": c_string(data, at + CLASS_OFFSET, CLASS_BYTES),
			"rect": list(struct.unpack_from("<4i", data, at + RECT_OFFSET)),
			"frame": archive_path(c_string(data, at + FRAME_OFFSET, FRAME_BYTES)),
			"texture": archive_path(c_string(data, at + TEXTURE_OFFSET, TEXTURE_BYTES)),
			"text": c_string(data, at + TEXT_OFFSET, TEXT_BYTES, TEXT_CODEC),
			"color": struct.unpack_from("<I", data, at + COLOR_OFFSET)[0],
			"flags": struct.unpack_from("<I", data, at + FLAGS_OFFSET)[0],
			"rootWord": struct.unpack_from("<I", data, at + ROOT_WORD_OFFSET)[0],
		}
		if parent is not None:
			by_key[parent]["children"].append(key)
		by_key[key] = control
		controls.append(control)
	if root is None:
		raise ValueError(f"{name}: no root window")
	return {"format": LAYOUT_FORMAT, "version": LAYOUT_VERSION, "name": name, "root": str(root), "controls": controls}


"""
================
layout_art

Every art path a layout names, sorted: the control textures and the frame
slots (a frame is a tile, or the start of a nine-piece frame the importer
resolves against the archive).
================
"""
def layout_art(layout):
	textures = sorted({control["texture"] for control in layout["controls"] if control["texture"]})
	frames = sorted({control["frame"] for control in layout["controls"] if control["frame"]})
	return textures, frames
