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
# _is_junction
#
# os.path.isjunction only exists from Python 3.12, and only means
# anything on Windows; the release hosts run 3.11, where a junction
# cannot occur and the lexists half of the test already covers
# symlinks. Absent the function, the answer is no.
# ================
def _is_junction(path: Path) -> bool:
	isjunction = getattr(os.path, "isjunction", None)
	return isjunction is not None and isjunction(path)


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
		if not env.get(override) and (os.path.lexists(checkout / tree) or _is_junction(checkout / tree))
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


# ================
# resolve_build_state_root
#
# The build state beside a generated tree (generatedRoot.mjs
# resolveBuildStateRoot): the reuse caches, stamps and the asset lock of the
# tree they describe, so a fresh worktree finds the last build's.
# ================
def resolve_build_state_root(generated_root: Path | None = None) -> Path:
	return tree_state_root(Path(GENERATED_ROOT if generated_root is None else generated_root).parent)


# ================
# tree_state_root
#
# The build state of one tree: <tree>/.state. The generated tree's state is
# that of the tree holding it (resolve_build_state_root); a variant game
# root keeps its own settle ledger and stamp here, beside the caches its
# asset build writes (SRO_GENERATED_ROOT=<variant>/.generated).
# ================
def tree_state_root(tree_root: Path) -> Path:
	return Path(tree_root) / ".state"


# ================
# trees_overlap
#
# Whether two resolved folders are the same or one holds the other. Every
# importer refuses to write its overlay into (or around) the tree it reads.
# ================
def trees_overlap(a: Path, b: Path) -> bool:
	return a == b or a in b.parents or b in a.parents


BUILD_STATE_ROOT = resolve_build_state_root()


# ================
# build_state_path
# ================
def build_state_path(*parts) -> Path:
	return BUILD_STATE_ROOT.joinpath(*parts)
