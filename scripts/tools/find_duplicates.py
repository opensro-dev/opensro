"""
===========================================================================

find_duplicates.py - the bytes a build tree holds more than once

A variant staging held 34 GB for a 4.7 GB game (r203r, 2026-11-21). This
measures where the copies are, so a workflow change is chosen by evidence:

	identical   files with the same bytes that are not one file: hard
	            links share their storage and cost nothing, so a group is
	            counted once per distinct file (device, file index)
	packed      loose files a pack manifest also stores inside a .bin pack
	            (assets/packs/manifest.json "assets": path -> pack); the
	            pack holds the bytes the player downloads

	py -3 scripts/tools/find_duplicates.py ROOT [--packs MANIFEST]
		[--min-bytes N] [--top N] [--json OUT]

Files are grouped by size first, then by a hash of their first block, and
only then hashed whole, so a tree of tens of GB costs one read of the
files that could match. Read-only: it never deletes or links anything.

===========================================================================
"""
import argparse
import hashlib
import json
import os
import sys
from pathlib import Path

# Smallest file worth counting: below this the copies are noise.
DEFAULT_MIN_BYTES = 64 * 1024
# The first-block hash that splits same-size files before a whole read.
HEAD_BYTES = 64 * 1024
READ_CHUNK = 4 * 1024 * 1024
DEFAULT_TOP = 15
# The folder depth, below ROOT, waste is summed at.
FOLDER_DEPTH = 3


# ================
# walk
#
# (path, size, identity) for every regular file under root at least
# min_bytes long. identity is (device, file index): two paths with one
# identity are hard links to one file. Links and junctions are not
# followed, so a tree that links another is not counted twice.
# ================
def walk(root, min_bytes):
	stack = [Path(root)]
	while stack:
		folder = stack.pop()
		try:
			entries = list(os.scandir(folder))
		except OSError:
			continue
		for entry in entries:
			if entry.is_symlink() or (hasattr(entry, "is_junction") and entry.is_junction()):
				continue
			if entry.is_dir(follow_symlinks=False):
				stack.append(Path(entry.path))
				continue
			if not entry.is_file(follow_symlinks=False):
				continue
			stat = os.stat(entry.path, follow_symlinks=False)
			if stat.st_size >= min_bytes:
				yield Path(entry.path), stat.st_size, (stat.st_dev, stat.st_ino)


# ================
# digest
#
# The blake2b of a file's first limit bytes, or of the whole file.
# ================
def digest(path, limit=None):
	hasher = hashlib.blake2b(digest_size=20)
	remaining = limit
	with open(path, "rb") as handle:
		while remaining is None or remaining > 0:
			block = handle.read(READ_CHUNK if remaining is None else min(READ_CHUNK, remaining))
			if not block:
				break
			hasher.update(block)
			if remaining is not None:
				remaining -= len(block)
	return hasher.hexdigest()


# ================
# identical_groups
#
# Lists of distinct files (one path per file identity) that hold the same
# bytes, each list two or more long.
# ================
def identical_groups(files):
	by_size = {}
	for path, size, identity in files:
		by_size.setdefault(size, {}).setdefault(identity, path)
	groups = []
	for size, distinct in by_size.items():
		if len(distinct) < 2:
			continue
		by_head = {}
		for path in distinct.values():
			by_head.setdefault(digest(path, HEAD_BYTES), []).append(path)
		for heads in by_head.values():
			if len(heads) < 2:
				continue
			if size <= HEAD_BYTES:
				groups.append((size, heads))
				continue
			by_whole = {}
			for path in heads:
				by_whole.setdefault(digest(path), []).append(path)
			groups.extend((size, paths) for paths in by_whole.values() if len(paths) > 1)
	return groups


# ================
# packed_loose
#
# (path, size) for each loose file the pack manifest stores in a pack.
# Manifest paths are URL paths (/assets/...); the public root is the
# folder that holds assets/.
# ================
def packed_loose(manifest):
	manifest = Path(manifest)
	public_root = manifest.parents[2]
	document = json.loads(manifest.read_text(encoding="utf-8"))
	found = []
	for asset in document.get("assets", []):
		if not asset.get("packPath"):
			continue
		loose = public_root / asset["path"].lstrip("/")
		if loose.is_file():
			found.append((loose, loose.stat().st_size))
	return found


# ================
# folder_of
#
# The path's folder FOLDER_DEPTH levels below root, as the report key.
# ================
def folder_of(root, path):
	parts = Path(path).relative_to(root).parts[:-1]
	return "/".join(parts[:FOLDER_DEPTH]) or "."


# ================
# report
#
# The summary: waste per kind, and the folders holding most of it. In an
# identical group every copy past the first is waste, charged to the
# folder it sits in; the first is the group's lowest path.
# ================
def report(root, groups, packed, top):
	waste = {}
	pairs = {}
	identical_bytes = 0
	for size, paths in groups:
		keep, *copies = sorted(paths)
		for path in copies:
			identical_bytes += size
			waste[folder_of(root, path)] = waste.get(folder_of(root, path), 0) + size
			pair = (folder_of(root, keep), folder_of(root, path))
			pairs[pair] = pairs.get(pair, 0) + size
	packed_bytes = sum(size for _, size in packed)
	packed_folders = {}
	for path, size in packed:
		packed_folders[folder_of(root, path)] = packed_folders.get(folder_of(root, path), 0) + size
	return {
		"root": str(root),
		"identical": {
			"groups": len(groups),
			"wasted_bytes": identical_bytes,
			"folders": sorted(waste.items(), key=lambda item: -item[1])[:top],
			"pairs": [[keep, copy, size] for (keep, copy), size in sorted(pairs.items(), key=lambda item: -item[1])[:top]]
		},
		"packed": {"files": len(packed), "loose_bytes": packed_bytes,
			"folders": sorted(packed_folders.items(), key=lambda item: -item[1])[:top]}
	}


# ================
# print_report
# ================
def print_report(summary):
	gb = 1e9
	identical, packed = summary["identical"], summary["packed"]
	print(f"{summary['root']}")
	print(f"identical: {identical['wasted_bytes'] / gb:.2f} GB wasted in {identical['groups']} groups")
	for folder, size in identical["folders"]:
		print(f"  {size / gb:7.2f} GB  {folder}")
	print("  copy of -> copy in:")
	for keep, copy, size in identical["pairs"]:
		print(f"  {size / gb:7.2f} GB  {keep}  ->  {copy}")
	print(f"packed: {packed['loose_bytes'] / gb:.2f} GB loose in {packed['files']} files a pack also stores")
	for folder, size in packed["folders"]:
		print(f"  {size / gb:7.2f} GB  {folder}")


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description="The bytes a build tree holds more than once (read-only).")
	parser.add_argument("root", type=Path, help="the tree to measure")
	parser.add_argument("--packs", type=Path, help="an assets/packs/manifest.json whose packed files to look for loose")
	parser.add_argument("--min-bytes", type=int, default=DEFAULT_MIN_BYTES, help="smallest file counted")
	parser.add_argument("--top", type=int, default=DEFAULT_TOP, help="folders listed per kind")
	parser.add_argument("--json", type=Path, help="also write the summary here")
	arguments = parser.parse_args()
	root = arguments.root.resolve()
	if not root.is_dir():
		raise SystemExit(f"{root} is not a folder")
	groups = identical_groups(walk(root, arguments.min_bytes))
	packed = packed_loose(arguments.packs) if arguments.packs else []
	summary = report(root, groups, packed, arguments.top)
	print_report(summary)
	if arguments.json:
		arguments.json.write_text(json.dumps(summary, indent="\t") + "\n", encoding="utf-8", newline="\n")
	return 0


if __name__ == "__main__":
	sys.exit(main())
