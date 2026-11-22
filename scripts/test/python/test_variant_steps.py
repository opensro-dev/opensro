"""
===========================================================================

test_variant_steps.py - an importer reruns only on its own change

The variant journal keeps one recorded effect per step (variant_steps.py,
#3993): a build over an unchanged world replays every effect byte for
byte and runs no importer; a source one step reads reruns that step, and
a step downstream reruns only when the effect it consumes changed; a flag
a step's code names reruns only that step; a change in a Node script a
step's module names, or in what that script imports, reruns only its
step; a tree file tampered with
between builds is repaired by replay, not by rerunning the importer; a
step whose summary cannot be recorded runs on every build instead of
breaking it; a build that dies leaves no journal behind.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import importlib
import os
import sys
import tempfile
import unittest
from pathlib import Path

import variant_settle
import variant_steps


# The suffix of the Node scripts a step can run, assembled: the code
# closure scans a Python file's literals for script names, and this
# file is itself a step's seed in these tests, so its own text names
# none.
MJS = ".m" + "js"


# ================
# run_build
#
# One whole variant build over the registered steps, the way
# build_variant drives the runner: register, load, begin, take every
# result in order, finish.
# ================
def run_build(variant: Path, steps: list, roots=(), repository_root=None):
	(variant / "extracted").mkdir(parents=True, exist_ok=True)
	runner = variant_steps.Runner(variant, roots, repository_root=repository_root)
	for name, body, code_from in steps:
		runner.register(name, body, code_from=code_from)
	runner.load({"variant": str(variant)})
	runner.begin()
	results = [runner.result(index) for index in range(len(steps))]
	runner.finish()
	return results


class VariantSteps(unittest.TestCase):

	def setUp(self):
		self.temp = tempfile.TemporaryDirectory()
		self.variant = Path(self.temp.name) / "variant"

	def tearDown(self):
		self.temp.cleanup()

	# ================
	# write_step
	#
	# A counted step body writing one file under extracted/.
	# ================
	def write_step(self, counters: dict, name: str, filename: str, data: bytes, result):
		def body():
			counters[name] = counters.get(name, 0) + 1
			target = self.variant / "extracted" / filename
			target.parent.mkdir(parents=True, exist_ok=True)
			target.write_bytes(data)
			return result

		return (name, body, body)

	# ================
	# test_unchanged_build_replays_every_step
	# ================
	def test_unchanged_build_replays_every_step(self):
		counters: dict = {}

		def first_body():
			counters["first"] = counters.get("first", 0) + 1
			out = self.variant / "extracted"
			(out / "a.txt").write_bytes(b"one")
			(out / "b.txt").write_bytes(b"two")
			return {"sum": 3}

		def second_body():
			counters["second"] = counters.get("second", 0) + 1
			out = self.variant / "extracted"
			(out / "b.txt").unlink()
			(out / "c.txt").write_bytes((out / "a.txt").read_bytes() + b"!")
			return {"done": True}

		steps = [("first", first_body, first_body), ("second", second_body, second_body)]
		results = run_build(self.variant, steps)
		tree = self.variant / "extracted"
		self.assertEqual((tree / "a.txt").read_bytes(), b"one")
		self.assertFalse((tree / "b.txt").exists())
		self.assertEqual((tree / "c.txt").read_bytes(), b"one!")

		again = run_build(self.variant, steps)
		self.assertEqual(again, results)
		self.assertEqual(counters, {"first": 1, "second": 1})
		self.assertEqual((tree / "c.txt").read_bytes(), b"one!")
		self.assertFalse((tree / "b.txt").exists())

	# ================
	# test_source_change_reruns_the_reader_and_changed_downstream_only
	# ================
	def test_source_change_reruns_the_reader_and_changed_downstream_only(self):
		counters: dict = {}
		source = Path(self.temp.name) / "sources"
		source.mkdir()
		(source / "s.txt").write_bytes(b"aa")

		def reader():
			counters["reader"] = counters.get("reader", 0) + 1
			data = (source / "s.txt").read_bytes()
			(self.variant / "extracted" / "out.txt").write_bytes(data.upper())
			return {"length": len(data)}

		def downstream():
			counters["downstream"] = counters.get("downstream", 0) + 1
			data = (self.variant / "extracted" / "out.txt").read_bytes()
			(self.variant / "extracted" / "d.txt").write_bytes(b"down:" + data)
			return {"echo": data.decode("ascii")}

		steps = [("reader", reader, reader), ("downstream", downstream, downstream)]
		roots = (source,)
		run_build(self.variant, steps, roots)
		# A change the reader's effect does not show: the reader reruns,
		# the downstream step replays.
		(source / "s.txt").write_bytes(b"AA")
		run_build(self.variant, steps, roots)
		self.assertEqual(counters, {"reader": 2, "downstream": 1})
		self.assertEqual((self.variant / "extracted" / "d.txt").read_bytes(), b"down:AA")
		# A change the effect does show: the downstream step reruns too.
		(source / "s.txt").write_bytes(b"bb")
		run_build(self.variant, steps, roots)
		self.assertEqual(counters, {"reader": 3, "downstream": 2})
		self.assertEqual((self.variant / "extracted" / "d.txt").read_bytes(), b"down:BB")

	# ================
	# test_flag_change_reruns_only_steps_naming_it
	# ================
	def test_flag_change_reruns_only_steps_naming_it(self):
		modules = Path(self.temp.name) / "stepmods"
		modules.mkdir()
		(modules / "flagged_step.py").write_text(
			"CALLS = {'n': 0}\n\n\n"
			"def run(variant):\n"
			"\timport os\n"
			"\tCALLS['n'] += 1\n"
			"\tout = variant / 'extracted'\n"
			"\tout.mkdir(parents=True, exist_ok=True)\n"
			"\t(out / 'flagged.txt').write_bytes(b'f')\n"
			"\treturn {'on': bool(os.environ.get('SRO_FLAG_A'))}\n",
			encoding="utf-8",
		)
		(modules / "plain_step.py").write_text(
			"CALLS = {'n': 0}\n\n\n"
			"def run(variant):\n"
			"\tCALLS['n'] += 1\n"
			"\tout = variant / 'extracted'\n"
			"\tout.mkdir(parents=True, exist_ok=True)\n"
			"\t(out / 'plain.txt').write_bytes(b'p')\n"
			"\treturn {'on': False}\n",
			encoding="utf-8",
		)
		sys.path.insert(0, str(modules))
		try:
			flagged = importlib.import_module("flagged_step")
			plain = importlib.import_module("plain_step")
		finally:
			sys.path.remove(str(modules))
		steps = [
			("flagged", lambda: flagged.run(self.variant), flagged.run),
			("plain", lambda: plain.run(self.variant), plain.run),
		]
		saved = os.environ.pop("SRO_FLAG_A", None)
		try:
			run_build(self.variant, steps)
			os.environ["SRO_FLAG_A"] = "1"
			run_build(self.variant, steps)
		finally:
			os.environ.pop("SRO_FLAG_A", None)
			if saved is not None:
				os.environ["SRO_FLAG_A"] = saved
		self.assertEqual(flagged.CALLS["n"], 2)
		self.assertEqual(plain.CALLS["n"], 1)

	# ================
	# test_subprocess_script_change_reruns_only_its_step
	#
	# A step whose module names a Node script (the items importer runs
	# one) is keyed on that script and everything it imports: a change
	# in the script's own import reruns its step and no other.
	# ================
	def test_subprocess_script_change_reruns_only_its_step(self):
		repo = Path(self.temp.name) / "repo"
		scripts = repo / "scripts"
		scripts.mkdir(parents=True)
		(scripts / "stepmod.py").write_text(
			"CALLS = {'n': 0}\n"
			f"SCRIPT = \"worker{MJS}\"\n\n\n"
			"def run(variant):\n"
			"\tCALLS['n'] += 1\n"
			"\tout = variant / 'extracted'\n"
			"\tout.mkdir(parents=True, exist_ok=True)\n"
			"\t(out / 'scripted.txt').write_bytes(b's')\n"
			"\treturn {'script': SCRIPT}\n",
			encoding="utf-8",
		)
		(scripts / f"worker{MJS}").write_text(
			f"import {{ value }} from \"./helper{MJS}\";\nexport const seen = value;\n", encoding="utf-8"
		)
		(scripts / f"helper{MJS}").write_text("export const value = 1;\n", encoding="utf-8")
		sys.path.insert(0, str(scripts))
		try:
			stepmod = importlib.import_module("stepmod")
		finally:
			sys.path.remove(str(scripts))
		counters: dict = {}
		plain = self.write_step(counters, "plain", "plain.txt", b"p", {"p": 1})
		steps = [
			("scripted", lambda: stepmod.run(self.variant), stepmod.run),
			plain,
		]
		try:
			run_build(self.variant, steps, repository_root=repo)
			run_build(self.variant, steps, repository_root=repo)
			self.assertEqual(stepmod.CALLS["n"], 1)
			(scripts / f"helper{MJS}").write_text("export const value = 2;\n", encoding="utf-8")
			run_build(self.variant, steps, repository_root=repo)
		finally:
			sys.modules.pop("stepmod", None)
		self.assertEqual(stepmod.CALLS["n"], 2)
		self.assertEqual(counters, {"plain": 1})

	# ================
	# test_tampered_tree_is_repaired_by_replay
	# ================
	def test_tampered_tree_is_repaired_by_replay(self):
		counters: dict = {}
		steps = [
			self.write_step(counters, "writer", "x.txt", b"good", {"x": 1}),
			self.write_step(counters, "keeper", "sub/y.txt", b"keep", {"y": 2}),
		]
		run_build(self.variant, steps)
		tree = self.variant / "extracted"
		(tree / "x.txt").write_bytes(b"evil!")
		(tree / "sub" / "y.txt").unlink()
		run_build(self.variant, steps)
		self.assertEqual(counters, {"writer": 1, "keeper": 1})
		self.assertEqual((tree / "x.txt").read_bytes(), b"good")
		self.assertEqual((tree / "sub" / "y.txt").read_bytes(), b"keep")

	# ================
	# test_unrecordable_summary_runs_every_build
	# ================
	def test_unrecordable_summary_runs_every_build(self):
		counters: dict = {}

		def body():
			counters["loose"] = counters.get("loose", 0) + 1
			(self.variant / "extracted" / "z.txt").write_bytes(b"z")
			return {"ids": {1, 2}}

		steps = [("loose", body, body)]
		run_build(self.variant, steps)
		run_build(self.variant, steps)
		self.assertEqual(counters["loose"], 2)
		self.assertEqual((self.variant / "extracted" / "z.txt").read_bytes(), b"z")

	# ================
	# test_failing_step_leaves_no_journal
	# ================
	def test_failing_step_leaves_no_journal(self):
		def body():
			raise RuntimeError("boom")

		runner = variant_steps.Runner(self.variant, ())
		runner.register("bad", body, code_from=body)
		runner.load({"variant": str(self.variant)})
		runner.begin()
		with self.assertRaises(RuntimeError):
			runner.result(0)
		self.assertFalse(variant_steps.journal_path(self.variant).exists())

	# ================
	# test_changed_step_list_starts_over
	# ================
	def test_changed_step_list_starts_over(self):
		counters: dict = {}
		one = self.write_step(counters, "one", "1.txt", b"1", {"n": 1})
		two = self.write_step(counters, "two", "2.txt", b"2", {"n": 2})
		run_build(self.variant, [one, two])
		run_build(self.variant, [one])
		self.assertEqual(counters["one"], 2)


if __name__ == "__main__":
	unittest.main()
