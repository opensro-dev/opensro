"""
===========================================================================

test_checkout_root.py - the shared worktree rule

resolve_main_checkout has one owner (scripts/checkout_root.py) since
#4033: sro_paths and revtool resolve the main checkout through it. These
tests pin the rule on fake trees; the revtool side's wiring is pinned in
tools/re/revtool/test_revtool_paths.py.

===========================================================================
"""

import os
import tempfile
import unittest
from pathlib import Path

import script_paths  # noqa: F401  (one import-path owner)

import checkout_root
import sro_paths


# ================
# make_main_checkout
#
# A fake main checkout: a .git directory with a worktrees/ folder.
# ================
def make_main_checkout(root: Path) -> Path:
	main = root / "main"
	(main / ".git" / "worktrees").mkdir(parents=True)
	return main


# ================
# make_worktree
#
# A fake linked worktree: a .git file naming the main checkout's
# worktrees/<name> git dir, absolute or relative.
# ================
def make_worktree(root: Path, main: Path, name: str = "wt", relative: bool = False) -> Path:
	worktree = root / name
	worktree.mkdir(parents=True)
	(main / ".git" / "worktrees" / name).mkdir(parents=True, exist_ok=True)
	target = main / ".git" / "worktrees" / name
	if relative:
		target = Path(os.path.relpath(target, worktree))
	worktree.joinpath(".git").write_text(f"gitdir: {target}\n", encoding="utf-8")
	return worktree


class ResolveMainCheckoutTests(unittest.TestCase):
	def test_main_checkout_is_itself(self):
		with tempfile.TemporaryDirectory() as tmp:
			main = make_main_checkout(Path(tmp))
			self.assertEqual(checkout_root.resolve_main_checkout(main), main)

	def test_directory_without_git_is_itself(self):
		with tempfile.TemporaryDirectory() as tmp:
			plain = Path(tmp) / "plain"
			plain.mkdir()
			self.assertEqual(checkout_root.resolve_main_checkout(plain), plain)

	def test_worktree_resolves_to_the_main_checkout(self):
		with tempfile.TemporaryDirectory() as tmp:
			main = make_main_checkout(Path(tmp))
			worktree = make_worktree(Path(tmp), main)
			self.assertEqual(checkout_root.resolve_main_checkout(worktree), main.resolve())

	def test_worktree_with_a_relative_gitdir_resolves(self):
		with tempfile.TemporaryDirectory() as tmp:
			main = make_main_checkout(Path(tmp))
			worktree = make_worktree(Path(tmp), main, relative=True)
			self.assertEqual(checkout_root.resolve_main_checkout(worktree), main.resolve())

	def test_git_file_without_a_gitdir_line_is_refused(self):
		with tempfile.TemporaryDirectory() as tmp:
			broken = Path(tmp) / "broken"
			broken.mkdir()
			broken.joinpath(".git").write_text("not a worktree link\n", encoding="utf-8")
			with self.assertRaises(RuntimeError):
				checkout_root.resolve_main_checkout(broken)

	def test_sro_paths_resolves_through_the_shared_module(self):
		self.assertEqual(
			sro_paths.MAIN_CHECKOUT_ROOT,
			checkout_root.resolve_main_checkout(sro_paths.REPO_ROOT),
		)


if __name__ == "__main__":
	unittest.main()
