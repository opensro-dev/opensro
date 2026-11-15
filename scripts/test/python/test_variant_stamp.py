"""
===========================================================================

test_variant_stamp.py - an unchanged variant is not rebuilt

A variant stamped after a build is current while its code, flags, sources
and tree are what that build saw. A stage 1 rerun that writes the same
bytes with fresh times stays current. A touched archive, changed stage 1
bytes, a changed flag, a
lost or added variant file, a missing manifest or a cleared stamp each make
it stale and name why. A variable that names the variant (an output) is
never a source, and a flag's value never reaches the stamp file.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path

import arabia_import
import variant_settle
import variant_stamp

MANIFEST = ".opensro-alexandria-variant.json"
# A later time than any file in the scratch tree, in nanoseconds.
LATER_NS = 4_000_000_000_000_000_000
SECRET = "webhook-secret-value"


class Stamp(unittest.TestCase):

	@classmethod
	def setUpClass(cls):
		# The closure runs node once; every test reuses its digest.
		cls.code = variant_stamp.code_hash()

	def setUp(self):
		self.temp = tempfile.TemporaryDirectory()
		root = Path(self.temp.name)
		self.native = self.write(root / "extracted" / "Media_extracted" / "a.txt", b"native").parents[1]
		self.imported = self.write(root / "stage1" / "Map" / "r.m", b"region").parents[1]
		self.vsro = self.write(root / "vsro" / "Data.pk2", b"archive").parent
		self.variant = root / "variant"
		self.write(self.variant / "extracted" / "Media_extracted" / "a.txt", b"native")
		self.write(self.variant / MANIFEST, b"{}")
		self.env = {
			"SRO_ALEXANDRIA": "1",
			"SRO_V1696_ROOT": str(self.write(root / "v1696" / "Media.pk2", b"later").parent),
			arabia_import.NESTS_ENV: str(self.write(root / "nests.json", b"[]")),
			"SRO_GAME_ROOT": str(self.variant),
			"SRO_X_WEBHOOK": SECRET,
			"SRO_HOME": str(self.write(root / "home" / "notes.txt", b"machine").parent),
			"SRO_BUILD_JOBS": "15"
		}
		variant_settle.settle(self.variant, 2)
		variant_stamp.write_stamp(self.variant, self.inputs())

	def tearDown(self):
		self.temp.cleanup()

	def write(self, path, content):
		path.parent.mkdir(parents=True, exist_ok=True)
		path.write_bytes(content)
		return path

	def inputs(self, env=None):
		return variant_stamp.read_inputs(self.native, self.imported, self.vsro, self.variant, env or self.env, self.code)

	def stale(self, env=None):
		return variant_stamp.stale_parts(self.variant, self.inputs(env), (MANIFEST,))

	def test_an_unchanged_build_is_current(self):
		self.assertEqual(self.stale(), [])

	def test_a_touched_source_is_named(self):
		archive = self.vsro / "Data.pk2"
		os.utime(archive, ns=(LATER_NS, LATER_NS))
		self.assertEqual(self.stale(), [f"sources:{self.vsro.resolve()}"])

	def test_a_rerun_stage1_with_the_same_bytes_is_current(self):
		region = self.imported / "Map" / "r.m"
		region.write_bytes(b"region")
		os.utime(region, ns=(LATER_NS, LATER_NS))
		self.assertEqual(self.stale(), [])

	def test_a_changed_stage1_file_is_named(self):
		(self.imported / "Map" / "r.m").write_bytes(b"other region")
		self.assertEqual(self.stale(), [f"sources:{self.imported.resolve()}"])

	def test_a_changed_flag_is_named(self):
		self.assertEqual(self.stale({**self.env, "SRO_ALEXANDRIA": "on"}), ["env:SRO_ALEXANDRIA"])

	def test_parallelism_is_not_an_input(self):
		self.assertEqual(self.stale({**self.env, "SRO_BUILD_JOBS": "4"}), [])

	def test_the_variant_is_never_its_own_source(self):
		sources = self.inputs()["sources"]
		self.assertNotIn(str(self.variant.resolve()), sources)
		self.assertIn(str(self.native.resolve()), sources)

	def test_only_importer_source_variables_are_walked(self):
		sources = self.inputs()["sources"]
		self.assertIn(str(Path(self.env["SRO_V1696_ROOT"]).resolve()), sources)
		self.assertNotIn(str(Path(self.env["SRO_HOME"]).resolve()), sources)

	def test_an_edited_or_extra_variant_file_rebuilds(self):
		self.write(self.variant / "extracted" / "Media_extracted" / "b.txt", b"stray")
		self.assertEqual(self.stale(), ["the variant tree changed since it was built"])

	def test_a_missing_manifest_rebuilds(self):
		(self.variant / MANIFEST).unlink()
		self.assertEqual(self.stale(), [f"missing {MANIFEST}"])

	def test_a_cleared_stamp_rebuilds(self):
		variant_stamp.clear_stamp(self.variant)
		self.assertEqual(self.stale(), ["no stamp"])

	def test_values_never_reach_the_stamp_file(self):
		text = variant_stamp.stamp_path(self.variant).read_text(encoding="utf-8")
		self.assertNotIn(SECRET, text)
		self.assertIn("SRO_X_WEBHOOK", text)


class Code(unittest.TestCase):

	def test_the_closure_holds_the_importers_and_their_node_helper(self):
		names = {path.name for path in variant_stamp.code_closure()}
		for name in ("build_alexandria_variant.py", "variant_later_layers.py", "cap110_asset_dependencies.mjs"):
			self.assertIn(name, names)


if __name__ == "__main__":
	unittest.main()
