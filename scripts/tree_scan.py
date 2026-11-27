"""
===========================================================================

tree_scan.py - every file under a tree with its stat, quickly and exactly

The variant build's no-op checks (variant_stamp.py source fingerprints,
variant_settle.py's content ledger, variant_steps.py's tree snapshots)
compare each file's size and mtime with what an earlier build recorded.
They walked with os.walk and asked Path.stat or os.stat for each file in
turn: 58k native files cost 5.9 s and the variant's 97k content files
13.7 s, each pass, three passes in a no-op build (#4337).

The listing comes from os.scandir, whose names are current. The stat does
not: on NTFS a directory entry's size and times lag the file itself (a
time set with os.utime, or a write through another hard link, is not in
the entry), and test_variant_settle.py fails on entry stats. Each file is
therefore stat'ed by os.stat, which reads the file, on a pool of threads
(os.stat releases the GIL): the same values the old walk saw, in about a
quarter of the time.

===========================================================================
"""
from __future__ import annotations

import os
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

# Threads for the stat pass: stat is I/O-bound, so more than the cores help
# on a cold cache, and a pool past this gains nothing on one disk.
MAX_STAT_THREADS = 32

# Paths handed to a thread at a time: per-path dispatch would cost more
# than the stat it runs.
STAT_CHUNK = 512


# ================
# list_files
#
# (relative posix path, absolute path) for every file under root, sorted
# by path. skip names top-level folders to leave out; as os.walk, a linked
# folder is not descended. A missing root has no files.
# ================
def list_files(root: Path, skip: frozenset[str] | tuple[str, ...] = ()) -> list[tuple[str, str]]:
	found = []
	pending = [(str(root), "")]
	while pending:
		directory, prefix = pending.pop()
		try:
			listing = os.scandir(directory)
		except FileNotFoundError:
			continue
		with listing:
			for entry in listing:
				relative = prefix + entry.name
				if entry.is_dir():
					linked = entry.is_symlink() or getattr(entry, "is_junction", lambda: False)()
					if not linked and (prefix or entry.name not in skip):
						pending.append((entry.path, relative + "/"))
					continue
				found.append((relative, entry.path))
	found.sort(key=lambda row: row[0])
	return found


# ================
# scan_files
#
# (relative posix path, absolute path, os.stat result) for every file
# list_files finds, stat'ed in parallel.
# ================
def scan_files(root: Path, skip: frozenset[str] | tuple[str, ...] = ()) -> list[tuple[str, Path, os.stat_result]]:
	files = list_files(root, skip)
	if not files:
		return []
	threads = min(MAX_STAT_THREADS, (os.cpu_count() or 1) * 2)
	with ThreadPoolExecutor(threads) as pool:
		stats = list(pool.map(os.stat, (absolute for _, absolute in files), chunksize=STAT_CHUNK))
	return [(relative, Path(absolute), stat) for (relative, absolute), stat in zip(files, stats)]
