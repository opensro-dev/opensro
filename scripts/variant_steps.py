"""
===========================================================================

variant_steps.py - each variant importer runs only when what it reads changed

build_alexandria_variant.py folds about thirty importers, in a load-bearing
order, over one tree: each reads the tables the ones before it wrote and
appends its own rows. variant_stamp.py skips the whole build when nothing
changed, but any change to any importer reran all of them (12.5 minutes for
r203q). This module runs the fold one step at a time and replays a step
from its record when nothing the step could see has changed since it last
ran.

A step's record is found by the state the tree held before it (a chain:
the build's inputs, then each earlier step's name, delta and value) and is
reused only while these still match:

	code     every repository module loaded when the step ended, by
	         content. Loaded, not merely executed: a module's constants
	         and the memo tables earlier steps filled are code the step can
	         see, so a change to any module loaded by then reruns it
	reads    every other repository file the step opened (archive pins,
	         data tables); the build's source roots are in the chain's
	         first state already (variant_stamp.read_inputs)
	scripts  every file a child process was started on: a repository
	         script by its code closure (codeStamp.mjs), a tool such as
	         ffmpeg by size and time

What a step did is recorded through Python's audit hooks, so the importers
need no change: every file opened for writing, renamed, linked, removed or
re-timed under the content tree, every tree removed, every folder made. A
replay applies the same end state: the removed trees and files go, the
written files come back from a content-named store beside the tree with
the time they had, and the step's return value is decoded. A step whose
effects the hooks cannot see in full (a write outside the content tree
and the temp folder, a writable map, a shell, a path relative to a folder
handle) or whose value has no exact encoding is run every time and says so.

The chain makes a change cost what it should: an unchanged prefix replays,
the first step that can see a change and every step after it run (they
read what it wrote). A rerun step whose delta and value come out the same
as before leaves the chain where it was, so the steps after it replay.

Store blobs are copies, never links: the asset build and later steps write
the content tree, and a link would carry their writes into the store.

===========================================================================
"""
from __future__ import annotations

import base64
import hashlib
import json
import os
import shutil
import sys
import tempfile
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

import state_document
from prepare_client_resources import file_digest
from sro_paths import REPO_ROOT, tree_state_root

STEP_FORMAT = "sro-variant-step"
STEP_VERSION = 1
STORE_NAME = "variant-steps"
# Repository files a step's code is: Python modules. Everything else a
# step opens in the repository is a read.
CODE_SUFFIXES = frozenset((".py",))
# Compiled caches the import system opens beside the modules; their source
# is the code record.
IGNORED_SUFFIXES = frozenset((".pyc", ".pyd"))
SCRIPT_SUFFIXES = frozenset((".mjs", ".js", ".cjs"))
# os.open flags that make an open a write.
WRITE_FLAGS = os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_CREAT | os.O_TRUNC
# Audit events whose first argument is a path the event changes.
CHANGE_EVENTS = frozenset(("os.remove", "os.truncate", "os.utime", "os.chmod"))
# Audit events that start a process the hooks cannot follow.
SHELL_EVENTS = frozenset(("os.system", "os.exec", "os.spawn", "os.posix_spawn", "os.startfile"))
# The folder handle an audit event reports when a call names none.
NO_FOLDER_HANDLE = -1
# Where the import system writes compiled modules.
BYTECODE_FOLDER = os.sep + "__pycache__" + os.sep
# mmap access modes that can write: ACCESS_DEFAULT and ACCESS_WRITE.
WRITABLE_MAPS = frozenset((0, 2))


#============================================================================
# Recording
#============================================================================


# ================
# norm
#
# A comparable form of a path argument, or None for a descriptor.
# ================
def norm(value) -> str | None:
	if isinstance(value, int):
		return None
	try:
		path = os.fspath(value)
	except TypeError:
		return None
	if isinstance(path, bytes):
		path = os.fsdecode(path)
	return os.path.normcase(os.path.abspath(path))


# ================
# Recorder
#
# What one step did, gathered by the audit hook while the step runs.
# ================
@dataclass
class Recorder:
	out: str
	tree: str
	covered: tuple[str, ...]
	written: dict[str, str] = field(default_factory=dict)
	trees: list[str] = field(default_factory=list)
	dirs: dict[str, str] = field(default_factory=dict)
	reads: set[str] = field(default_factory=set)
	scripts: set[str] = field(default_factory=set)
	blind: list[str] = field(default_factory=list)

	def inside(self, key: str) -> bool:
		return key.startswith(self.out + os.sep)

	# A path under the variant root but outside out (its manifest, its
	# build state) is never ignored, even when the root is in temp.
	def ignored(self, key: str) -> bool:
		if key == self.tree or key.startswith(self.tree + os.sep):
			return False
		return any(key == root or key.startswith(root + os.sep) for root in self.covered)

	def write(self, value, event: str) -> None:
		key = norm(value)
		if key is None:
			# A descriptor comes from an earlier open by path (recorded
			# then), a pipe or a socket: nothing new to record.
			return
		if os.path.splitext(key)[1] in IGNORED_SUFFIXES or BYTECODE_FOLDER in key:
			# The import system caching a module's bytecode, not a step's effect.
			return
		if self.inside(key):
			self.written.setdefault(key, os.path.abspath(os.fsdecode(os.fspath(value))))
		elif key != self.out and not self.ignored(key):
			self.blind.append(f"{event} {value}")

	def read(self, value) -> None:
		key = norm(value)
		if key is None or self.inside(key) or self.ignored(key):
			return
		suffix = os.path.splitext(key)[1]
		if suffix in CODE_SUFFIXES or suffix in IGNORED_SUFFIXES:
			return
		if key.startswith(REPO_KEY + os.sep) and os.path.isfile(key):
			self.reads.add(key)


REPO_KEY = os.path.normcase(os.path.abspath(REPO_ROOT))
_active: Recorder | None = None
_installed = False


# ================
# audit
#
# The process-wide hook: a no-op unless a step is recording.
# ================
def audit(event: str, args) -> None:
	recorder = _active
	if recorder is None:
		return
	if event == "open":
		path, mode, flags = args
		if isinstance(mode, str):
			writes = any(c in mode for c in "wax+")
		else:
			writes = bool((flags or 0) & WRITE_FLAGS)
		if writes:
			recorder.write(path, "open")
		else:
			recorder.read(path)
	elif event in CHANGE_EVENTS:
		recorder.write(args[0], event)
	elif event in ("os.rename", "os.link", "os.symlink"):
		if event == "os.rename":
			recorder.write(args[0], event)
		else:
			recorder.read(args[0])
		recorder.write(args[1], event)
		if any(fd is not None and fd != NO_FOLDER_HANDLE for fd in args[2:]):
			recorder.blind.append(f"{event} relative to a folder handle")
	elif event in ("shutil.copyfile", "shutil.copymode", "shutil.copystat"):
		recorder.read(args[0])
		recorder.write(args[1], event)
	elif event in ("shutil.rmtree", "os.rmdir"):
		key = norm(args[0])
		if key is not None and recorder.inside(key):
			recorder.trees.append(os.path.abspath(os.fsdecode(os.fspath(args[0]))))
		else:
			recorder.write(args[0], event)
	elif event == "os.mkdir":
		key = norm(args[0])
		if key is not None and recorder.inside(key):
			recorder.dirs.setdefault(key, os.path.abspath(os.fsdecode(os.fspath(args[0]))))
		elif key is None or not recorder.ignored(key):
			recorder.write(args[0], event)
	elif event == "subprocess.Popen":
		executable, argv = args[0], args[1]
		for part in [executable, *(argv if isinstance(argv, (list, tuple)) else [argv])]:
			if part is None:
				continue
			candidate = shutil.which(str(part)) if isinstance(part, str) and not os.path.isabs(part) else part
			key = norm(candidate) if candidate else None
			if key and os.path.isfile(key):
				recorder.scripts.add(key)
	elif event in SHELL_EVENTS:
		recorder.blind.append(event)
	elif event == "mmap.__new__" and args[2] in WRITABLE_MAPS:
		recorder.blind.append("a writable map")


# ================
# install
# ================
def install() -> None:
	global _installed
	if not _installed:
		sys.addaudithook(audit)
		_installed = True


#============================================================================
# Values
#============================================================================


class Unencodable(Exception):
	pass


# ================
# encode_value
#
# An exact JSON form of a step's return value: bytes, tuples, paths and
# non-string keys keep their type. Anything else is Unencodable.
# ================
def encode_value(value):
	kind = type(value)
	if value is None or kind in (bool, int, float, str):
		return value
	if kind is bytes:
		return {"$bytes": base64.b64encode(value).decode("ascii")}
	if kind is list:
		return [encode_value(item) for item in value]
	if kind is tuple:
		return {"$tuple": [encode_value(item) for item in value]}
	if isinstance(value, Path):
		return {"$path": str(value)}
	if kind is dict:
		if all(type(key) is str and not key.startswith("$") for key in value):
			return {key: encode_value(item) for key, item in value.items()}
		return {"$dict": [[encode_value(key), encode_value(item)] for key, item in value.items()]}
	raise Unencodable(kind.__name__)


# ================
# decode_value
# ================
def decode_value(value):
	if isinstance(value, list):
		return [decode_value(item) for item in value]
	if not isinstance(value, dict):
		return value
	if "$bytes" in value:
		return base64.b64decode(value["$bytes"])
	if "$tuple" in value:
		return tuple(decode_value(item) for item in value["$tuple"])
	if "$path" in value:
		return Path(value["$path"])
	if "$dict" in value:
		return {decode_value(key): decode_value(item) for key, item in value["$dict"]}
	return {key: decode_value(item) for key, item in value.items()}


#============================================================================
# Store
#============================================================================


# ================
# Step
#
# One importer: a name unique in the build and a function of no arguments
# that returns the importer's value.
# ================
@dataclass(frozen=True)
class Step:
	name: str
	run: Callable[[], object]


# ================
# digest
# ================
def digest(*parts) -> str:
	hasher = hashlib.sha256()
	for part in parts:
		hasher.update(json.dumps(part, sort_keys=True, separators=(",", ":")).encode("utf-8"))
		hasher.update(b"\0")
	return hasher.hexdigest()


# ================
# StepStore
#
# Records and blobs beside the variant tree, in its build state.
# ================
class StepStore:
	def __init__(self, variant_root: Path):
		self.root = tree_state_root(variant_root) / STORE_NAME
		self.used_records: set[str] = set()
		self.used_blobs: set[str] = set()

	def record_path(self, key: str) -> Path:
		return self.root / "records" / f"{key}.json"

	def blob_path(self, sha: str) -> Path:
		return self.root / "blobs" / sha[:2] / sha

	def load(self, key: str) -> dict | None:
		return state_document.read_document(self.record_path(key), STEP_FORMAT, "step", STEP_VERSION)

	def save(self, key: str, record: dict) -> None:
		state_document.write_document(self.record_path(key), STEP_FORMAT, "step", record, STEP_VERSION)

	def put_blob(self, source: Path, sha: str) -> None:
		target = self.blob_path(sha)
		if target.is_file():
			return
		target.parent.mkdir(parents=True, exist_ok=True)
		partial = target.with_name(target.name + ".partial")
		shutil.copyfile(source, partial)
		os.replace(partial, target)

	# ================
	# prune
	#
	# After a build, only what that build used stays: records of other
	# chains and blobs no kept record names are removed.
	# ================
	def prune(self) -> None:
		for folder, keep in (("records", {f"{key}.json" for key in self.used_records}), ("blobs", None)):
			base = self.root / folder
			if not base.is_dir():
				continue
			for directory, _, names in os.walk(base):
				for name in names:
					if (keep is not None and name not in keep) or (keep is None and name not in self.used_blobs):
						Path(directory, name).unlink(missing_ok=True)


#============================================================================
# Running
#============================================================================


# ================
# StepRunner
#
# Runs the fold over out. state is the chain: the digest of the build's
# inputs, then of every step taken.
# ================
class StepRunner:
	def __init__(self, out: Path, store: StepStore, inputs: dict, log: Callable[[str], None]):
		self.out = Path(out)
		self.store = store
		self.log = log
		self.state = digest(STEP_FORMAT, STEP_VERSION, sys.version, {k: v for k, v in inputs.items() if k != "code"})
		self.covered = tuple(sorted({norm(root) for root in (tempfile.gettempdir(), sys.prefix, sys.base_prefix)}))
		self.digests: dict[str, str] = {}
		self.script_keys: dict[str, str] = {}
		self.reused = 0
		self.saved = 0.0
		install()

	def content(self, key: str) -> str | None:
		if key not in self.digests:
			self.digests[key] = file_digest(key) if os.path.isfile(key) else None
		return self.digests[key]

	def script_key(self, key: str) -> str | None:
		if key not in self.script_keys:
			if not os.path.isfile(key):
				self.script_keys[key] = None
			elif key.startswith(REPO_KEY + os.sep) and os.path.splitext(key)[1] in SCRIPT_SUFFIXES:
				import variant_stamp
				self.script_keys[key] = variant_stamp.code_stamp("codeHash", Path(key))
			else:
				stat = os.stat(key)
				self.script_keys[key] = f"{stat.st_size}\0{stat.st_mtime_ns}"
		return self.script_keys[key]

	def loaded_code(self) -> dict[str, str]:
		code = {}
		for module in list(sys.modules.values()):
			path = getattr(module, "__file__", None)
			if not path:
				continue
			key = os.path.normcase(os.path.abspath(path))
			if key.startswith(REPO_KEY + os.sep) and os.path.splitext(key)[1] in CODE_SUFFIXES:
				code[key] = self.content(key)
		return code

	# ================
	# stale
	#
	# Why a record cannot be replayed now, or None.
	# ================
	def stale(self, record: dict) -> str | None:
		for key, sha in record["code"].items():
			if self.content(key) != sha:
				return f"code {os.path.relpath(key, REPO_KEY)}"
		for key, sha in record["reads"].items():
			if self.content(key) != sha:
				return f"read {os.path.relpath(key, REPO_KEY)}"
		for key, stamp in record["scripts"].items():
			if self.script_key(key) != stamp:
				return f"script {key}"
		for _, sha, _ in record["delta"]["files"]:
			if sha is not None and not self.store.blob_path(sha).is_file():
				return "a stored file is missing"
		return None

	# ================
	# take
	#
	# One step: replayed from its record, or run and recorded.
	# ================
	def take(self, step: Step):
		key = digest(self.state, step.name)
		record = self.store.load(key)
		reason = "no record" if record is None else self.stale(record)
		if reason is None:
			started = time.monotonic()
			self.replay(record["delta"])
			value = decode_value(record["value"])
			self.reused += 1
			self.saved += max(0.0, record["seconds"] - (time.monotonic() - started))
			self.log(f"step {step.name}: replayed ({len(record['delta']['files'])} files, {record['seconds']:.0f} s saved)")
			self.keep(key, record)
			self.state = digest(self.state, step.name, record["delta_digest"], record["value"])
			return value
		value, record = self.run(step)
		if record is None:
			# Not replayable: the chain still moves, by a state no record shares.
			self.state = digest(self.state, step.name, os.urandom(16).hex())
			return value
		self.log(f"step {step.name}: ran in {record['seconds']:.0f} s ({reason})")
		self.store.save(key, record)
		self.keep(key, record)
		self.state = digest(self.state, step.name, record["delta_digest"], record["value"])
		return value

	def keep(self, key: str, record: dict) -> None:
		self.store.used_records.add(key)
		self.store.used_blobs.update(sha for _, sha, _ in record["delta"]["files"] if sha is not None)

	def run(self, step: Step):
		global _active
		recorder = Recorder(norm(self.out), norm(self.out.parent), self.covered)
		started = time.monotonic()
		_active = recorder
		try:
			value = step.run()
		finally:
			_active = None
		seconds = time.monotonic() - started
		try:
			encoded = encode_value(value)
		except Unencodable as error:
			recorder.blind.append(f"a {error} value")
		if recorder.blind:
			self.log(f"step {step.name}: ran in {seconds:.0f} s, not recorded: {recorder.blind[0]}")
			return value, None
		delta = self.delta(recorder)
		record = {
			"step": step.name,
			"seconds": seconds,
			"code": self.loaded_code(),
			"reads": {key: self.content(key) for key in sorted(recorder.reads)},
			"scripts": {key: self.script_key(key) for key in sorted(recorder.scripts)},
			"delta": delta,
			# Bytes, not times: a rerun that writes what it wrote before
			# leaves the chain where it was.
			"delta_digest": digest([entry[:2] for entry in delta["files"]], delta["trees"], delta["dirs"]),
			"value": encoded
		}
		return value, record

	# ================
	# delta
	#
	# The end state of everything the step touched, relative to out. A
	# written file is stored by content; a touched path that is gone is
	# recorded as removed.
	# ================
	def delta(self, recorder: Recorder) -> dict:
		files = []
		for key in sorted(recorder.written):
			relative = self.disk_relative(recorder.written[key])
			if os.path.isfile(key):
				sha = file_digest(key)
				self.store.put_blob(Path(key), sha)
				files.append([relative, sha, os.stat(key).st_mtime_ns])
			elif not os.path.exists(key):
				files.append([relative, None, 0])
		trees = [Path(tree).relative_to(self.out).as_posix() for tree in recorder.trees]
		dirs = sorted(self.disk_relative(path) for key, path in recorder.dirs.items() if os.path.isdir(key))
		return {"files": files, "trees": trees, "dirs": dirs}

	# ================
	# disk_relative
	#
	# A touched path relative to out, spelled as the disk holds it. On a
	# case-insensitive disk a folder keeps the spelling of whoever made it
	# first (an earlier step's "arabia"), while a later step may write
	# through "Arabia/"; recording the step's own spelling made a replay
	# create "Arabia" and change 1,771 paths. realpath reads the disk's
	# names (on Windows, the final path name); a path gone by the step's
	# end, or one realpath leads outside out, keeps the step's spelling.
	# ================
	def disk_relative(self, path: str) -> str:
		written = Path(path).relative_to(self.out).as_posix()
		if not os.path.exists(path):
			return written
		try:
			real = Path(os.path.realpath(path)).relative_to(os.path.realpath(self.out)).as_posix()
		except ValueError:
			return written
		return real if real.lower() == written.lower() else written

	# ================
	# replay
	# ================
	def replay(self, delta: dict) -> None:
		for tree in delta["trees"]:
			target = self.out / tree
			if target.is_dir():
				shutil.rmtree(target)
			else:
				target.unlink(missing_ok=True)
		for relative, sha, _ in delta["files"]:
			if sha is None:
				(self.out / relative).unlink(missing_ok=True)
		for relative in delta["dirs"]:
			(self.out / relative).mkdir(parents=True, exist_ok=True)
		for relative, sha, mtime in delta["files"]:
			if sha is None:
				continue
			target = self.out / relative
			target.parent.mkdir(parents=True, exist_ok=True)
			partial = target.with_name(target.name + ".replay")
			shutil.copyfile(self.store.blob_path(sha), partial)
			os.replace(partial, target)
			os.utime(target, ns=(mtime, mtime))
		# A replayed file's bytes are known; a later step's read of it is not
		# hashed again.
		for relative, sha, _ in delta["files"]:
			self.digests.pop(norm(self.out / relative), None)

	def summary(self) -> str:
		return f"{self.reused} steps replayed, about {self.saved:.0f} s saved"
