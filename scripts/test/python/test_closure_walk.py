"""
===========================================================================

test_closure_walk.py - the one resource closure walk (#4757)

cap110_item_import.walk_closure serves the equipment, world layer, pet
and Sky Temple family importers. Before #4757 two copies of the loop each
held rules the other lacked; these tests pin the union under every policy:
a bare particle name and a sound-root path resolve for every importer, an
unknown archive stops the walk, KEEP never rewrites a held file, and each
gap policy records or refuses exactly its misses. The audit is injected,
and later-archive entries carry their own bytes (payload is injected too).

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import cap110_item_import as walk  # noqa: E402

STRICT = walk.closure_policy()
SKY = walk.closure_policy(walk.WALK_EVERYTHING, walk.CLOSURE_KEEP, walk.CLOSURE_GAPS_MEDIA)
TOLERANT = walk.closure_policy(walk.WALK_EVERYTHING, walk.CLOSURE_KEEP, walk.CLOSURE_GAPS_ALL)


# ================
# ClosureWalkTests
# ================
class ClosureWalkTests(unittest.TestCase):

	# ================
	# setUp
	#
	# A scratch native tree and output tree; archives hold later files as
	# {path: bytes}, and the audit answers from self.graph by path.
	# ================
	def setUp(self):
		self.folder = tempfile.TemporaryDirectory()
		self.root = Path(self.folder.name)
		self.native, self.output = self.root / "native", self.root / "out"
		self.native.mkdir()
		self.output.mkdir()
		self.graph, self.unparsable = {}, set()
		self.archives = {"Data": (None, {}), "Particles": (None, {})}

	def tearDown(self):
		self.folder.cleanup()

	def place(self, tree, archive, name, raw):
		path = tree / walk.FOLDERS[archive] / name
		path.parent.mkdir(parents=True, exist_ok=True)
		path.write_bytes(raw)

	def audit(self, requests):
		if any(request["path"] in self.unparsable for request in requests):
			raise ValueError("unparsable")
		return [self.graph.get(request["path"], []) for request in requests]

	def fresh_output(self):
		shutil.rmtree(self.output)
		self.output.mkdir()

	def run_walk(self, seeds, policy):
		with patch.object(walk, "resource_dependencies", side_effect=self.audit), \
				patch.object(walk.sro_pk2, "payload", side_effect=lambda data, entry: entry):
			return walk.walk_closure(seeds, self.native, self.output, self.archives, policy)

	# ================
	# test_a_sound_root_path_resolves_for_every_policy
	#
	# A BSR authoring sound\prim\snd\... names the archive's prim/snd/...
	# file; the equipment walk lacked this rule before #4757.
	# ================
	def test_a_sound_root_path_resolves_for_every_policy(self):
		self.archives["Data"][1].update({"res/mob.bsr": b"model", "prim/snd/roar.wav": b"roar"})
		self.graph["res/mob.bsr"] = ["sound/prim/snd/roar.wav"]
		for policy in (STRICT, SKY):
			with self.subTest(policy=policy["gaps"]):
				self.fresh_output()
				result = self.run_walk({("Data", "res/mob.bsr")}, policy)
				self.assertIn("Data_extracted/prim/snd/roar.wav", result["files"])
				self.assertEqual((self.output / "Data_extracted/prim/snd/roar.wav").read_bytes(), b"roar")

	# ================
	# test_a_bare_particle_name_resolves_for_every_policy
	#
	# An effect program naming orb.bms with no folder means the one
	# Particles path ending in it; the Sky Temple walk lacked this rule.
	# Two candidates refuse under RAISE and are a recorded miss otherwise.
	# ================
	def test_a_bare_particle_name_resolves_for_every_policy(self):
		self.archives["Particles"][1].update({"system/a.efp": b"effect", "geometry/orb.bms": b"mesh"})
		self.graph["system/a.efp"] = [{"archive": "Particles", "path": "orb.bms"}]
		for policy in (STRICT, SKY):
			with self.subTest(policy=policy["gaps"]):
				self.fresh_output()
				result = self.run_walk({("Particles", "system/a.efp")}, policy)
				self.assertIn("Particles_extracted/geometry/orb.bms", result["files"])
		self.place(self.native, "Particles", "other/orb.bms", b"second")
		with self.assertRaisesRegex(ValueError, "ambiguous or missing particle resource: orb.bms"):
			self.run_walk({("Particles", "system/a.efp")}, STRICT)
		result = self.run_walk({("Particles", "system/a.efp")}, TOLERANT)
		self.assertEqual(result["missingFiles"], ["Particles:orb.bms"])

	# ================
	# test_an_archive_the_walk_was_not_given_stops_it
	# ================
	def test_an_archive_the_walk_was_not_given_stops_it(self):
		self.archives["Data"][1]["res/mob.bsr"] = b"model"
		self.graph["res/mob.bsr"] = [{"archive": "Music", "path": "theme.ogg"}]
		for policy in (STRICT, SKY, TOLERANT):
			with self.subTest(policy=policy["gaps"]):
				with self.assertRaisesRegex(ValueError, "resource closure left the later archives"):
					self.run_walk({"res/mob.bsr"}, policy)

	# ================
	# test_keep_parses_held_files_and_rewrite_copies_them
	#
	# KEEP walks through a native file and an earlier import's file but
	# writes neither; REWRITE (the standalone equipment overlay) copies
	# the native one and ignores what the output already held.
	# ================
	def test_keep_parses_held_files_and_rewrite_copies_them(self):
		self.place(self.native, "Data", "res/mob.bsr", b"native")
		self.place(self.output, "Data", "res/earlier.bmt", b"earlier")
		self.archives["Data"][1].update({"res/earlier.bmt": b"later", "res/skin.ddj": b"texture"})
		self.graph.update({"res/mob.bsr": ["res/earlier.bmt"], "res/earlier.bmt": ["res/skin.ddj"]})
		kept = self.run_walk({"res/mob.bsr"}, SKY)
		self.assertEqual(kept["files"], ["Data_extracted/res/skin.ddj"])
		self.assertEqual([record["source"] for record in kept["records"]], ["native", "output", "later"])
		self.assertEqual((self.output / "Data_extracted/res/earlier.bmt").read_bytes(), b"earlier")
		rewritten = self.run_walk({"res/mob.bsr"}, STRICT)
		self.assertEqual([record["source"] for record in rewritten["records"]], ["native", "later", "later"])
		self.assertEqual((self.output / "Data_extracted/res/mob.bsr").read_bytes(), b"native")
		self.assertEqual((self.output / "Data_extracted/res/earlier.bmt").read_bytes(), b"later")

	# ================
	# test_each_gap_policy_records_or_refuses_its_misses
	#
	# RAISE stops at any miss; MEDIA records a missing sound or effect
	# program and stops at a missing mesh; ALL records the mesh and an
	# unparsable model too.
	# ================
	def test_each_gap_policy_records_or_refuses_its_misses(self):
		self.archives["Data"][1].update({"res/mob.bsr": b"model", "res/broken.bsr": b"bad"})
		self.graph["res/mob.bsr"] = ["res/cry.wav", "res/appear.efp"]
		with self.assertRaisesRegex(ValueError, "missing Data resource: res/appear.efp"):
			self.run_walk({"res/mob.bsr"}, STRICT)
		media = self.run_walk({"res/mob.bsr"}, SKY)
		self.assertEqual((media["missingSounds"], media["missingEffects"]), (["res/cry.wav"], ["res/appear.efp"]))
		self.graph["res/mob.bsr"].append("res/body.bms")
		with self.assertRaisesRegex(ValueError, "missing Data resource: res/body.bms"):
			self.run_walk({"res/mob.bsr"}, SKY)
		self.unparsable.add("res/broken.bsr")
		tolerant = self.run_walk({"res/mob.bsr", "res/broken.bsr"}, TOLERANT)
		self.assertEqual(tolerant["missingFiles"], ["Data:res/body.bms"])
		self.assertEqual(tolerant["unparsedFiles"], ["res/broken.bsr"])


if __name__ == "__main__":
	unittest.main()
