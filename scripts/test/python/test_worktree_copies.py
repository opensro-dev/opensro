"""
===========================================================================

test_worktree_copies.py - a worktree never holds its own generated tree

The Python twin of the generatedRoot.mjs rule: a linked worktree's own
.generated or apps/server/.generated - a copy, a symlink or a junction,
broken or not - is reported, unless that tree's override names another one.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import sro_paths  # noqa: E402


# ================
# dangling_link
#
# A link to a missing directory: a junction on Windows (no privilege needed,
# and what a worktree there would hold), a symlink elsewhere.
# ================
def dangling_link(target: Path, link: Path) -> None:
	if sys.platform == "win32":
		import _winapi

		target.mkdir()
		_winapi.CreateJunction(str(target), str(link))
		target.rmdir()
	else:
		os.symlink(target, link, target_is_directory=True)


class WorktreeCopiesTest(unittest.TestCase):
	def setUp(self) -> None:
		self.scratch = Path(tempfile.mkdtemp(prefix="sro-worktree-copies-"))
		self.main = self.scratch / "main"
		self.worktree = self.scratch / "wt"
		(self.main / ".git" / "worktrees" / "wt").mkdir(parents=True)
		(self.worktree / "apps" / "server").mkdir(parents=True)
		(self.worktree / ".git").write_text(f"gitdir: {self.main / '.git' / 'worktrees' / 'wt'}\n", encoding="utf-8")

	def tearDown(self) -> None:
		shutil.rmtree(self.scratch, ignore_errors=True)

	def copies(self, env=None):
		return sro_paths.worktree_copies(self.worktree, self.main, env or {})

	def test_a_clean_worktree_holds_nothing(self) -> None:
		self.assertEqual(self.copies(), [])

	def test_copies_and_dangling_links_are_found(self) -> None:
		(self.worktree / ".generated").mkdir()
		dangling_link(self.scratch / "gone", self.worktree / "apps" / "server" / ".generated")
		self.assertEqual(
			self.copies(),
			[self.worktree / ".generated", self.worktree / "apps" / "server" / ".generated"],
		)

	def test_an_override_exempts_only_its_own_tree(self) -> None:
		(self.worktree / ".generated").mkdir()
		(self.worktree / "apps" / "server" / ".generated").mkdir()
		self.assertEqual(
			self.copies({"SRO_GENERATED_ROOT": str(self.scratch / "elsewhere")}),
			[self.worktree / "apps" / "server" / ".generated"],
		)

	def test_the_main_checkout_owns_its_trees(self) -> None:
		(self.main / ".generated").mkdir()
		self.assertEqual(sro_paths.worktree_copies(self.main, self.main, {}), [])


if __name__ == "__main__":
	unittest.main()
