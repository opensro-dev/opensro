"""
===========================================================================

test_script_paths.py - every test module imports on its own

#3410: a module that relied on a sibling's sys.path insert passed in the
full discovery and failed when run alone. Each module is imported here in
a fresh interpreter whose path holds only this folder, as
`unittest discover -s scripts/test/python` sets it up, so a module that
forgets `import script_paths` fails by name instead of by file order.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import subprocess
import sys
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

HERE = Path(__file__).resolve().parent
IMPORT_TIMEOUT_S = 120
WORKERS = 8


# ================
# import_alone
#
# (module, error) for one module imported in a fresh interpreter; error is
# "" when it imported.
# ================
def import_alone(module):
	code = f"import sys; sys.path[:0] = [{str(HERE)!r}]; import {module}"
	run = subprocess.run([sys.executable, "-B", "-c", code], cwd=HERE.parents[2], capture_output=True, text=True,
		encoding="utf-8", errors="replace", timeout=IMPORT_TIMEOUT_S)
	if run.returncode == 0:
		return module, ""
	lines = [line for line in run.stderr.splitlines() if line.strip()]
	return module, lines[-1] if lines else f"exit {run.returncode}"


class ScriptPaths(unittest.TestCase):
	def test_every_test_module_imports_alone(self):
		modules = sorted(path.stem for path in HERE.glob("test_*.py"))
		self.assertGreater(len(modules), 1)
		with ThreadPoolExecutor(WORKERS) as pool:
			failed = [f"{module}: {error}" for module, error in pool.map(import_alone, modules) if error]
		self.assertEqual(failed, [], "modules that import only after a sibling ran:\n" + "\n".join(failed))


if __name__ == "__main__":
	unittest.main()
