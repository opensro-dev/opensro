"""
===========================================================================

test_variant_build.py - the variant staging keeps the native bytes

Fake steps stand in for the four builds (no game data): the step order
and environments, the overlay the variant must hold, and the refusal of
any drift in the native artifacts the build was shown.

===========================================================================
"""
import json
from pathlib import Path
import tempfile
import unittest

import variant_build


# ================
# FakeRun
#
# Records each step and lays down the outputs the real step would: the
# isolated import, the variant textdata with one cap110 overlay, and the
# bundle with its overlay projection. Defects are opt-in per test.
# ================
class FakeRun:
	def __init__(self, staging, defect=None, native_manifest=None):
		self.staging = staging
		self.defect = defect
		self.native_manifest = native_manifest
		self.calls = []
		self.shards = {}

	def __call__(self, argv, env):
		self.calls.append((argv, env))
		tool = Path(argv[1]).name
		if "import_vsro_alexandria" in tool:
			(self.staging / "stage1" / "extracted" / "Alexandria_extracted").mkdir(parents=True)
		elif "build_alexandria_variant" in tool:
			# The bundle step projects what this step's settings produced.
			self.shards = {flag: env.get(flag) for flag in variant_build.SHARD_FILES}
			textdata = self.staging / "variant" / variant_build.TEXTDATA_DIR
			textdata.mkdir(parents=True)
			(textdata / "cap110-monsters.txt").write_text("rows", encoding="utf-8")
		elif "task.mjs" in tool:
			(self.staging / "variant" / ".generated").mkdir(parents=True, exist_ok=True)
		elif "buildServerGameDataBundle" in tool:
			textdata = self.staging / "server-data" / "textdata"
			textdata.mkdir(parents=True)
			if self.defect != "no-overlay":
				(textdata / "alexandria-overlay.json").write_text(
					json.dumps({"format": variant_build.OVERLAY_FORMAT}), encoding="utf-8")
			if self.defect != "no-cap110":
				(textdata / "cap110-monsters.txt").write_text("rows", encoding="utf-8")
			for flag, shard in variant_build.SHARD_FILES.items():
				asked = self.shards.get(flag) == "on"
				if asked and self.defect != "drop-shard" or not asked and self.defect == "extra-shard":
					(textdata / shard).write_text("rows", encoding="utf-8")
			(self.staging / "server-data.srogz").write_bytes(b"archive")
			if self.defect == "touch-native":
				self.native_manifest.write_bytes(b"rewritten")


class VariantBuildTests(unittest.TestCase):
	def setUp(self):
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		base = Path(self.root.name)
		self.staging = base / "staging"
		self.archive = base / "native.srogz"
		self.archive.write_bytes(b"native archive")
		self.manifest = base / "manifest.json"
		self.manifest.write_bytes(b"native manifest")
		self.native = [self.archive, self.manifest]

	def build(self, run, env=None):
		return variant_build.build(base_vsro(), self.staging, "variant-1", self.native, run=run,
			env={} if env is None else env)

	def test_the_four_steps_run_in_order_against_the_variant(self):
		run = FakeRun(self.staging)
		archive = self.build(run)
		self.assertEqual(archive, self.staging / "server-data.srogz")
		tools = [call[0][1] for call in run.calls]
		self.assertIn("import_vsro_alexandria", tools[0])
		self.assertIn("build_alexandria_variant", tools[1])
		self.assertIn("task.mjs", tools[2])
		self.assertIn("buildServerGameDataBundle", tools[3])
		self.assertEqual(run.calls[0][1]["SRO_GAME_ROOT"], str(self.staging / "stage1"))
		self.assertEqual(run.calls[0][1]["SRO_ALEXANDRIA"], "1")
		for call in run.calls[2:]:
			self.assertEqual(call[1]["SRO_GAME_ROOT"], str(self.staging / "variant"))
			self.assertEqual(call[1]["SRO_GENERATED_ROOT"], str(self.staging / "variant" / ".generated"))

	def test_a_native_change_refuses_the_staging(self):
		run = FakeRun(self.staging, defect="touch-native", native_manifest=self.manifest)
		with self.assertRaisesRegex(ValueError, "changed during the variant build"):
			self.build(run)

	def test_a_missing_native_artifact_stops_before_any_step(self):
		run = FakeRun(self.staging)
		with self.assertRaisesRegex(ValueError, "not found"):
			variant_build.build(base_vsro(), self.staging, "variant-1",
				[self.archive, Path(self.root.name) / "absent.json"], run=run)
		self.assertEqual(run.calls, [])

	def test_a_bundle_without_the_overlay_is_refused(self):
		run = FakeRun(self.staging, defect="no-overlay")
		with self.assertRaisesRegex(ValueError, "holds no"):
			self.build(run)

	def test_a_bundle_missing_a_cap110_overlay_is_refused(self):
		run = FakeRun(self.staging, defect="no-cap110")
		with self.assertRaisesRegex(ValueError, "missing cap110 overlays"):
			self.build(run)

	def test_staging_must_be_fresh(self):
		self.staging.mkdir(parents=True)
		(self.staging / "leftover").write_bytes(b"x")
		run = FakeRun(self.staging)
		with self.assertRaisesRegex(ValueError, "is not empty"):
			self.build(run)
		self.assertEqual(run.calls, [])

	def test_the_variant_step_receives_the_settings_normalized(self):
		run = FakeRun(self.staging)
		self.build(run, env={"SRO_CAP110_ITEMS": "1"})
		step = run.calls[1][1]
		self.assertEqual(step["SRO_CAP110_ITEMS"], "on")
		self.assertEqual(step["SRO_CAP110_SKILLS"], "off")
		self.assertTrue((self.staging / "server-data" / "textdata" / "itemdata_110000.txt").is_file())
		self.assertFalse((self.staging / "server-data" / "textdata" / "skilldata_110000.txt").exists())

	def test_a_shard_the_settings_did_not_ask_for_is_refused(self):
		run = FakeRun(self.staging, defect="extra-shard")
		with self.assertRaisesRegex(ValueError, "disagrees with SRO_CAP110_ITEMS=off"):
			self.build(run)

	def test_a_missing_shard_the_settings_asked_for_is_refused(self):
		run = FakeRun(self.staging, defect="drop-shard")
		with self.assertRaisesRegex(ValueError, "disagrees with SRO_CAP110_ITEMS=on"):
			self.build(run, env={"SRO_CAP110_ITEMS": "on"})

	def test_a_native_bundle_with_an_overlay_is_refused(self):
		native_data = Path(self.root.name) / "native-data"
		(native_data / "textdata").mkdir(parents=True)
		(native_data / "textdata" / "alexandria-overlay.json").write_text("{}", encoding="utf-8")
		run = FakeRun(self.staging)
		with self.assertRaisesRegex(ValueError, "is not native"):
			variant_build.build(base_vsro(), self.staging, "variant-1", self.native, native_data, run=run)
		self.assertEqual(run.calls, [])


# ================
# base_vsro
#
# The tests never read a client; the folder only names the argument.
# ================
def base_vsro():
	return Path("vsro-client")


if __name__ == "__main__":
	unittest.main()
