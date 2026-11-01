"""
===========================================================================

test_variant_build.py - the variant staging keeps the native bytes

Fake steps stand in for the four builds (no game data): the step order
and environments, the overlay the variant must hold, the monster
setting and its database, and the refusal of any drift in the native
artifacts the build was shown (the native mapinfo among them).

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
# isolated import, the variant textdata (one cap110 overlay when the
# monster setting is on), and the bundle with its overlay projection.
# Defects are opt-in per test.
# ================
class FakeRun:
	def __init__(self, staging, defect=None, native_manifest=None):
		self.staging = staging
		self.defect = defect
		self.native_manifest = native_manifest
		self.calls = []
		self.shards = {}
		self.monsters = False
		self.qinshi = False

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
			self.monsters = env.get(variant_build.MONSTERS_FLAG) == "on"
			if self.monsters and self.defect != "no-variant-overlay":
				(textdata / "cap110-monsters.txt").write_text("rows", encoding="utf-8")
			self.qinshi = env.get(variant_build.QINSHI_FLAG) == "on"
			if self.qinshi and self.defect != "no-qinshi-variant":
				(textdata / variant_build.QINSHI_FILE).write_text("rows", encoding="utf-8")
		elif "task.mjs" in tool:
			(self.staging / "variant" / ".generated").mkdir(parents=True, exist_ok=True)
		elif "buildServerGameDataBundle" in tool:
			textdata = self.staging / "server-data" / "textdata"
			textdata.mkdir(parents=True)
			if self.defect != "no-overlay":
				(textdata / "alexandria-overlay.json").write_text(
					json.dumps({"format": variant_build.OVERLAY_FORMAT}), encoding="utf-8")
			if self.monsters and self.defect not in ("no-cap110", "no-variant-overlay"):
				(textdata / "cap110-monsters.txt").write_text("rows", encoding="utf-8")
			variant_file = self.staging / "variant" / variant_build.TEXTDATA_DIR / variant_build.QINSHI_FILE
			if variant_file.is_file() and self.defect != "no-qinshi" or self.defect == "extra-qinshi":
				(textdata / variant_build.QINSHI_FILE).write_text("rows", encoding="utf-8")
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
		self.mapinfo = base / "mapinfo.mfo"
		self.mapinfo.write_bytes(b"native mapinfo")
		self.database = base / "monster-db"

	def inputs(self, **changes):
		inputs = variant_build.VariantInputs(base_vsro(), self.staging, "variant-1", self.native, self.mapinfo)
		for name, value in changes.items():
			setattr(inputs, name, value)
		return inputs

	def build(self, run, env=None, **changes):
		return variant_build.build(self.inputs(**changes), run=run, env={} if env is None else env)

	def monsters_on(self):
		return {variant_build.MONSTERS_FLAG: "on"}

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
		argv = run.calls[0][0]
		self.assertEqual(argv[argv.index("--native-mapinfo") + 1], str(self.mapinfo))
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
			self.build(run, native_files=[self.archive, Path(self.root.name) / "absent.json"])
		self.assertEqual(run.calls, [])

	def test_a_missing_native_mapinfo_stops_before_any_step(self):
		run = FakeRun(self.staging)
		with self.assertRaisesRegex(ValueError, "mapinfo.mfo not found"):
			self.build(run, native_mapinfo=Path(self.root.name) / "absent" / "mapinfo.mfo")
		self.assertEqual(run.calls, [])

	def test_a_native_mapinfo_change_refuses_the_staging(self):
		run = FakeRun(self.staging, defect="touch-native", native_manifest=self.mapinfo)
		with self.assertRaisesRegex(ValueError, "changed during the variant build: .*mapinfo"):
			self.build(run)

	def test_monsters_on_without_a_database_stops_before_any_step(self):
		run = FakeRun(self.staging)
		with self.assertRaisesRegex(ValueError, "needs --monster-database"):
			self.build(run, env=self.monsters_on())
		self.assertEqual(run.calls, [])

	def test_monsters_on_hands_the_database_to_the_import_and_the_variant(self):
		run = FakeRun(self.staging)
		self.build(run, env=self.monsters_on(), monster_database=self.database)
		for call in run.calls[:2]:
			self.assertEqual(call[1][variant_build.MONSTERS_FLAG], "on")
			self.assertEqual(call[1][variant_build.MONSTER_DATABASE_ENV], str(self.database))
		self.assertTrue((self.staging / "server-data" / "textdata" / "cap110-monsters.txt").is_file())

	def test_monsters_off_blanks_a_stale_database(self):
		run = FakeRun(self.staging)
		self.build(run, env={variant_build.MONSTER_DATABASE_ENV: "stale"}, monster_database=self.database)
		for call in run.calls[:2]:
			self.assertEqual(call[1][variant_build.MONSTERS_FLAG], "off")
			self.assertEqual(call[1][variant_build.MONSTER_DATABASE_ENV], "")

	def test_monsters_off_needs_no_cap110_overlay(self):
		run = FakeRun(self.staging)
		self.build(run)
		self.assertEqual(list((self.staging / "server-data" / "textdata").glob("cap110-*.txt")), [])

	def test_monsters_on_without_a_variant_overlay_is_refused(self):
		run = FakeRun(self.staging, defect="no-variant-overlay")
		with self.assertRaisesRegex(ValueError, "holds no cap110-\*.txt overlay"):
			self.build(run, env=self.monsters_on(), monster_database=self.database)

	def test_a_bundle_without_the_overlay_is_refused(self):
		run = FakeRun(self.staging, defect="no-overlay")
		with self.assertRaisesRegex(ValueError, "holds no"):
			self.build(run)

	def test_a_bundle_missing_a_cap110_overlay_is_refused(self):
		run = FakeRun(self.staging, defect="no-cap110")
		with self.assertRaisesRegex(ValueError, "missing cap110 overlays"):
			self.build(run, env=self.monsters_on(), monster_database=self.database)

	def test_qinshi_on_without_monsters_stops_before_any_step(self):
		run = FakeRun(self.staging)
		with self.assertRaisesRegex(ValueError, "SRO_CAP110_QINSHI=on needs SRO_CAP110_MONSTERS=on"):
			self.build(run, env={variant_build.QINSHI_FLAG: "on"})
		self.assertEqual(run.calls, [])

	def test_qinshi_on_hands_the_family_to_the_steps_and_ships_its_nest_file(self):
		run = FakeRun(self.staging)
		env = {**self.monsters_on(), variant_build.QINSHI_FLAG: "1"}
		self.build(run, env=env, monster_database=self.database)
		for call in run.calls[:2]:
			self.assertEqual(call[1][variant_build.QINSHI_FLAG], "on")
		self.assertTrue((self.staging / "server-data" / "textdata" / variant_build.QINSHI_FILE).is_file())

	def test_qinshi_on_without_a_variant_nest_file_is_refused(self):
		run = FakeRun(self.staging, defect="no-qinshi-variant")
		env = {**self.monsters_on(), variant_build.QINSHI_FLAG: "on"}
		with self.assertRaisesRegex(ValueError, "disagrees with SRO_CAP110_QINSHI=on"):
			self.build(run, env=env, monster_database=self.database)

	def test_a_qinshi_overlay_the_settings_did_not_ask_for_is_refused(self):
		run = FakeRun(self.staging, defect="extra-qinshi")
		with self.assertRaisesRegex(ValueError, "disagrees with SRO_CAP110_QINSHI=off"):
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
		self.assertEqual(step["SRO_CAP110_MONSTERS"], "off")
		self.assertEqual(step["SRO_CAP110_QINSHI"], "off")
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
			self.build(run, native_server_data=native_data)
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
