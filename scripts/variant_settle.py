"""
===========================================================================

variant_settle.py - a rebuilt variant keeps the timestamps of unchanged files

build_alexandria_variant.py rebuilds the expanded game root in place: it
re-adds 13,633 VSRO files, restores native tables and re-imports every
layer on each run. Most files end with the bytes they held before, but
with a fresh mtime, and every cache downstream keys on (size, mtime): the
image discovery and conversion (convert_images.py), the resource build
fingerprint, the lane memo, the file hash cache. One variant rebuild
therefore reconverted all 25,629 images (release 55e35e3e3, 2026-11-16)
and replayed no lane at all.

settle() runs after a successful variant build. A ledger beside the tree
records each file's size, mtime and SHA-256 as the last build left it. A
file whose size and mtime still match is trusted unread; any other is
hashed, and when its bytes equal the ledger's its old mtime is restored,
so the downstream caches see the tree exactly as unchanged as it is. A
file whose bytes did change keeps its new mtime. The ledger is rewritten
with what the tree now holds, so files that left the tree leave it too.

The ledger lives in the build state beside the tree (<root>/.state), never
in the checkout, so every worktree that rebuilds the variant shares it.

settle only re-times files the variant owns. The variant build hard-links
the native extraction's folders (build_alexandria_variant.link_tree), and
a time set on a link is set on the native file too, so a file with more
than one link keeps whatever time it has: settle records it as it stands.

===========================================================================
"""
from __future__ import annotations

import hashlib
import json
import os
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

LEDGER_FORMAT = "sro-variant-content"
LEDGER_VERSION = 1
# The trees settle walks: the game data the build reads. The build state,
# the generated tree and loose root files are not variant content.
CONTENT_ROOT = "extracted"
HASH_CHUNK = 1 << 20


# ================
# ledger_path
# ================
def ledger_path(variant_root: Path) -> Path:
	return Path(variant_root) / ".state" / "variant-content.json"


# ================
# load_ledger
#
# {relative path: [size, mtime_ns, sha256]}; a missing, unreadable or
# foreign ledger is empty, so the worst case is one full hash.
# ================
def load_ledger(path: Path) -> dict[str, list]:
	try:
		document = json.loads(path.read_text(encoding="utf-8"))
	except (OSError, ValueError):
		return {}
	if document.get("format") != LEDGER_FORMAT or document.get("version") != LEDGER_VERSION:
		return {}
	entries = document.get("entries")
	return entries if isinstance(entries, dict) else {}


# ================
# write_ledger
# ================
def write_ledger(path: Path, entries: dict[str, list]) -> None:
	data = json.dumps({"format": LEDGER_FORMAT, "version": LEDGER_VERSION, "entries": dict(sorted(entries.items()))},
		separators=(",", ":")).encode("utf-8")
	path.parent.mkdir(parents=True, exist_ok=True)
	temporary = path.with_suffix(".json.tmp")
	temporary.write_bytes(data)
	os.replace(temporary, path)


# ================
# file_sha256
# ================
def file_sha256(path: Path) -> str:
	digest = hashlib.sha256()
	with open(path, "rb") as handle:
		while chunk := handle.read(HASH_CHUNK):
			digest.update(chunk)
	return digest.hexdigest()


# ================
# content_files
#
# (relative posix path, absolute path, stat) for every file under the
# content root, in walk order.
# ================
def content_files(variant_root: Path) -> list[tuple[str, Path, os.stat_result]]:
	base = Path(variant_root)
	found = []
	for directory, _, names in os.walk(base / CONTENT_ROOT):
		for name in names:
			absolute = Path(directory, name)
			found.append((absolute.relative_to(base).as_posix(), absolute, os.stat(absolute)))
	return found


# ================
# matches_ledger
#
# Whether the tree is exactly what the last settle recorded: the same files,
# each with its recorded size and mtime. variant_stamp.py asks this before
# it lets an unchanged build skip; nothing is read but the directory.
# ================
def matches_ledger(variant_root: Path) -> bool:
	ledger = load_ledger(ledger_path(variant_root))
	if not ledger:
		return False
	files = content_files(variant_root)
	if len(files) != len(ledger):
		return False
	for relative, _, stat in files:
		known = ledger.get(relative)
		if not known or known[0] != stat.st_size or known[1] != stat.st_mtime_ns:
			return False
	return True


# ================
# settle
#
# Restores the recorded mtime of every file whose bytes the last build
# left unchanged, and records the tree as it now stands. Returns the
# counts: files seen, trusted unread, hashed, restored and changed.
# ================
def settle(variant_root: Path, jobs: int = 8) -> dict[str, int]:
	path = ledger_path(variant_root)
	ledger = load_ledger(path)
	files = content_files(variant_root)
	entries: dict[str, list] = {}
	pending = []
	for relative, absolute, stat in files:
		known = ledger.get(relative)
		if known and known[0] == stat.st_size and known[1] == stat.st_mtime_ns:
			entries[relative] = known
		else:
			pending.append((relative, absolute, stat))
	counts = {"files": len(files), "unread": len(files) - len(pending), "hashed": len(pending), "restored": 0, "changed": 0}
	with ThreadPoolExecutor(max_workers=max(1, jobs)) as pool:
		digests = pool.map(lambda item: file_sha256(item[1]), pending)
		for (relative, absolute, stat), sha in zip(pending, digests):
			known = ledger.get(relative)
			if known and known[0] == stat.st_size and known[2] == sha and stat.st_nlink == 1:
				# Same bytes as the last build left: give the file back its
				# old time, so (size, mtime) caches downstream still hit.
				os.utime(absolute, ns=(stat.st_atime_ns, known[1]))
				entries[relative] = known
				counts["restored"] += 1
			else:
				entries[relative] = [stat.st_size, stat.st_mtime_ns, sha]
				counts["changed"] += 1
	write_ledger(path, entries)
	return counts
