"""
===========================================================================

test_variant_steps.py - a variant importer replays only what it would write

variant_steps.StepRunner folds steps over a content tree. A step whose
record still holds is replayed instead of run, and the tree it leaves must
be the tree running it would have left: the same files, bytes and times,
the removed files and trees gone, the same value. A changed read, a moved
chain or a step the hooks cannot see in full reruns.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import shutil
import tempfile
import unittest
from pathlib import Path

import variant_steps  # noqa: E402
from sro_paths import REPO_ROOT  # noqa: E402

INPUTS = {"code": "ignored", "env": {}, "arguments": {}, "sources": {}}


# ================
# snapshot
#
# Every file under root: its bytes and mtime, by relative path.
# ================
def snapshot(root: Path) -> dict:
	found = {}
	for directory, _, names in os.walk(root):
		for name in names:
			path = Path(directory, name)
			found[path.relative_to(root).as_posix()] = (path.read_bytes(), path.stat().st_mtime_ns)
	return found


class StepRunnerTests(unittest.TestCase):
	def setUp(self):
		self.variant = Path(self.enterContext(tempfile.TemporaryDirectory()))
		self.out = self.variant / "extracted"
		self.lines = []

	def seed(self):
		if self.out.exists():
			shutil.rmtree(self.out)
		(self.out / "Data" / "old").mkdir(parents=True)
		(self.out / "Data" / "table.txt").write_text("native\n", encoding="utf-8")
		(self.out / "Data" / "drop.txt").write_text("gone soon\n", encoding="utf-8")
		(self.out / "Data" / "old" / "stale.bin").write_bytes(b"\0" * 8)

	def runner(self, inputs=INPUTS):
		return variant_steps.StepRunner(self.out, variant_steps.StepStore(self.variant), inputs, self.lines.append)

	def steps(self, calls):
		out = self.out

		def append():
			calls.append("append")
			table = out / "Data" / "table.txt"
			table.write_text(table.read_text(encoding="utf-8") + "row\n", encoding="utf-8")
			(out / "Data" / "drop.txt").unlink()
			shutil.rmtree(out / "Data" / "old")
			(out / "Media" / "new").mkdir(parents=True)
			(out / "Media" / "new" / "a.dat").write_bytes(b"abc")
			return {"rows": 1, 7: b"\x01\x02", "pair": (1, "x"), "path": Path("p")}

		def count():
			calls.append("count")
			return len((out / "Data" / "table.txt").read_text(encoding="utf-8").splitlines())

		return [variant_steps.Step("append", append), variant_steps.Step("count", count)]

	def fold(self, inputs=INPUTS):
		calls = []
		self.seed()
		runner = self.runner(inputs)
		values = [runner.take(step) for step in self.steps(calls)]
		runner.store.prune()
		return calls, values, snapshot(self.out)

	def test_a_replay_leaves_the_tree_running_would(self):
		calls, values, ran = self.fold()
		self.assertEqual(calls, ["append", "count"])
		calls, replayed_values, replayed = self.fold()
		self.assertEqual(calls, [])
		self.assertEqual(replayed, ran)
		self.assertEqual(replayed_values, values)
		self.assertNotIn("Data/drop.txt", replayed)
		self.assertNotIn("Data/old/stale.bin", replayed)
		self.assertEqual(values[0][7], b"\x01\x02")
		self.assertEqual(values[0]["pair"], (1, "x"))
		self.assertEqual(values[1], 2)

	def test_changed_inputs_run_every_step_again(self):
		self.fold()
		calls, _, _ = self.fold({**INPUTS, "env": {"SRO_FLAG": "on"}})
		self.assertEqual(calls, ["append", "count"])

	def test_a_rerun_with_the_same_delta_keeps_the_chain(self):
		self.fold()
		store = variant_steps.StepStore(self.variant)
		for record in (store.root / "records").iterdir():
			text = record.read_text(encoding="utf-8")
			if '"step":"append"' in text:
				record.unlink()
		calls, _, _ = self.fold()
		# append reran (no record) and wrote what it wrote before, so count,
		# whose record follows the same chain, replays.
		self.assertEqual(calls, ["append"])

	def test_a_changed_repository_read_reruns_the_step(self):
		data = Path(self.enterContext(tempfile.TemporaryDirectory(dir=REPO_ROOT / "temp" if (REPO_ROOT / "temp").is_dir() else REPO_ROOT)))
		pin = data / "pin.json"
		pin.write_text("1", encoding="utf-8")
		calls = []

		def reads():
			calls.append("reads")
			return pin.read_text(encoding="utf-8")

		for expected, text in ((["reads"], "1"), ([], "1"), (["reads"], "2")):
			pin.write_text(text, encoding="utf-8")
			calls.clear()
			self.seed()
			value = self.runner().take(variant_steps.Step("reads", reads))
			self.assertEqual(calls, expected)
			self.assertEqual(value, text)

	def test_renames_bytecode_and_descriptors_are_recorded_or_ignored(self):
		# A real importer writes atomically (a rename), may import a module
		# for the first time (its bytecode cache) and writes through a
		# descriptor; none of these may make the step unrecordable.
		out, cache = self.out, self.variant.parent / "__pycache__"
		calls = []

		def atomic():
			calls.append("atomic")
			partial = out / "Data" / "table.txt.partial"
			with open(os.open(partial, os.O_WRONLY | os.O_CREAT | os.O_TRUNC), "wb") as handle:
				handle.write(b"atomic\n")
			os.replace(partial, out / "Data" / "table.txt")
			cache.mkdir(exist_ok=True)
			(cache / "module.cpython-312.pyc.123").write_bytes(b"cache")

		for _ in range(2):
			self.seed()
			self.runner().take(variant_steps.Step("atomic", atomic))
		self.assertEqual(calls, ["atomic"])
		self.assertEqual((out / "Data" / "table.txt").read_bytes(), b"atomic\n")
		self.assertFalse((out / "Data" / "table.txt.partial").exists())
		shutil.rmtree(cache, ignore_errors=True)

	def test_a_write_outside_the_tree_is_never_replayed(self):
		elsewhere = self.variant / "manifest.json"
		calls = []

		def outside():
			calls.append("outside")
			elsewhere.write_text("{}", encoding="utf-8")

		for _ in range(2):
			self.seed()
			self.runner().take(variant_steps.Step("outside", outside))
		self.assertEqual(calls, ["outside", "outside"])
		self.assertTrue(any("not recorded" in line for line in self.lines))

	def test_an_unencodable_value_is_never_replayed(self):
		calls = []

		def odd():
			calls.append("odd")
			return {1, 2}

		for _ in range(2):
			self.seed()
			self.assertEqual(self.runner().take(variant_steps.Step("odd", odd)), {1, 2})
		self.assertEqual(calls, ["odd", "odd"])

	def test_a_missing_blob_reruns_the_step(self):
		self.fold()
		shutil.rmtree(variant_steps.StepStore(self.variant).root / "blobs")
		calls, _, _ = self.fold()
		self.assertEqual(calls, ["append"])


class ValueCodecTests(unittest.TestCase):
	def test_values_round_trip_exactly(self):
		value = {"a": [1, 2.5, None, True], "b": (b"\xff", Path("x/y")), 3: {"$odd": 1}}
		self.assertEqual(variant_steps.decode_value(variant_steps.encode_value(value)), value)


if __name__ == "__main__":
	unittest.main()
