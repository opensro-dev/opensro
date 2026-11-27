"""
===========================================================================

test_winter_event_import.py - the Christmas event's later monsters (#4540)

Pins which later character rows the winter import admits (the winter
kinds and So-Ok's Pandora's Box monster, under the level cap) and the
presentation half it shares with the rigid scrolls: the kinds'
characterinfo rows join the native section, their sound rules join the
native table, and the switch off takes the rules away again.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path

import cap110_monsters as cap110
import later_world_import
import winter_event_import as winter


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in rows).encode("utf-16-le")


# ================
# character_row
#
# A later character row holding only the cells admits reads.
# ================
def character_row(code, level):
	row = ["0"] * (winter.CHAR_LEVEL + 1)
	row[winter.CHAR_CODE], row[winter.CHAR_LEVEL] = code, str(level)
	return row


# ================
# info_row
#
# A later characterinfo row: service and id, then the native record.
# ================
def info_row(code):
	return ["1", "1029", code, code, "3.8", "none", "none", "none", "none", "none", "0,14,-8", "hit_2_greenblood", "none", "0", "0"]


# ================
# sound_rule
# ================
def sound_rule(code, kind, wav):
	return "\t".join(["1", code, kind, "-", "-", "-", "-", "0", "Common\\", wav, "80"])


NATIVE_SOUND = "\tMOB_CH_TIGER\tSND_STAND\t-\t-\t-\t-\t0\tCommon\\\ttiger.wav\t80\r\n"


# ================
# WinterSwitchTest
#
# The kinds import only in a mode that spawns them: the decoration mode
# (#4676) and an unknown mode leave them out, and such a build reaps a
# previous import before it reads any archive.
# ================
class WinterSwitchTest(unittest.TestCase):
	def test_the_kinds_follow_the_modes_that_spawn_them(self):
		for mode, on in (("", True), ("winter", True), (" So_Ok_2006 ", True), ("decoration", False), ("snow", False)):
			with self.subTest(mode=mode):
				self.assertEqual(winter.flag_enabled({winter.FLAG: "on", winter.MODE_ENV: mode}), on)
		self.assertFalse(winter.flag_enabled({winter.FLAG: "off", winter.MODE_ENV: "winter"}))

	def test_the_decoration_mode_reaps_without_reading_an_archive(self):
		with tempfile.TemporaryDirectory() as folder:
			root = Path(folder)
			native, out = root / "native", root / "out"
			native.mkdir()
			out.mkdir()
			env = {winter.FLAG: "on", winter.MODE_ENV: "decoration", winter.ROOT_ENV: str(root / "no-such-client")}
			self.assertEqual(winter.add_winter_monsters(native, out, env), {"enabled": False, "reason": "off"})


# ================
# WinterAdmitsTest
# ================
class WinterAdmitsTest(unittest.TestCase):
	def test_the_pandora_box_rides_with_the_winter_kinds(self):
		self.assertTrue(winter.admits(character_row(winter.PANDORA_BOX, 1)))
		self.assertTrue(winter.admits(character_row("MOB_EV_WINTER_SNOWMAN", 1)))

	def test_other_event_kinds_and_kinds_over_the_cap_stay_out(self):
		self.assertFalse(winter.admits(character_row("MOB_EVENT_PANDORA_GOLD", 1)))
		self.assertFalse(winter.admits(character_row("MOB_EV_SNOWMAN", 1)))
		self.assertFalse(winter.admits(character_row("MOB_EV_WINTER_PRINCESS_130", cap110.MAX_LEVEL + 1)))


# ================
# WinterPresentationTest
# ================
class WinterPresentationTest(unittest.TestCase):
	# ================
	# test_the_box_gets_its_profile_and_sounds_and_loses_the_sounds_off
	#
	# The box names itself as its characterinfo base, so its own row
	# joins the native section (not the first profile's), and its rules
	# join beside the native ones; the off reap drops only the rules
	# the marker names.
	# ================
	def test_the_box_gets_its_profile_and_sounds_and_loses_the_sounds_off(self):
		later = {
			"characterinfo.txt": utf16([info_row(winter.PANDORA_BOX), info_row("MOB_EV_SNOWMAN")]),
			winter.SOUND_TABLE: "\r\n".join([sound_rule(winter.PANDORA_BOX, "SND_STAND", "obj_knock1.wav"),
				sound_rule(winter.PANDORA_BOX, "VOC_SHOUT1", "obj_boxopen.wav"),
				sound_rule("MOB_EV_SNOWMAN", "SND_STAND", "snow.wav")]).encode(winter.SOUND_ENCODING),
		}
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			(native / later_world_import.TEXT_ROOT).mkdir(parents=True)
			(native / later_world_import.TEXT_ROOT / winter.SOUND_TABLE).write_bytes(NATIVE_SOUND.encode(winter.SOUND_ENCODING))
			text_root = out / later_world_import.TEXT_ROOT
			text_root.mkdir(parents=True)
			effect = "#section\tcharacterInfo\r\nMOB_CH_TIGER\tMOB_CH_TIGER\t2\r\n#section\tskill\r\nSKILL_ROW\r\n"
			(text_root / "skilleffect.txt").write_bytes(b"\xff\xfe" + effect.encode("utf-16-le"))

			summary = winter.add_winter_presentation(native, out, later.__getitem__, {winter.PANDORA_BOX}, {})

			self.assertIn(winter.PANDORA_BOX, summary["characterInfo"])
			effect_text = (text_root / "skilleffect.txt").read_bytes().decode("utf-16")
			self.assertIn(winter.PANDORA_BOX, cap110.characterinfo_codenames(effect_text))
			self.assertLess(effect_text.index(winter.PANDORA_BOX), effect_text.index("#section\tskill"))
			sounds = (text_root / winter.SOUND_TABLE).read_bytes().decode(winter.SOUND_ENCODING)
			self.assertIn("MOB_CH_TIGER", sounds)
			self.assertIn("obj_boxopen.wav", sounds)

			winter.reap_sounds(out, summary)
			sounds = (text_root / winter.SOUND_TABLE).read_bytes().decode(winter.SOUND_ENCODING)
			self.assertIn("MOB_CH_TIGER", sounds)
			self.assertNotIn(winter.PANDORA_BOX, sounds)


if __name__ == "__main__":
	unittest.main()
