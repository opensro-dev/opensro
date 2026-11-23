"""
===========================================================================

test_placeholder_text.py - the importers' no-text rule is the build's

alexandria_npcs.is_placeholder_text answers "does this text cell carry
no text?" for the Python importers; englishCompletions.mjs
isPlaceholderText answers it for the asset build, which refuses an
active row whose English is a placeholder. r204 (#4223) failed because
the two disagreed on "0". This test asks both about the same cells, so
a change to either set that the other does not make fails here.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import shutil
import subprocess
import unittest
from pathlib import Path

import alexandria_npcs as characters  # noqa: E402

BUILD_RULE = Path(__file__).resolve().parents[2] / "build" / "shared" / "englishCompletions.mjs"
# Every placeholder spelling either side knows, case and whitespace
# variants, and real text that only looks close to one.
CELLS = ["", " ", "0", " 0 ", "xxx", "XXX", "-", "null", "NULL", "Null", "00", "0.0", "x", "--", "none",
		 "Lamp of Wishes 1", "소원의 램프 1"]
NODE_TIMEOUT_S = 60


class PlaceholderTextTest(unittest.TestCase):
	# ================
	# test_python_and_the_build_agree_on_every_cell
	# ================
	def test_python_and_the_build_agree_on_every_cell(self):
		node = shutil.which("node")
		if node is None:
			self.skipTest("node is not on PATH")
		script = ("import { isPlaceholderText } from " + json.dumps(BUILD_RULE.as_uri()) + ";"
				  "const cells = JSON.parse( process.argv[1] );"
				  "process.stdout.write( JSON.stringify( cells.map( isPlaceholderText ) ) );")
		out = subprocess.run([node, "--input-type=module", "-e", script, json.dumps(CELLS)], capture_output=True,
							 text=True, encoding="utf-8", timeout=NODE_TIMEOUT_S, check=True, stdin=subprocess.DEVNULL)
		build = json.loads(out.stdout)
		python = [characters.is_placeholder_text(cell) for cell in CELLS]
		self.assertEqual(dict(zip(CELLS, python)), dict(zip(CELLS, build)))
		# The set itself: "0" is a placeholder, real text is not.
		self.assertTrue(characters.is_placeholder_text("0"))
		self.assertFalse(characters.is_placeholder_text("Lamp of Wishes 1"))


if __name__ == "__main__":
	unittest.main()
