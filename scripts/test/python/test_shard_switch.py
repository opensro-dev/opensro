"""
===========================================================================

test_shard_switch.py - a shard switch's deployed posture and its overlays

The Python build reads a port-only switch the way the deployed shard
does (shardSwitch.mjs): an explicit env value wins, "off" included; an
unset one takes the profile's entry, else the registry Default; a
registry without the row stops the build. Overlays skip blank and
comment lines and a byte-order mark.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ on sys.path)
import json
import tempfile
import unittest
from pathlib import Path

import shard_switch


REGISTRY = '''var Registry = []Setting{
	{Env: "SRO_TOMB", NomadVar: "tomb", Default: "on", Catalogue: true},
	{Env: "SRO_OTHER", NomadVar: "other", Default: "off"},
}
'''


# ================
# server
#
# A scratch server root holding a profile and the registry.
# ================
def server(base, profile):
	(base / "config").mkdir(parents=True)
	(base / shard_switch.SHARD_PROFILE).write_text(json.dumps(profile), encoding="utf-8")
	(base / shard_switch.SHARD_REGISTRY).parent.mkdir(parents=True)
	(base / shard_switch.SHARD_REGISTRY).write_text(REGISTRY, encoding="utf-8")
	return base


class Posture(unittest.TestCase):
	def test_unset_takes_the_profile_then_the_registry_default(self):
		with tempfile.TemporaryDirectory() as root:
			base = server(Path(root), {"SRO_OTHER": "on"})
			self.assertEqual(shard_switch.switch_posture("SRO_TOMB", base), "on")
			self.assertEqual(shard_switch.switch_posture("SRO_OTHER", base), "on")
			self.assertTrue(shard_switch.switch_enabled("SRO_TOMB", ("on",), {}, base))
			self.assertTrue(shard_switch.switch_enabled("SRO_TOMB", ("on",), {"SRO_TOMB": "  "}, base))

	def test_an_explicit_value_wins_off_included(self):
		with tempfile.TemporaryDirectory() as root:
			base = server(Path(root), {})
			self.assertFalse(shard_switch.switch_enabled("SRO_TOMB", ("on",), {"SRO_TOMB": "off"}, base))
			self.assertTrue(shard_switch.switch_enabled("SRO_OTHER", ("on", "yes"), {"SRO_OTHER": " YES "}, base))

	def test_a_switch_the_registry_lacks_stops_the_build(self):
		with tempfile.TemporaryDirectory() as root:
			base = server(Path(root), {})
			with self.assertRaisesRegex(ValueError, "SRO_MISSING"):
				shard_switch.switch_enabled("SRO_MISSING", ("on",), {}, base)

	def test_the_shipped_registry_defaults_the_qinshi_tomb_on(self):
		# The case r203g turned on: the live shard's tomb rows stand.
		self.assertEqual(shard_switch.switch_posture("SRO_QINSHI_TOMB"), "on")


class Overlay(unittest.TestCase):
	def test_rows_skip_blank_and_comment_lines_and_the_mark(self):
		with tempfile.TemporaryDirectory() as root:
			path = Path(root) / "data" / "x.tsv"
			path.parent.mkdir()
			path.write_text("﻿# header\n\n// note\r\n1\tMOB_A\t5\r\n  \n1\tMOB_B\t6\n", encoding="utf-8")
			self.assertEqual(shard_switch.overlay_rows("data/x.tsv", Path(root)), [["1", "MOB_A", "5"], ["1", "MOB_B", "6"]])


if __name__ == "__main__":
	unittest.main()
