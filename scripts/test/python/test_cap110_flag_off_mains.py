"""
===========================================================================

test_cap110_flag_off_mains.py - the cap-110 tools need no client when off

With their flags off, the four standalone cap-110 tools only remove their
owned outputs and read no VSRO archive, so the archive pin (#1745) must
not refuse them: each main runs against an empty VSRO root and returns.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import contextlib
import io
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import cap110_item_import  # noqa: E402
import cap110_monsters  # noqa: E402
import cap110_rewards  # noqa: E402
import cap110_skill_import  # noqa: E402
import import_vsro_alexandria  # noqa: E402

FLAGS = ("SRO_CAP110_MONSTERS", "SRO_CAP110_ITEMS", "SRO_CAP110_SKILLS", "SRO_PHARAOH_TOMB")


class FlagOffMainTests(unittest.TestCase):
	def setUp(self):
		import_vsro_alexandria._verified_archives.clear()
		self.root = tempfile.TemporaryDirectory()
		base = Path(self.root.name)
		self.vsro, self.native = base / "vsro", base / "native"
		self.vsro.mkdir()
		self.native.mkdir()
		self.output = base / "output"
		self.env = mock.patch.dict(os.environ, {flag: "" for flag in FLAGS})
		self.env.start()

	def tearDown(self):
		self.env.stop()
		self.root.cleanup()

	def run_main(self, module, argv):
		with mock.patch.object(sys, "argv", [module.__name__, *argv]), contextlib.redirect_stdout(io.StringIO()):
			module.main()

	def test_every_main_runs_off_without_a_client(self):
		common = ["--vsro-root", str(self.vsro), "--native-extracted", str(self.native)]
		overlay = ["--output", str(self.output), "--source-root", str(self.native)]
		cases = [
			(cap110_monsters, common + overlay),
			(cap110_rewards, common + overlay),
			(cap110_item_import, common + ["--output-extracted", str(self.output / "items")]),
			(cap110_skill_import, common + ["--output-extracted", str(self.output / "skills")]),
		]
		for module, argv in cases:
			with self.subTest(module.__name__):
				self.run_main(module, argv)

	def test_an_enabled_main_still_refuses_an_unpinned_root(self):
		with mock.patch.dict(os.environ, {"SRO_CAP110_MONSTERS": "on"}):
			argv = ["--vsro-root", str(self.vsro), "--native-extracted", str(self.native),
				"--output", str(self.output), "--source-root", str(self.native)]
			with self.assertRaises(SystemExit):
				self.run_main(cap110_monsters, argv)


if __name__ == "__main__":
	unittest.main()
