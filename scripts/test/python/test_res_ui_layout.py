"""
===========================================================================

test_res_ui_layout.py - the later client's .2dt layouts and their art (#4245)

Builds .2dt files record by record, the way the archive's questlist and
questminilist are laid out, and checks the tree the reader keys from
them, the fields it carries, the files it refuses, and the art the
importer asks the archive for.

===========================================================================
"""

import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import struct
import unittest

import import_vsro_res_layouts as importer  # noqa: E402
import res_ui_layout as layout  # noqa: E402


"""
================
record

One 976-byte control record: its id path runs from the control up to the root.
================
"""
def record(path, type_, rect, class_name="", texture="", frame="", text="", color=0xFFFFFFFF, flags=0x10100):
	data = bytearray(layout.RECORD_BYTES)
	data[layout.CLASS_OFFSET:layout.CLASS_OFFSET + len(class_name)] = class_name.encode("latin-1")
	data[layout.FRAME_OFFSET:layout.FRAME_OFFSET + len(frame)] = frame.encode("latin-1")
	data[layout.TEXTURE_OFFSET:layout.TEXTURE_OFFSET + len(texture)] = texture.encode("latin-1")
	encoded = text.encode(layout.TEXT_CODEC)
	data[layout.TEXT_OFFSET:layout.TEXT_OFFSET + len(encoded)] = encoded
	struct.pack_into("<i", data, layout.TYPE_OFFSET, type_)
	struct.pack_into(f"<{len(path)}i", data, layout.PATH_OFFSET, *path)
	struct.pack_into("<I", data, layout.COLOR_OFFSET, color)
	struct.pack_into("<4i", data, layout.RECT_OFFSET, *rect)
	struct.pack_into("<I", data, layout.FLAGS_OFFSET, flags)
	return bytes(data)


def file_of(*records):
	return struct.pack("<I", len(records)) + b"".join(records)


# The tracker's shape: a root, two slots, each slot's fold button and text.
TRACKER = file_of(
	record([173], 18, [702, 267, 324, 93], "CNIFMiniQuestWnd"),
	record([115, 173], 8, [702, 267, 324, 30], "CNIFMiniQuestSlot", "interface\\quest\\qst_list_background.ddj"),
	record([56, 115, 173], 4, [750, 275, 12, 12], texture="interface\\Quest\\qst_list_close_button.ddj"),
	record([57, 115, 173], 5, [768, 274, 251, 16], text="진행중", color=0xFFFFF9BA, flags=0x10000),
	record([121, 173], 8, [702, 327, 324, 30], "CNIFMiniQuestSlot", "interface\\quest\\qst_list_background.ddj"),
	record([56, 121, 173], 4, [750, 335, 12, 12], texture="interface\\Quest\\qst_list_close_button.ddj"),
)


class ReadLayoutTest(unittest.TestCase):
	def test_controls_key_by_their_path_below_the_root(self):
		read = layout.read_layout("questminilist", TRACKER)
		self.assertEqual((read["format"], read["version"], read["root"]), ("res-ui-layout", 1, "173"))
		controls = {control["key"]: control for control in read["controls"]}
		self.assertEqual(list(controls), ["173", "115", "115/56", "115/57", "121", "121/56"])
		self.assertEqual(controls["173"]["children"], ["115", "121"])
		self.assertEqual(controls["115"]["children"], ["115/56", "115/57"])
		self.assertEqual((controls["121/56"]["parent"], controls["121/56"]["id"]), ("121", 56))
		self.assertIsNone(controls["173"]["parent"])

	def test_fields_carry_the_record(self):
		controls = {control["key"]: control for control in layout.read_layout("questminilist", TRACKER)["controls"]}
		text = controls["115/57"]
		self.assertEqual((text["type"], text["rect"], text["text"]), (5, [768, 274, 251, 16], "진행중"))
		self.assertEqual((text["color"], text["flags"]), (0xFFFFF9BA, 0x10000))
		self.assertEqual(controls["115"]["className"], "CNIFMiniQuestSlot")
		# Archive paths fold to the published tree's slashes and case.
		self.assertEqual(controls["115/56"]["texture"], "interface/quest/qst_list_close_button.ddj")

	def test_refuses_a_broken_file(self):
		root = record([173], 18, [0, 0, 1, 1])
		cases = {
			"size": file_of(root)[:-1],
			"orphan": file_of(root, record([56, 115, 173], 4, [0, 0, 1, 1])),
			"second root": file_of(root, record([174], 18, [0, 0, 1, 1])),
			"other root": file_of(root, record([115, 174], 8, [0, 0, 1, 1])),
			"twice": file_of(root, record([115, 173], 8, [0, 0, 1, 1]), record([115, 173], 8, [0, 0, 1, 1])),
			"no id": file_of(root, record([], 8, [0, 0, 1, 1])),
		}
		for name, data in cases.items():
			with self.subTest(name), self.assertRaises(ValueError):
				layout.read_layout("broken", data)

	def test_art_lists_textures_and_frames_once(self):
		read = layout.read_layout("questminilist", TRACKER)
		self.assertEqual(layout.layout_art(read), ([
			"interface/quest/qst_list_background.ddj",
			"interface/quest/qst_list_close_button.ddj",
		], []))


class ArtEntriesTest(unittest.TestCase):
	NAMES = {
		"interface/quest/qst_world_button.ddj",
		"interface/quest/qst_world_button_focus.ddj",
		"interface/quest/qst_world_button_press.ddj",
		"interface/frame/mframe_wnd_1.ddj",
		"interface/frame/mframe_wnd_2.ddj",
		"interface/frame/other.ddj",
		"interface/ifcommon/bg_tile/com_bg_tile_a.ddj",
	}

	def test_faces_and_frame_pieces_come_along(self):
		wanted = importer.art_entries(
			self.NAMES,
			["interface/quest/qst_world_button.ddj"],
			["interface/frame/mframe_wnd_", "interface/ifcommon/bg_tile/com_bg_tile_a.ddj"],
		)
		self.assertEqual(wanted, sorted([
			"interface/quest/qst_world_button.ddj",
			"interface/quest/qst_world_button_focus.ddj",
			"interface/quest/qst_world_button_press.ddj",
			"interface/frame/mframe_wnd_1.ddj",
			"interface/frame/mframe_wnd_2.ddj",
			"interface/ifcommon/bg_tile/com_bg_tile_a.ddj",
		]))

	def test_missing_art_refuses(self):
		for textures, frames in ((["interface/quest/none.ddj"], []), ([], ["interface/frame/none_"])):
			with self.subTest(textures + frames), self.assertRaises(SystemExit):
				importer.art_entries(self.NAMES, textures, frames)

	def test_public_image_is_the_extraction_layout(self):
		self.assertEqual(
			importer.public_image("interface/quest/qst_sub_window.ddj"),
			"assets/images/Media_extracted/interface/quest/qst_sub_window.png",
		)


if __name__ == "__main__":
	unittest.main()
