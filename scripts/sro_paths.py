"""
===========================================================================

sro_paths.py - the Python twin of scripts/build/world/paths.mjs

The one owner of the pipeline's filesystem roots for Python tools. The
repository root is this checkout. Generated output lives in the MAIN
checkout's .generated unless SRO_GENERATED_ROOT names another absolute tree
(the rule scripts/lib/generatedRoot.mjs owns), so a worktree reads the
shared build with no junction and no environment. The game data
(extracted/ and the client files) lives beside the main checkout, which a
linked git worktree names in its .git file. Keep the rule identical to
generatedRoot.mjs and paths.mjs.

===========================================================================
"""
from __future__ import annotations

import os
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]


# ================
# _resolve_main_checkout
#
# The main checkout: this one, or the one a linked worktree's .git file
# names ("gitdir: <main>/.git/worktrees/<name>"); a main checkout's .git is
# a directory.
# ================
def _resolve_main_checkout(checkout: Path) -> Path:
	dot_git = checkout / ".git"
	if not dot_git.is_file():
		return checkout
	for line in dot_git.read_text(encoding="utf-8").splitlines():
		if line.startswith("gitdir:"):
			worktree_git_dir = (checkout / line[len("gitdir:"):].strip()).resolve()
			# parents: [0] .git/worktrees, [1] .git, [2] main checkout
			return worktree_git_dir.parents[2]
	raise RuntimeError(f"Unreadable worktree link {dot_git}")


MAIN_CHECKOUT_ROOT = _resolve_main_checkout(REPO_ROOT)

# The trees a worktree must never hold itself, each exempt only while its
# own override names another tree (scripts/lib/generatedRoot.mjs).
_MAIN_CHECKOUT_TREES = (
	(Path(".generated"), "SRO_GENERATED_ROOT"),
	(Path("apps") / "server" / ".generated", "SRO_SERVER_GAME_DATA_ROOT"),
)


# ================
# worktree_copies
#
# The generated trees a linked worktree holds of its own - a copy, a
# symlink or a junction, broken or not. The main checkout's are the only
# ones any tool reads; a second one is a stale tree waiting to be used.
# ================
def worktree_copies(checkout: Path = REPO_ROOT, main_checkout: Path = MAIN_CHECKOUT_ROOT, env=os.environ) -> list[Path]:
	if checkout.resolve() == main_checkout.resolve():
		return []
	return [
		checkout / tree
		for tree, override in _MAIN_CHECKOUT_TREES
		if not env.get(override) and (os.path.lexists(checkout / tree) or os.path.isjunction(checkout / tree))
	]


_COPIES = worktree_copies()
if _COPIES:
	raise RuntimeError(
		f"This worktree holds its own generated tree: {', '.join(map(str, _COPIES))}. Every tool reads and builds "
		f"the main checkout's ({MAIN_CHECKOUT_ROOT}); move these aside into temp/ (or delete them; unlink a symlink "
		"or junction, never delete through it) and rerun."
	)


# ================
# _resolve_game_root
#
# SRO_GAME_ROOT when the operator sets it, else the parent of the main
# checkout.
# ================
def _resolve_game_root() -> Path:
	configured = os.environ.get("SRO_GAME_ROOT", "").strip()
	if configured:
		return Path(configured).resolve()
	return MAIN_CHECKOUT_ROOT.parent


GAME_ROOT = _resolve_game_root()
EXTRACTED_ROOT = GAME_ROOT / "extracted"
def _resolve_generated_root() -> Path:
	"""SRO_GENERATED_ROOT when set (absolute only), else the main checkout's."""
	override = os.environ.get("SRO_GENERATED_ROOT")
	if override:
		path = Path(override)
		if not path.is_absolute():
			raise RuntimeError(f"SRO_GENERATED_ROOT must be an absolute path, not {override!r}")
		return path.resolve()
	return MAIN_CHECKOUT_ROOT / ".generated"


GENERATED_ROOT = _resolve_generated_root()
PUBLIC_ROOT = GENERATED_ROOT / "client-public"
