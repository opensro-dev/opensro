"""
===========================================================================

test_variant_later_layers.py - each later layer is its own variant step

variant_later_layers.py (#4337 item 11): the builder registers one variant
step per layer, so a change to one layer reruns that layer alone. Each
layer's step name is its own, and the runner takes a step's result by name,
so registering a layer never shifts the results of the steps after it.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import contextlib
import io
import tempfile
import unittest
from pathlib import Path

import variant_later_layers
import variant_steps


def first():
	(Path(OUT) / "a.txt").write_bytes(b"a")
	return "first"


def second():
	(Path(OUT) / "b.txt").write_bytes(b"b")
	return "second"


OUT = ""


class LaterLayerSteps(unittest.TestCase):
	# ================
	# test_every_layer_has_its_own_step
	# ================
	def test_every_layer_has_its_own_step(self):
		names = [variant_later_layers.step_name(key) for key, _ in variant_later_layers.LATER_LAYERS]
		self.assertEqual(len(names), len(set(names)))
		self.assertTrue(all(name.startswith("later-") for name in names))

	# ================
	# test_results_are_taken_by_name
	# ================
	def test_results_are_taken_by_name(self):
		global OUT
		with tempfile.TemporaryDirectory() as temp:
			variant = Path(temp) / "variant"
			OUT = str(variant / "extracted")
			(variant / "extracted").mkdir(parents=True)
			runner = variant_steps.Runner(variant, [])
			runner.register("first", first)
			runner.register("second", second)
			runner.load({"variant": str(variant)})
			runner.begin()
			with contextlib.redirect_stdout(io.StringIO()):
				self.assertEqual(runner.result(runner.index("first")), "first")
				self.assertEqual(runner.result(runner.index("second")), "second")
			with self.assertRaises(KeyError):
				runner.index("later-missing")


if __name__ == "__main__":
	unittest.main()
