"""
===========================================================================

variant_steps.py - each variant importer reruns only on its own change

build_alexandria_variant.py folds about thirty importers into one tree,
one after another. The whole-build stamp (variant_stamp.py) could only
skip all of them or rerun all of them: one changed input cost the whole
fold, 12-17 minutes, however late the changed input was read (#3993).

This module keeps a journal of the last successful build, one entry per
step: the step's key, the summary it returned, and its recorded effect -
every file it created or changed, by content hash, and every file it
removed. A step's key covers what the step reads:

	its code        the files its last run ran and imported, and every
	                checkout file it opened, plus what those import at
	                module level, the Node scripts they name and the pin
	                data beside them (codeStamp's moduleClosure), each
	                by checkout-relative path, so a fresh freeze
	                checkout keys the same
	its flags       the SRO_* variables named in that code, by value
	its sources     the source roots its last run opened files under,
	                fingerprinted as variant_stamp fingerprints them,
	                plus any loose file it opened outside those roots
	the tree before the accumulated effect of every earlier step,
	                chained by hash, so a step whose inputs came back
	                byte-identical is current again

A build whose journal still matches runs nothing: the tree is verified
against the journal (a file an outsider changed names its owning step).
Otherwise the steps from the first stale one on are rolled back - their
recorded effects undone from a content-addressed blob store beside the
journal - and the build walks forward again: a step whose key again
matches replays its recorded effect byte for byte instead of running,
and only a step whose key moved runs its importer, recording a fresh
effect. An importer whose summary cannot be journaled (a type the
journal cannot hold) simply runs on every build.

The journal and the blobs live in the tree's build state (.state), like
the settle ledger. Blobs are plain copies, never hard links: a linked
blob shares its inode with the tree file, and a rewrite of either in
place would rewrite the other.

===========================================================================
"""
from __future__ import annotations

import builtins
import hashlib
import json
import os
import re
import shutil
import sys
import sysconfig
import time
from pathlib import Path

import state_document
import variant_settle
import variant_stamp
from prepare_client_resources import file_digest, write_atomically
from sro_paths import REPO_ROOT, tree_state_root

JOURNAL_FORMAT = "sro-variant-steps"
# 2: a step's code is what it ran, keyed by repository-relative path (#4251).
# The runner's own code is no step's input; a change to what the journal
# means bumps this instead, and an older journal starts the record over.
JOURNAL_VERSION = 2
JOURNAL_NAME = "variant-steps.json"
BLOB_ROOT = "variant-blobs"
FLAG_LITERAL = re.compile(r"SRO_[A-Z0-9_]+")
# The chain every build starts from: the empty tree, in this format.
EMPTY_STATE = hashlib.sha256(f"sro-variant-steps/{JOURNAL_VERSION}".encode("ascii")).hexdigest()
# The sys.monitoring tool slot the code recorder takes while a step runs
# (0-2 and 5 are the debugger, coverage, profiler and optimizer slots).
CODE_TOOL = 4
# Where interpreter and package code lives: never a step's own code.
INSTALLED_CODE = tuple(
	str(Path(path).resolve()) for path in {sysconfig.get_path(name) for name in ("stdlib", "platstdlib", "purelib", "platlib")}
)


# ================
# journal_path
# ================
def journal_path(variant_root: Path) -> Path:
	return tree_state_root(variant_root) / JOURNAL_NAME


# ================
# blob_path
# ================
def blob_path(variant_root: Path, sha: str) -> Path:
	return tree_state_root(variant_root) / BLOB_ROOT / sha[:2] / sha


# ================
# store_blob_bytes
#
# The blob for these bytes, written if the store lacks it.
# ================
def store_blob_bytes(variant_root: Path, content: bytes) -> str:
	sha = hashlib.sha256(content).hexdigest()
	target = blob_path(variant_root, sha)
	if not target.exists():
		write_atomically(target, content)
	return sha


# ================
# store_blob_file
#
# The blob for a file's bytes. Always a copy, never a hard link: a
# linked blob would share its inode with the tree file, and a rewrite
# of either in place would rewrite the other.
# ================
def store_blob_file(variant_root: Path, source: Path) -> str:
	sha = file_digest(source)
	target = blob_path(variant_root, sha)
	if not target.exists():
		target.parent.mkdir(parents=True, exist_ok=True)
		shutil.copy2(source, target)
	return sha


# ================
# lay_blob
#
# Put a blob's bytes at target, as a copy (never a link, for the same
# reason store_blob_file copies). A target that already holds exactly
# these bytes is left alone.
# ================
def lay_blob(variant_root: Path, sha: str, target: Path) -> None:
	if target.is_file() and file_digest(target) == sha:
		return
	target.parent.mkdir(parents=True, exist_ok=True)
	if target.exists():
		target.unlink()
	shutil.copy2(blob_path(variant_root, sha), target)


#============================================================================
# A step's code: what it ran, and what that code names
#============================================================================


# ================
# is_installed_code
#
# A file of the interpreter or of an installed package: pinned by the
# toolchain, never a step's own code.
# ================
def is_installed_code(path: str) -> bool:
	return path.startswith("<") or path.startswith(INSTALLED_CODE)


# ================
# code_location
#
# How the journal names one code file: repository-relative inside the
# checkout, so a release built from a fresh freeze checkout keys every
# step exactly as the last one did (r204i: each new rel-<sha> checkout
# moved every key, #4251); absolute outside it.
# ================
def code_location(path: Path, repository_root: Path) -> str:
	try:
		return path.resolve().relative_to(repository_root.resolve()).as_posix()
	except ValueError:
		return str(path.resolve())


# ================
# code_path
#
# The file a recorded code location names under this checkout.
# ================
def code_path(location: str, repository_root: Path) -> Path:
	path = Path(location)
	return path if path.is_absolute() else repository_root / location


# ================
# code_key
#
# A digest of the step's code files, each by its location and content,
# and of the SRO_* flags they name: the step reruns when its code changes
# or when a flag it names moves. A file that is gone keys as gone.
# ================
def code_key(files: list[Path], repository_root: Path, environ) -> str:
	digest = hashlib.sha256()
	flags: dict[str, str] = {}
	for path in sorted(files, key=lambda item: code_location(item, repository_root)):
		digest.update(code_location(path, repository_root).encode("utf-8") + b"\0")
		try:
			content = path.read_bytes()
		except OSError:
			digest.update(b"gone\n")
			continue
		digest.update(hashlib.sha256(content).hexdigest().encode("ascii") + b"\n")
		for name in FLAG_LITERAL.findall(content.decode("utf-8", "ignore")):
			value = environ.get(name)
			flags[name] = hashlib.sha256(value.encode("utf-8")).hexdigest() if value is not None else ""
	for name in sorted(flags):
		digest.update(f"flag\0{name}\0{flags[name]}\n".encode("utf-8"))
	return digest.hexdigest()


#============================================================================
# The read log: what a run opened, and what that costs next time
#============================================================================

_recording: dict = {"reads": None}
_hook_installed = False


# ================
# audit_hook
#
# sys.addaudithook's open event, while a step runs: every path opened
# for reading is the step's to answer for. Writes are the effect's
# business (the tree diff sees them however they were made).
# ================
def audit_hook(event, args):
	if event != "open":
		return
	reads = _recording["reads"]
	if reads is None:
		return
	path = args[0]
	mode = args[1] if len(args) > 1 else None
	flags = args[2] if len(args) > 2 else 0
	if isinstance(mode, str):
		if any(mark in mode for mark in ("w", "a", "x", "+")):
			return
	elif flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_APPEND):
		return
	if isinstance(path, (str, os.PathLike)):
		reads.add(os.fspath(path))


# ================
# record_run
#
# Run one step with the recorders on; returns (result, the paths it
# opened for reading, the source files of the code it ran, those of the
# modules it only imported).
#
# The code is what ran, not what its modules could reach: the static
# import closure of any importer was nearly the whole build (160 files,
# 70 of the 98 modules in one import cycle), so one edit anywhere reran
# every step (#4251). sys.monitoring reports each function's first start
# and is then switched off for it, so the recorder costs one call per
# function, not per call. Constants are read without running code, so
# every module the step imports (an import statement that runs, even of
# a module already loaded) is recorded too, apart from the code that
# ran: module_closures takes the scripts a file names only from files
# that ran.
# ================
def record_run(run):
	global _hook_installed
	if not _hook_installed:
		sys.addaudithook(audit_hook)
		_hook_installed = True
	reads: set[str] = set()
	ran: set[str] = set()
	imported: set[str] = set()
	monitoring = sys.monitoring

	def started(code, offset):
		ran.add(code.co_filename)
		return monitoring.DISABLE

	original_import = builtins.__import__

	def recording_import(name, globals=None, locals=None, fromlist=(), level=0):
		module = original_import(name, globals, locals, fromlist, level)
		imported.add(module.__name__.partition(".")[0] if level else name.partition(".")[0])
		return module

	monitoring.use_tool_id(CODE_TOOL, "variant-steps")
	monitoring.register_callback(CODE_TOOL, monitoring.events.PY_START, started)
	monitoring.set_events(CODE_TOOL, monitoring.events.PY_START)
	# Code a previous step already reported was switched off; this step
	# must see it again.
	monitoring.restart_events()
	_recording["reads"] = reads
	builtins.__import__ = recording_import
	try:
		value = run()
	finally:
		builtins.__import__ = original_import
		_recording["reads"] = None
		monitoring.set_events(CODE_TOOL, 0)
		monitoring.register_callback(CODE_TOOL, monitoring.events.PY_START, None)
		monitoring.free_tool_id(CODE_TOOL)
	own = str(Path(__file__).resolve())

	def own_code(paths):
		return {str(Path(path).resolve()) for path in paths if path and not is_installed_code(path)} - {own}

	code = own_code(ran)
	modules = own_code(getattr(sys.modules.get(name), "__file__", None) for name in imported) - code
	return value, reads, sorted(code), sorted(modules)


#============================================================================
# The journal codec: a step's summary, held exactly
#============================================================================


class Unrecordable(Exception):
	"""A summary holds a type the journal cannot carry."""


# ================
# encode_result
#
# JSON, with bytes held as blobs and tuples kept tuples. Anything else
# the journal cannot hold raises Unrecordable, and the step just runs
# on every build.
# ================
def encode_result(value, variant_root: Path):
	if value is None or isinstance(value, (bool, int, float, str)):
		return value
	if isinstance(value, bytes):
		return {"@bytes": store_blob_bytes(variant_root, value)}
	if isinstance(value, tuple):
		return {"@tuple": [encode_result(item, variant_root) for item in value]}
	if isinstance(value, list):
		return [encode_result(item, variant_root) for item in value]
	if isinstance(value, dict):
		encoded = {}
		for key, item in value.items():
			if not isinstance(key, str):
				raise Unrecordable(f"a summary key of type {type(key).__name__}")
			encoded[key] = encode_result(item, variant_root)
		return encoded
	raise Unrecordable(f"a summary value of type {type(value).__name__}")


# ================
# decode_result
# ================
def decode_result(value, variant_root: Path):
	if isinstance(value, dict):
		if set(value) == {"@bytes"}:
			return blob_path(variant_root, value["@bytes"]).read_bytes()
		if set(value) == {"@tuple"}:
			return tuple(decode_result(item, variant_root) for item in value["@tuple"])
		return {key: decode_result(item, variant_root) for key, item in value.items()}
	if isinstance(value, list):
		return [decode_result(item, variant_root) for item in value]
	return value


#============================================================================
# Effects: what one step did to the tree
#============================================================================


# ================
# snapshot
#
# {relative path: (size, mtime_ns, absolute)} of the variant's content.
# ================
def snapshot(variant_root: Path) -> dict[str, tuple[int, int, Path]]:
	return {
		relative: (stat.st_size, stat.st_mtime_ns, absolute)
		for relative, absolute, stat in variant_settle.content_files(variant_root)
	}


# ================
# effect_between
#
# The rows one step changed between two snapshots: [path, sha] for a
# file it left, [path, None] for one it removed. Files the step wrote
# are blobbed as they are found.
# ================
def effect_between(variant_root: Path, before: dict, after: dict) -> list[list]:
	rows = []
	for relative in sorted(set(before) | set(after)):
		old, new = before.get(relative), after.get(relative)
		if new is None:
			rows.append([relative, None])
		elif old is None or old[:2] != new[:2]:
			rows.append([relative, store_blob_file(variant_root, new[2])])
	return rows


# ================
# effect_digest
# ================
def effect_digest(rows: list[list]) -> str:
	digest = hashlib.sha256()
	for relative, sha in rows:
		digest.update(f"{relative}\0{sha or '-'}\n".encode("utf-8"))
	return digest.hexdigest()


# ================
# chain
#
# The tree state's running name: each step's effect folded into the
# last, so a prefix that came back identical names identically.
# ================
def chain(state: str, rows: list[list]) -> str:
	return hashlib.sha256(f"{state}\0{effect_digest(rows)}".encode("ascii")).hexdigest()


# ================
# parts_key
#
# A step's key from its name and its key parts, in a fixed order.
# ================
def parts_key(name: str, parts: dict) -> str:
	ordered = [name] + [parts[part] for part in ("arguments", "later", "code", "sources", "state")]
	return hashlib.sha256("\0".join(ordered).encode("utf-8")).hexdigest()


# ================
# arguments_digest
#
# A step's own arguments by value, so a flag the builder reads for it
# (the shops' port_stock) keys the step that receives it. Paths key as
# their text, bytes by content, sets in order.
# ================
def arguments_digest(values) -> str:
	def plain(value):
		if isinstance(value, Path):
			return {"@path": str(value)}
		if isinstance(value, bytes):
			return {"@bytes": hashlib.sha256(value).hexdigest()}
		if isinstance(value, (tuple, list)):
			return [plain(item) for item in value]
		if isinstance(value, (set, frozenset)):
			return {"@set": sorted(json.dumps(plain(item), sort_keys=True) for item in value)}
		if isinstance(value, dict):
			return {str(key): plain(item) for key, item in value.items()}
		if value is None or isinstance(value, (bool, int, float, str)):
			return value
		raise Unrecordable(f"a step argument of type {type(value).__name__}")
	return hashlib.sha256(json.dumps(plain(values), sort_keys=True).encode("utf-8")).hexdigest()


#============================================================================
# The runner
#============================================================================


# ================
# Runner
#
# One build's walk over the registered steps. Register every step
# first (register), then begin() decides and rolls the tree back to
# the first step that must move, then result() answers each step in
# order - skipping, replaying or running it - and finish() journals
# the build that succeeded.
# ================
class Runner:
	def __init__(self, variant_root: Path, roots: list[Path], environ=None, repository_root: Path | None = None):
		self.variant = Path(variant_root)
		self.roots = [Path(root).resolve() for root in roots]
		self.environ = os.environ if environ is None else environ
		# The checkout code closures are taken from; None lets the code
		# stamp resolve this checkout itself (the step tests key a
		# scratch repository instead).
		self.repository_root = None if repository_root is None else Path(repository_root)
		# The checkout code locations are relative to.
		self.code_root = REPO_ROOT if repository_root is None else Path(repository_root)
		self.steps: list[dict] = []
		self.journal: list[dict] | None = None
		self.arguments: dict | None = None
		self.fresh = True
		self.first = 0
		self.state = EMPTY_STATE
		self.records: list[dict] = []
		self._root_fingerprints: dict[str, str] = {}
		self._code_keys: dict[tuple[str, ...], str] = {}

	# ================
	# register
	#
	# A step is a function and the arguments it is called with, never a
	# closure in the builder: the builder's code would then run inside
	# every step and key all of them (#4251). arguments are known now
	# and key the step by value; later is called just before the step
	# runs, outside the recorder, for the arguments an earlier step's
	# result supplies (the chain state answers for those). roots names
	# source roots the step reads without opening files under them (the
	# base step links and walks the native tree): they are fingerprinted
	# into its key on every build, audited or not.
	# ================
	def register(self, name: str, function, arguments=(), later=None, roots=()):
		self.steps.append({
			# Digested now: an argument the key cannot hold fails at
			# registration, not halfway through a build.
			"name": name, "function": function, "arguments": tuple(arguments), "later": later,
			"argumentsKey": arguments_digest(tuple(arguments)),
			"roots": [str(Path(root).resolve()) for root in roots],
		})

	# ================
	# load
	#
	# The journal is this build's when its arguments are this build's
	# and its steps are the registered ones; anything else (or nothing
	# readable) starts the record over.
	# ================
	def load(self, arguments: dict) -> bool:
		self.arguments = arguments
		body = state_document.read_document(journal_path(self.variant), JOURNAL_FORMAT, "journal", JOURNAL_VERSION)
		if not body or body.get("arguments") != arguments:
			return False
		entries = body.get("steps")
		if not isinstance(entries, list) or [entry.get("name") for entry in entries] != [s["name"] for s in self.steps]:
			return False
		self.journal = entries
		return True

	# ================
	# root_fingerprint
	#
	# One source root's fingerprint, computed at most once a build, the
	# same way the stamp takes it (variant_stamp.source_fingerprint).
	# ================
	def root_fingerprint(self, root: Path) -> str:
		key = str(root)
		if key not in self._root_fingerprints:
			arguments = self.arguments or {}
			self._root_fingerprints[key] = variant_stamp.source_fingerprint(
				root, arguments.get("native"), arguments.get("imported"))
		return self._root_fingerprints[key]

	# ================
	# recorded_code_key
	#
	# The key of the code a step's last run ran and imported, as this
	# checkout holds it now: those files, what they import at module
	# level, the pins beside them, and the scripts the files that ran name
	# (variant_stamp.module_closures). Steps with the same record share one
	# closure computation.
	# ================
	def recorded_code_key(self, ran: list[str], imported: list[str] = ()) -> str:
		key = (tuple(ran), tuple(imported))
		if key not in self._code_keys:
			self.key_code([key])
		return self._code_keys[key]

	# ================
	# key_code
	#
	# The code keys of these (ran, imported) records, in one closure
	# computation for all of them (variant_stamp.module_closures). A file
	# a record names that is gone keys as gone (code_key).
	# ================
	def key_code(self, keys: list[tuple]):
		records = []
		for ran, imported in keys:
			records.append((
				[code_path(location, self.code_root) for location in ran],
				[code_path(location, self.code_root) for location in imported],
			))
		closures = variant_stamp.module_closures(
			[([f for f in ran if f.is_file()], [f for f in imported if f.is_file()]) for ran, imported in records],
			self.repository_root,
		)
		for key, (ran, imported), closure in zip(keys, records, closures):
			files = set(closure) | set(ran) | set(imported)
			self._code_keys[key] = code_key(sorted(files), self.code_root, self.environ)

	# ================
	# key_journal_code
	#
	# Every journaled step's code key at once, before begin or explain
	# walks the steps: one Node start for the build instead of one a step.
	# ================
	def key_journal_code(self):
		keys = []
		for entry in self.journal or []:
			reads = entry.get("reads") or {}
			key = (tuple(reads.get("code", [])), tuple(reads.get("imported", [])))
			if key not in self._code_keys and key not in keys:
				keys.append(key)
		if keys:
			self.key_code(keys)

	# ================
	# sources_key
	#
	# What a step's recorded reads cost now: each source root it read
	# under, refingerprinted, and each loose file outside them, taken
	# again. A step with no record reads as a sentinel nothing matches.
	# ================
	def sources_key(self, recorded: dict | None) -> str:
		if recorded is None:
			return "unrecorded"
		digest = hashlib.sha256()
		for root in recorded.get("roots", []):
			digest.update(f"root\0{root}\0{self.root_fingerprint(Path(root))}\n".encode("utf-8"))
		for path in sorted(recorded.get("files", {})):
			target = Path(path)
			key = variant_stamp.file_key(target, True) if target.is_file() else "gone"
			digest.update(f"file\0{path}\0{key}\n".encode("utf-8"))
		return digest.hexdigest()

	# ================
	# classify_reads
	#
	# A run's raw read paths and code as the journal holds them: the
	# source roots touched; the code it ran with every checkout data file
	# it opened (a pin, a table beside the code), and the modules it only
	# imported with every checkout .py the importer opened, both by
	# checkout-relative location, so a fresh freeze checkout keys the
	# same; and the loose files outside both, each with the key it must
	# keep. Compiled caches (__pycache__) follow their source and are not
	# recorded.
	# ================
	def classify_reads(self, reads: set[str], code: list[str] = (), imported: list[str] = ()) -> dict:
		own = str(self.variant.resolve())
		checkout = self.code_root.resolve()
		roots: set[str] = set()
		files: dict[str, str] = {}
		locations = {code_location(Path(path), checkout) for path in code}
		modules = {code_location(Path(path), checkout) for path in imported}
		for raw in reads:
			try:
				path = Path(raw).resolve()
			except (OSError, RuntimeError):
				continue
			text = str(path)
			if text.startswith(own) or is_installed_code(text) or "__pycache__" in path.parts:
				continue
			owner = next((root for root in self.roots if path == root or root in path.parents), None)
			if owner is not None:
				roots.add(str(owner))
			elif checkout == path or checkout in path.parents:
				if path.is_file():
					# A module's source is opened by its import, which is not a
					# sign it ran; monitoring says which code did.
					(modules if path.suffix == ".py" else locations).add(code_location(path, checkout))
			elif path.is_file():
				files[text] = variant_stamp.file_key(path, True)
		return {
			"roots": sorted(roots), "code": sorted(locations), "imported": sorted(modules - locations),
			"files": files,
		}

	# ================
	# key_parts
	#
	# What a step's key is made of, by name, so a stale step can say which
	# part moved (explain): its arguments, those an earlier step's result
	# supplied (later_key), its code, its sources, and the chain state in
	# front of it.
	# ================
	def key_parts(self, step: dict, recorded_reads: dict | None, state: str, later_key: str = "") -> dict:
		if recorded_reads is not None and step["roots"]:
			recorded_reads = {
				**recorded_reads,
				"roots": sorted(set(recorded_reads.get("roots", [])) | set(step["roots"])),
			}
		code = "unrecorded" if recorded_reads is None else self.recorded_code_key(
			recorded_reads.get("code", []), recorded_reads.get("imported", [])
		)
		return {
			"arguments": step["argumentsKey"], "later": later_key, "code": code,
			"sources": self.sources_key(recorded_reads), "state": state,
		}

	# ================
	# key_of
	#
	# The key a step must match to skip: its name and its key parts.
	# ================
	def key_of(self, step: dict, recorded_reads: dict | None, state: str, later_key: str = "") -> str:
		return parts_key(step["name"], self.key_parts(step, recorded_reads, state, later_key))

	# ================
	# explain
	#
	# The smoke for a code change (#4251): with the journal loaded and
	# before begin, each step's verdict as begin would reach it - current,
	# or stale with the key parts that moved - in seconds, without
	# building anything. A step after the first stale one is judged
	# against the journal's chain, so it reads as it would if the earlier
	# ones came back byte-identical.
	# ================
	def explain(self) -> list[tuple[str, str]]:
		if self.journal is None:
			return [(step["name"], "no usable journal: every step runs") for step in self.steps]
		lines = []
		state = EMPTY_STATE
		self.key_journal_code()
		for step, entry in zip(self.steps, self.journal):
			now = self.key_parts(step, entry.get("reads"), state, entry.get("laterKey", ""))
			if parts_key(step["name"], now) == entry.get("key"):
				lines.append((step["name"], "current"))
			else:
				was = entry.get("parts") or {}
				moved = [name for name in now if was.get(name) != now[name]] if was else ["unrecorded parts"]
				lines.append((step["name"], "stale: " + ", ".join(moved)))
			state = chain(state, entry["effect"])
		return lines

	# ================
	# begin
	#
	# Decide the build and roll the tree back to the first step that
	# must move: the first whose key moved, or the first whose files
	# an outsider touched, whichever is earlier. A forced or
	# journal-less build starts from the cleared tree instead.
	# ================
	def begin(self, force: bool = False):
		self.fresh = force or self.journal is None
		if self.fresh:
			self.first = 0
			return
		first = len(self.steps)
		state = EMPTY_STATE
		self.key_journal_code()
		for index, (step, entry) in enumerate(zip(self.steps, self.journal)):
			# Over a prefix that still matches, every earlier result is the
			# journal's, so the arguments they supplied are the recorded ones.
			if entry.get("key") != self.key_of(step, entry.get("reads"), state, entry.get("laterKey", "")):
				first = index
				break
			state = chain(state, entry["effect"])
		dirty = self.dirty_step()
		if dirty is not None and dirty < first:
			first = dirty
		self.first = first
		if first < len(self.steps):
			self.rollback(first)

	# ================
	# final_state
	#
	# ({relative path: sha} the journal's build left, each path's
	# owning step index).
	# ================
	def final_state(self) -> tuple[dict[str, str], dict[str, int]]:
		state: dict[str, str] = {}
		owners: dict[str, int] = {}
		for index, entry in enumerate(self.journal or []):
			for relative, sha in entry["effect"]:
				if sha is None:
					state.pop(relative, None)
					owners.pop(relative, None)
				else:
					state[relative] = sha
					owners[relative] = index
		return state, owners

	# ================
	# dirty_step
	#
	# The earliest step whose recorded files the tree no longer holds,
	# or None when the tree is exactly the journal's. A file no step
	# recorded is a stray (a failed build's leavings, an outsider's
	# drop): it is removed and names no step. Files whose size and
	# time still match the settle ledger are trusted unread, as
	# settle trusts them; the rest are hashed.
	# ================
	def dirty_step(self) -> int | None:
		expected, owners = self.final_state()
		ledger = variant_settle.load_ledger(variant_settle.ledger_path(self.variant))
		dirty: int | None = None
		seen: set[str] = set()
		for relative, absolute, stat in variant_settle.content_files(self.variant):
			seen.add(relative)
			sha = expected.get(relative)
			if sha is None:
				absolute.unlink(missing_ok=True)
				continue
			known = ledger.get(relative)
			if known and known[0] == stat.st_size and known[1] == stat.st_mtime_ns:
				continue
			if file_digest(absolute) != sha and (dirty is None or owners[relative] < dirty):
				dirty = owners[relative]
		for relative, index in owners.items():
			if relative not in seen and (dirty is None or index < dirty):
				dirty = index
		return dirty

	# ================
	# rollback
	#
	# Undo every step from the last down to `first`: each file goes
	# back to the bytes the steps before it left (or goes away), so
	# the walk forward starts from exactly the recorded prefix.
	# ================
	def rollback(self, first: int):
		befores: list[dict[str, str | None]] = []
		current: dict[str, str] = {}
		for entry in self.journal or []:
			befores.append({relative: current.get(relative) for relative, _ in entry["effect"]})
			for relative, sha in entry["effect"]:
				if sha is None:
					current.pop(relative, None)
				else:
					current[relative] = sha
		for index in range(len(self.journal or []) - 1, first - 1, -1):
			for relative, _ in (self.journal or [])[index]["effect"]:
				before = befores[index][relative]
				target = self.variant / relative
				if before is None:
					target.unlink(missing_ok=True)
				else:
					lay_blob(self.variant, before, target)
		# Directories the undone steps leave empty go with them.
		content = self.variant / variant_settle.CONTENT_ROOT
		if content.is_dir():
			for folder, _, _ in os.walk(content, topdown=False):
				if Path(folder) != content:
					try:
						Path(folder).rmdir()
					except OSError:
						pass

	# ================
	# result
	#
	# One step's summary, in registration order. Before the first
	# moved step the journal's word is enough (its files were
	# verified); from there, a step whose key still matches replays
	# its recorded effect onto the rolled-back tree, and a step whose
	# key moved runs, recording a fresh effect.
	# ================
	def result(self, index: int):
		started = time.perf_counter()
		value, how = self.settle_step(index)
		# One line a step, so a slow build names the step that cost it (#4251).
		print(f"step {self.steps[index]['name']}: {how} in {time.perf_counter() - started:.1f} s", flush=True)
		return value

	# ================
	# settle_step
	#
	# One step brought to its journaled state: kept (current), its effect
	# laid back down (replayed), or its importer run (ran).
	# ================
	def settle_step(self, index: int):
		step = self.steps[index]
		entry = None if self.fresh else (self.journal or [])[index]
		if entry is not None and index < self.first:
			self.state = chain(self.state, entry["effect"])
			self.records.append(entry)
			return decode_result(entry["result"], self.variant), "current"
		# Every earlier step is settled, so their results are in hand.
		later = tuple(step["later"]()) if step["later"] else ()
		later_key = arguments_digest(later) if later else ""
		if entry is not None and entry.get("key") == self.key_of(step, entry.get("reads"), self.state, later_key):
			for relative, sha in entry["effect"]:
				target = self.variant / relative
				if sha is None:
					target.unlink(missing_ok=True)
				else:
					lay_blob(self.variant, sha, target)
			self.state = chain(self.state, entry["effect"])
			self.records.append(entry)
			return decode_result(entry["result"], self.variant), "replayed"
		arguments = step["arguments"] + later
		before = snapshot(self.variant)
		value, reads, code, imported = record_run(lambda: step["function"](*arguments))
		effect = effect_between(self.variant, before, snapshot(self.variant))
		classified = self.classify_reads(reads, code, imported)
		try:
			encoded = encode_result(value, self.variant)
			parts = self.key_parts(step, classified, self.state, later_key)
			key = parts_key(step["name"], parts)
		except Unrecordable:
			encoded, key, parts = None, None, None
		self.state = chain(self.state, effect)
		self.records.append({
			"name": step["name"], "key": key, "parts": parts, "reads": classified, "effect": effect,
			"result": encoded, "laterKey": later_key,
		})
		return value, "ran"

	# ================
	# finish
	#
	# Journal the build that succeeded, in the state beside the tree.
	# ================
	def finish(self):
		state_document.write_document(
			journal_path(self.variant), JOURNAL_FORMAT, "journal",
			{"arguments": self.arguments, "steps": self.records}, JOURNAL_VERSION
		)
		# The ledger the next build's verification trusts is settled here,
		# over the tree the journal now describes (the build's own settle
		# in main() afterwards finds nothing left to do).
		variant_settle.settle(self.variant)
