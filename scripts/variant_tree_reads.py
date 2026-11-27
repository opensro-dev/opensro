"""
===========================================================================

variant_tree_reads.py - what a variant step read of the tree it builds

variant_steps.py keyed each step by the chain of every earlier step's
effect: one changed effect reran every step after it, whether it read
the change or not. r204n's try 8 (#4337): services' new shop slots
reran later-mall, world-layers, music and environment, 190 s, and a
change near the front of the 46 steps reran all the rest.

This module records, while a step runs, every way it can depend on the
tree in front of it: the files it opens for reading, the directories it
lists, and the paths whose existence or type it tests (stat, exists,
mkdir, remove, rename, link, an exclusive create). The step is then
keyed by those paths' values in the tree the earlier steps' effects
leave (TreeView): a file's content hash, "dir" or "gone", and the
names a listed directory holds. A step whose reads come back the same
is current whatever else moved, and replays its recorded effect.

What the recorder cannot see makes a step opaque, and an opaque step
keeps the chain key:

  - a process it starts: the child's reads are its own;
  - code outside the checkout and the standard library (site-packages):
    an extension may read files without an audit event;
  - an access to the variant outside its content root, which no effect
    records.

Paths are matched case-insensitively, as the Windows file system
matches them: a step that names a file in another case reads the same
file, and its key must follow that file.

===========================================================================
"""
from __future__ import annotations

import hashlib
import os
import sysconfig

# The audit events after which what a step read cannot be known.
OPAQUE_EVENTS = frozenset({
	"subprocess.Popen", "os.system", "os.posix_spawn", "os.spawn", "os.exec", "os.startfile", "os.fork",
	"ctypes.dlopen", "sqlite3.connect",
})
# Audit events whose path arguments a step depends on, by position: the
# path's content or existence decides what the call does.
PROBE_EVENTS = {
	"os.remove": (0,), "os.rmdir": (0,), "os.mkdir": (0,), "os.utime": (0,), "os.chmod": (0,),
	"os.truncate": (0,), "os.rename": (0, 1), "os.replace": (0, 1), "os.link": (0, 1), "os.symlink": (1,),
	"shutil.copyfile": (0, 1), "shutil.copymode": (0, 1), "shutil.copystat": (0, 1),
}
# Audit events that list a directory (os.walk, glob and Path.iterdir
# all reach one of them).
LIST_EVENTS = frozenset({"os.listdir", "os.scandir"})
# The os and os.path functions that test a path without an audit event,
# wrapped while a step runs. pathlib reaches os.stat; Windows' os.path
# tests are C functions of their own.
STAT_FUNCTIONS = ("stat", "lstat", "access")
PATH_TESTS = ("exists", "lexists", "isfile", "isdir", "islink", "isjunction")
# Third-party code: running it makes a step opaque.
PACKAGE_CODE = tuple(
	os.path.normcase(path) for path in {sysconfig.get_paths()["purelib"], sysconfig.get_paths()["platlib"]}
)
# How an opaque step is recorded in place of its reads.
OPAQUE = "opaque"


# ================
# Recorder
#
# One step's accesses to the variant tree, collected while it runs
# (start, then stop). paths and lists are variant-relative POSIX paths;
# opaque names why the step's reads cannot be known, or is None.
# ================
class Recorder:
	def __init__(self, variant_root, content_root: str):
		root = os.path.abspath(os.fspath(variant_root))
		self.root = root
		self.folded_root = os.path.normcase(root)
		self.content = content_root
		self.paths: set[str] = set()
		self.lists: set[str] = set()
		self.opaque: str | None = None
		self._saved: list[tuple[object, str, object]] = []

	# ================
	# relative
	#
	# A path's variant-relative POSIX form, or None outside the variant.
	# An fd, or anything not a path, is None: its file was opened by path.
	# ================
	def relative(self, path) -> str | None:
		if isinstance(path, int) or path is None:
			return None
		try:
			text = os.fsdecode(os.fspath(path))
		except TypeError:
			return None
		absolute = os.path.abspath(text)
		folded = os.path.normcase(absolute)
		if folded == self.folded_root:
			return ""
		if not folded.startswith(self.folded_root + os.sep):
			return None
		return absolute[len(self.root) + 1:].replace(os.sep, "/")

	# ================
	# note
	#
	# One access. The variant root and its content root are there for
	# the whole build; anything else outside the content root is no
	# effect's, so the step's reads cannot be keyed.
	# ================
	def note(self, path, listed: bool = False):
		relative = self.relative(path)
		if relative is None or relative in ("", self.content):
			if relative is not None and listed:
				self.lists.add(relative)
			return
		if not (relative + "/").casefold().startswith(self.content.casefold() + "/"):
			self.opaque = self.opaque or f"reads {relative} outside the content root"
			return
		(self.lists if listed else self.paths).add(relative)

	# ================
	# audit
	#
	# The audit hook's share: opens, listings, path probes, and the
	# events that make a step opaque.
	# ================
	def audit(self, event, args):
		if event == "open":
			mode = args[1] if len(args) > 1 else None
			flags = args[2] if len(args) > 2 else 0
			# A plain write ("w", O_TRUNC) replaces the file whatever it
			# held; any other open depends on what is there.
			if isinstance(mode, str):
				if "w" in mode and "+" not in mode:
					return
			elif flags & os.O_TRUNC and not flags & os.O_RDWR:
				return
			self.note(args[0])
		elif event in LIST_EVENTS:
			self.note(args[0] if args and args[0] is not None else ".", listed=True)
		elif event in PROBE_EVENTS:
			for position in PROBE_EVENTS[event]:
				if position < len(args):
					self.note(args[position])
		elif event in OPAQUE_EVENTS:
			self.opaque = self.opaque or f"ran {event}"

	# ================
	# start
	#
	# Wrap the path tests no audit event reports. Undone by stop.
	# ================
	def start(self):
		def wrapped(function):
			def test(path, *args, **kwargs):
				self.note(path)
				return function(path, *args, **kwargs)

			return test

		for module, names in ((os, STAT_FUNCTIONS), (os.path, PATH_TESTS)):
			for name in names:
				function = getattr(module, name, None)
				if function is not None:
					self._saved.append((module, name, function))
					setattr(module, name, wrapped(function))

	# ================
	# stop
	# ================
	def stop(self):
		for module, name, function in reversed(self._saved):
			setattr(module, name, function)
		self._saved.clear()

	# ================
	# ran_code
	#
	# The code files the step ran: any third-party file makes it opaque.
	# ================
	def ran_code(self, files):
		for path in files:
			if path and os.path.normcase(path).startswith(PACKAGE_CODE):
				self.opaque = self.opaque or f"ran installed package code {path}"
				return

	# ================
	# reads
	#
	# What the journal keeps: the sorted paths and listings, or OPAQUE.
	# ================
	def reads(self):
		if self.opaque is not None:
			return OPAQUE
		return {"paths": sorted(self.paths), "lists": sorted(self.lists)}


#============================================================================
# The tree the earlier steps' effects leave
#============================================================================


# ================
# TreeView
#
# The variant's content as the effects so far describe it: each file's
# content hash, and each directory's entries, kept as effects apply so
# a step's reads are looked up, never walked. Keys are casefolded.
# ================
class TreeView:
	def __init__(self):
		self.files: dict[str, str] = {}
		# folded directory -> {folded entry name: entry name as written}
		self.entries: dict[str, dict[str, str]] = {}

	# ================
	# copy
	# ================
	def copy(self) -> TreeView:
		view = TreeView()
		view.files = dict(self.files)
		view.entries = {folder: dict(names) for folder, names in self.entries.items()}
		return view

	# ================
	# apply
	#
	# One step's effect rows: [path, sha] lays a file, [path, None]
	# removes it, and a directory goes with its last entry.
	# ================
	def apply(self, rows):
		for relative, sha in rows:
			if sha is None:
				self.remove(relative)
			else:
				self.add(relative, sha)

	# ================
	# add
	# ================
	def add(self, relative: str, sha: str):
		folded = relative.casefold()
		if folded not in self.files:
			parent = ""
			for name in relative.split("/"):
				key = name.casefold()
				self.entries.setdefault(parent, {})[key] = name
				parent = f"{parent}/{key}" if parent else key
		self.files[folded] = sha

	# ================
	# remove
	# ================
	def remove(self, relative: str):
		folded = relative.casefold()
		if self.files.pop(folded, None) is None:
			return
		while folded:
			parent, _, name = folded.rpartition("/")
			names = self.entries.get(parent)
			if names is None:
				return
			names.pop(name, None)
			if names or not parent:
				return
			del self.entries[parent]
			folded = parent

	# ================
	# value
	#
	# What a path holds: its content hash, "dir", or "gone".
	# ================
	def value(self, relative: str) -> str:
		folded = relative.casefold()
		sha = self.files.get(folded)
		if sha is not None:
			return sha
		return "dir" if folded in self.entries else "gone"

	# ================
	# listing
	#
	# A directory's entry names, or the path's value when it holds none.
	# ================
	def listing(self, relative: str) -> str:
		names = self.entries.get(relative.casefold())
		if not names:
			return self.value(relative)
		return "\0".join(sorted(names.values()))


# ================
# tree_key
#
# A step's tree part: its recorded reads looked up in the tree in front
# of it. None when the step has no recorded reads (opaque, or journaled
# before reads were recorded): it keeps the chain.
# ================
def tree_key(view: TreeView, reads) -> str | None:
	if not isinstance(reads, dict):
		return None
	digest = hashlib.sha256()
	for relative in reads["paths"]:
		digest.update(f"path\0{relative}\0{view.value(relative)}\n".encode("utf-8"))
	for relative in reads["lists"]:
		digest.update(f"list\0{relative}\0{view.listing(relative)}\n".encode("utf-8"))
	return "tree:" + digest.hexdigest()
