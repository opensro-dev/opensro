"""
===========================================================================

sro_paths.py - the Python twin of scripts/build/world/paths.mjs

The one owner of the pipeline's filesystem roots for Python tools. The
repository root is this checkout and generated output lives in its
.generated, so worktrees build in isolation. The game data (extracted/ and
the client files) is shared and lives beside the MAIN checkout, which a
linked git worktree names in its .git file. Keep the rule identical to
paths.mjs.

===========================================================================
"""
from __future__ import annotations

import os
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]


# ================
# _resolve_game_root
#
# SRO_GAME_ROOT when the operator sets it, else the parent of the main
# checkout. A linked worktree's .git is a file "gitdir: <main>/.git/worktrees/<name>";
# a main checkout's .git is a directory.
# ================
def _resolve_game_root() -> Path:
	configured = os.environ.get("SRO_GAME_ROOT", "").strip()
	if configured:
		return Path(configured).resolve()
	dot_git = REPO_ROOT / ".git"
	if dot_git.is_file():
		for line in dot_git.read_text(encoding="utf-8").splitlines():
			if line.startswith("gitdir:"):
				worktree_git_dir = (REPO_ROOT / line[len("gitdir:"):].strip()).resolve()
				# parents: [0] .git/worktrees, [1] .git, [2] main checkout, [3] game root
				return worktree_git_dir.parents[3]
		raise RuntimeError(f"Unreadable worktree link {dot_git}")
	return REPO_ROOT.parent


GAME_ROOT = _resolve_game_root()
EXTRACTED_ROOT = GAME_ROOT / "extracted"
GENERATED_ROOT = REPO_ROOT / ".generated"
PUBLIC_ROOT = GENERATED_ROOT / "client-public"
