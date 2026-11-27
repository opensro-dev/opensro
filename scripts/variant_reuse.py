"""
===========================================================================

variant_reuse.py - files a variant rebuild removes, kept to be laid back

The one owner of the variant's reuse folder (#4337 items 29/31). The base
step links the native tree (48k files, 2.6 GB) into the variant. The native
tree is on H: and the staging on D:, links across volumes are refused, so
every file is copied. Whenever base reran (its code changes every few days),
rollback first unlinked all 48k files, then base copied every one of them
back from H:, one at a time: 764 s to 2234 s for bytes that had not changed.

Now a file the rebuild removes is moved here instead (a rename on the same
volume: no bytes move), and link_tree renames it back when the source file
still has the size and mtime the copy was taken with (copy2 keeps the
source's mtime; a hard link shares its inode). Anything left here when the
build ends is deleted. The folder is under the variant's .state, outside the
content tree, so no step's effect or settle ledger ever sees it.

===========================================================================
"""

import os
import shutil
from pathlib import Path

from sro_paths import tree_state_root

REUSE_FOLDER = "variant-reuse"


# ================
# reuse_root
# ================
def reuse_root(variant_root: Path) -> Path:
	return tree_state_root(variant_root) / REUSE_FOLDER


# ================
# stash
#
# Moves variant_root/relative into the reuse folder instead of deleting it.
# A file that is not there is nothing to keep.
# ================
def stash(variant_root: Path, relative: str) -> None:
	source = Path(variant_root) / relative
	if not source.is_file():
		return
	target = reuse_root(variant_root) / relative
	target.parent.mkdir(parents=True, exist_ok=True)
	os.replace(source, target)


# ================
# stash_tree
#
# A whole content folder moved aside in one rename (a fresh build's clear).
# ================
def stash_tree(variant_root: Path, relative: str) -> None:
	source = Path(variant_root) / relative
	if not source.is_dir():
		return
	target = reuse_root(variant_root) / relative
	if target.exists():
		shutil.rmtree(target)
	target.parent.mkdir(parents=True, exist_ok=True)
	os.replace(source, target)


# ================
# take
#
# Lays the kept copy of a file at link when it was taken from source as
# source is now (same size and mtime); returns whether it did.
# ================
def take(kept: Path, source_stat: os.stat_result, link: Path) -> bool:
	try:
		stat = kept.stat()
	except OSError:
		return False
	if stat.st_size != source_stat.st_size or stat.st_mtime_ns != source_stat.st_mtime_ns:
		return False
	os.replace(kept, link)
	return True


# ================
# clear
#
# Drops whatever the last build kept and did not take back.
# ================
def clear(variant_root: Path) -> None:
	root = reuse_root(variant_root)
	if root.exists():
		shutil.rmtree(root)
