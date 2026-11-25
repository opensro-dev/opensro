"""
===========================================================================

checkout_root.py - which checkout is the main one

The one owner of the worktree rule: a linked worktree's .git file names
the main checkout ("gitdir: <main>/.git/worktrees/<name>"); the main
checkout's own .git is a directory. scripts/sro_paths.py (pipeline
roots) and tools/re/revtool (its per-machine data dir) both resolve
through here, so a tool run from any worktree reads the main checkout's
shared trees. Side-effect free: importing this module resolves nothing,
so revtool can take the rule without taking sro_paths' root resolution
(and its refusals) with it (#4033).

===========================================================================
"""
from __future__ import annotations

from pathlib import Path


# ================
# resolve_main_checkout
#
# The main checkout: this one, or the one a linked worktree's .git file
# names ("gitdir: <main>/.git/worktrees/<name>"); a main checkout's .git is
# a directory.
# ================
def resolve_main_checkout(checkout: Path) -> Path:
	dot_git = checkout / ".git"
	if not dot_git.is_file():
		return checkout
	for line in dot_git.read_text(encoding="utf-8").splitlines():
		if line.startswith("gitdir:"):
			worktree_git_dir = (checkout / line[len("gitdir:"):].strip()).resolve()
			# parents: [0] .git/worktrees, [1] .git, [2] main checkout
			return worktree_git_dir.parents[2]
	raise RuntimeError(f"Unreadable worktree link {dot_git}")
