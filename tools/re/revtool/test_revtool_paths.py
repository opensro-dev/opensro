"""
test_revtool_paths.py - checks for revtool's path resolution.

The per-machine data dir lives in the MAIN checkout's .state/revtool,
so revtool run from a linked worktree reads the shared overlay DB
(#4033 part 2). The worktree rule itself is owned by
scripts/checkout_root.py; this suite pins the semantics on fake trees
and that revtool_paths wires its data dir through it. Run with:

    py -3 test_revtool_paths.py            (module's directory on path)
"""

from __future__ import annotations

import importlib
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
if os.environ.get("REVTOOL_HOME"):
	sys.path.insert(0, os.environ["REVTOOL_HOME"])

try:
	import revtool_paths  # noqa: E402
	# Imported after revtool_paths on purpose: revtool_paths puts the
	# repo's scripts/ on sys.path, which is how the shared module is
	# reached; this import failing is the failure mode under test.
	from checkout_root import resolve_main_checkout  # noqa: E402
	_MISSING = ""
except ImportError as exc:
	revtool_paths = None
	resolve_main_checkout = None
	_MISSING = f"revtool paths are not importable here: {exc}"


# ================
# make_worktree_pair
#
# A fake main checkout (.git directory) and one linked worktree (.git
# file), absolute or relative gitdir line.
# ================
def make_worktree_pair(root: Path, relative: bool = False):
	main = root / "main"
	(main / ".git" / "worktrees" / "wt").mkdir(parents=True)
	worktree = root / "wt"
	worktree.mkdir()
	target = main / ".git" / "worktrees" / "wt"
	if relative:
		target = Path(os.path.relpath(target, worktree))
	worktree.joinpath(".git").write_text(f"gitdir: {target}\n", encoding="utf-8")
	return main, worktree


@unittest.skipIf(_MISSING, _MISSING)
class ResolveMainCheckoutTests(unittest.TestCase):
	def test_main_checkout_is_itself(self):
		with tempfile.TemporaryDirectory() as tmp:
			main, _ = make_worktree_pair(Path(tmp))
			self.assertEqual(resolve_main_checkout(main), main)

	def test_worktree_resolves_to_the_main_checkout(self):
		with tempfile.TemporaryDirectory() as tmp:
			main, worktree = make_worktree_pair(Path(tmp))
			self.assertEqual(resolve_main_checkout(worktree), main.resolve())

	def test_worktree_with_a_relative_gitdir_resolves(self):
		with tempfile.TemporaryDirectory() as tmp:
			main, worktree = make_worktree_pair(Path(tmp), relative=True)
			self.assertEqual(resolve_main_checkout(worktree), main.resolve())

	def test_git_file_without_a_gitdir_line_is_refused(self):
		with tempfile.TemporaryDirectory() as tmp:
			broken = Path(tmp) / "broken"
			broken.mkdir()
			broken.joinpath(".git").write_text("not a worktree link\n", encoding="utf-8")
			with self.assertRaises(RuntimeError):
				resolve_main_checkout(broken)


@unittest.skipIf(_MISSING, _MISSING)
class DataDirTests(unittest.TestCase):
	def reload_paths(self, env):
		with mock.patch.dict(os.environ, env, clear=False):
			if "REVTOOL_DATA_DIR" not in env:
				os.environ.pop("REVTOOL_DATA_DIR", None)
			importlib.reload(revtool_paths)
		self.addCleanup(importlib.reload, revtool_paths)
		return revtool_paths

	def test_data_dir_is_the_main_checkouts_state(self):
		paths = self.reload_paths({})
		expected_main = str(resolve_main_checkout(Path(paths.REPO_ROOT)))
		self.assertEqual(paths.MAIN_CHECKOUT_ROOT, expected_main)
		self.assertEqual(
			paths.DATA_DIR, os.path.join(expected_main, ".state", "revtool")
		)
		self.assertEqual(
			paths.DEFAULT_DB, os.path.join(paths.DATA_DIR, "revtool.db")
		)

	def test_data_dir_override_still_wins(self):
		paths = self.reload_paths({"REVTOOL_DATA_DIR": "elsewhere"})
		self.assertEqual(paths.DATA_DIR, "elsewhere")


if __name__ == "__main__":
	unittest.main()
