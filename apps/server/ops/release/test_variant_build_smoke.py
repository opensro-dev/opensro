"""
===========================================================================

test_variant_build_smoke.py - the variant build, run once for real

test_variant_build.py fakes the four steps (no game data). This file
is the cover the #1745 rehearsal asked for ("D1, D2, D4 fixed and
covered by a real end-to-end smoke"): with the operator's pinned
inputs present it runs variant_build.build with its real step runner
into a fresh temporary staging, in the full candidate posture (the
equipment and skill tiers, the field monsters and the qinshi family
on), and asserts the staged bundle holds what that candidate must
hold while the native artifacts it was shown keep their hashes.

The archives and exports are operator-supplied and never committed,
so without the inputs this skips, naming what is missing; most
machines can only ever prove the skip. Where the inputs exist the
run takes minutes (the assets build is one of the four steps).

Inputs (environment):
	SRO_VSRO_ROOT                   the pinned later-client archives
	SRO_CAP110_MONSTER_DATABASE     the monster database export
	SRO_VARIANT_SMOKE_NATIVE_SERVER_ARCHIVE
	                                the native server.srogz
	SRO_VARIANT_SMOKE_NATIVE_CLIENT_MANIFEST
	                                the native client manifest
plus node on PATH and the native mapinfo in the native extraction
(variant_build.native_mapinfo_path).

===========================================================================
"""
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

import variant_build

VSRO_ROOT_ENV = "SRO_VSRO_ROOT"
NATIVE_ARCHIVE_ENV = "SRO_VARIANT_SMOKE_NATIVE_SERVER_ARCHIVE"
NATIVE_MANIFEST_ENV = "SRO_VARIANT_SMOKE_NATIVE_CLIENT_MANIFEST"


# ================
# missing_smoke_inputs
#
# What keeps this machine from running the build for real, named so
# the skip says what a rehearsal machine must provide.
# ================
def missing_smoke_inputs():
	missing = []
	for name in (VSRO_ROOT_ENV, variant_build.MONSTER_DATABASE_ENV):
		value = os.environ.get(name, "").strip()
		if not value or not Path(value).is_dir():
			missing.append(name)
	for name in (NATIVE_ARCHIVE_ENV, NATIVE_MANIFEST_ENV):
		value = os.environ.get(name, "").strip()
		if not value or not Path(value).is_file():
			missing.append(name)
	if not variant_build.native_mapinfo_path().is_file():
		missing.append("the native mapinfo")
	if shutil.which("node") is None:
		missing.append("node on PATH")
	return missing


MISSING = missing_smoke_inputs()


class MissingSmokeInputTests(unittest.TestCase):
	# ================
	# test_unset_and_bogus_inputs_are_missing
	#
	# The gate, pinned where no inputs exist: an unset input is
	# missing (an empty value must not read as the current folder),
	# and so is one naming nothing on disk.
	# ================
	def test_unset_and_bogus_inputs_are_missing(self):
		with mock.patch.dict(os.environ, {VSRO_ROOT_ENV: "", variant_build.MONSTER_DATABASE_ENV: ""}):
			missing = missing_smoke_inputs()
		self.assertIn(VSRO_ROOT_ENV, missing)
		self.assertIn(variant_build.MONSTER_DATABASE_ENV, missing)
		with mock.patch.dict(os.environ, {VSRO_ROOT_ENV: "definitely-absent-smoke-input"}):
			self.assertIn(VSRO_ROOT_ENV, missing_smoke_inputs())


@unittest.skipIf(MISSING, "variant smoke inputs absent: " + ", ".join(MISSING))
class VariantBuildSmokeTests(unittest.TestCase):
	def setUp(self):
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		self.staging = Path(self.root.name) / "staging"

	# ================
	# test_the_variant_build_runs_end_to_end
	#
	# The shipped command, for real: build() itself refuses a bundle
	# that disagrees with the settings and any drift in the native
	# artifacts; this test adds the candidate's file-level shape and
	# a second, direct reading of the native hashes.
	# ================
	def test_the_variant_build_runs_end_to_end(self):
		archive = Path(os.environ[NATIVE_ARCHIVE_ENV])
		manifest = Path(os.environ[NATIVE_MANIFEST_ENV])
		before = {path: variant_build.sha256_file(path) for path in (archive, manifest)}
		inputs = variant_build.VariantInputs(
			Path(os.environ[VSRO_ROOT_ENV]), self.staging, "variant-smoke",
			[archive, manifest], variant_build.native_mapinfo_path(),
			monster_database=Path(os.environ[variant_build.MONSTER_DATABASE_ENV]))
		env = dict(os.environ)
		for flag in (*variant_build.SHARD_FILES, variant_build.MONSTERS_FLAG, variant_build.QINSHI_FLAG):
			env[flag] = "on"
		built = variant_build.build(inputs, env=env)
		self.assertTrue(built.is_file())
		textdata = self.staging / "server-data" / "textdata"
		overlay = json.loads((textdata / variant_build.OVERLAY_NAME).read_text(encoding="utf-8"))
		self.assertEqual(overlay.get("format"), variant_build.OVERLAY_FORMAT)
		for name in ("cap110-monsters.txt", variant_build.QINSHI_FILE, *variant_build.SHARD_FILES.values()):
			self.assertTrue((textdata / name).is_file(), name)
		self.assertEqual(before, {path: variant_build.sha256_file(path) for path in before})


if __name__ == "__main__":
	unittest.main()
