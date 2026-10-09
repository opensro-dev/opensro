"""
===========================================================================

test_compiled_quest_evidence.py - byte and word fields of a compiled quest

Synthetic HLIL lines exercise the production importer, and synthetic
snapshot rows the generator's field check: a prerequisite that must be
completed three times is refused, never projected as completed once. No
licensed input or generated output is read or written.

===========================================================================
"""

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from import_compiled_quest_evidence import parse_initializer
from generate_compiled_quests import Unsupported, check_fields


# ================
# CompiledQuestFieldTests
# ================
class CompiledQuestFieldTests(unittest.TestCase):
	# ================
	# test_sub_width_writes_are_recorded
	#
	# A run-time value is kept as null, not dropped.
	# ================
	def test_sub_width_writes_are_recorded(self):
		quest = parse_initializer(
			"arg3[0x106].b = 3\n"
			"arg1[0x15c].w = *eax_11\n"
			"arg3[0x130] = \"SN_TALK_FIXTURE_01\"\n"
		)
		self.assertEqual(quest["words"], {"0x106.b": 3, "0x15c.w": None, "0x130": "SN_TALK_FIXTURE_01"})

	# ================
	# test_constructor_defaults_pass
	# ================
	def test_constructor_defaults_pass(self):
		check_fields({"words": {"0x106.b": 1, "0xc3.b": 0, "0x130": "SN_TALK_FIXTURE_01"}})
		check_fields({"words": {}})

	# ================
	# test_changed_fields_are_refused
	# ================
	def test_changed_fields_are_refused(self):
		cases = (
			({"0x106.b": 3}, "prerequisite completed 3 times"),
			({"0x15b.b": 1, "0x15c.w": None}, "instance world quest"),
			({"0x1a0.b": 2}, "quest field 0x1a0.b = 2"),
		)
		for words, reason in cases:
			with self.subTest(words=words):
				with self.assertRaisesRegex(Unsupported, reason):
					check_fields({"words": words})


if __name__ == "__main__":
	unittest.main()
