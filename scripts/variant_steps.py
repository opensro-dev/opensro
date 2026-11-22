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

	its code        the defining module and its static local imports
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
the settle ledger. Blobs are hard links into the store where the file
system allows, so recording a build costs almost no extra bytes.

===========================================================================
"""
from __future__ import annotations

import hashlib
import os
import re
import shutil
import sys
from pathlib import Path

import state_document
import variant_settle
import variant_stamp
from prepare_client_resources import file_digest, write_atomically
from sro_paths import tree_state_root

JOURNAL_FORMAT = "sro-variant-steps"
JOURNAL_VERSION = 1
JOURNAL_NAME = "variant-steps.json"
BLOB_ROOT = "variant-blobs"
FLAG_LITERAL = re.compile(r"SRO_[A-Z0-9_]+")
# The chain every build starts from: the empty tree, in this format.
EMPTY_STATE = hashlib.sha256(b"sro-variant-steps/1").hexdigest()


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
# A step's code: the defining module and its static local imports
#============================================================================


# ================
# module_file
# ================
def module_file(module_name: str) -> Path | None:
	module = sys.modules.get(module_name)
	path = getattr(module, "__file__", None)
	return Path(path) if path else None


FROM_LINE = re.compile(rb"^\s*from\s+([A-Za-z_][\w.]*)\s+import\b", re.MULTILINE)
IMPORT_LINE = re.compile(rb"^\s*import\s+([^\n#(]+)", re.MULTILINE)


# ================
# imported_names
#
# The top-level module names one Python file imports, by its own text:
# the closure below follows what a module can reach, not what one run
# happened to import first. A line scan, not a parse: every import
# statement names its module on its first line, a match inside a
# string or comment only widens the closure (the safe direction), and
# the scan costs bytes where an AST walk over a data module costs
# seconds.
# ================
def imported_names(path: Path) -> set[str]:
	try:
		content = path.read_bytes()
	except OSError:
		return set()
	names = {match.group(1).split(b".")[0].decode("ascii") for match in FROM_LINE.finditer(content)}
	for match in IMPORT_LINE.finditer(content):
		for part in match.group(1).split(b","):
			token = part.strip().split(None, 1)
			if token and re.fullmatch(rb"[A-Za-z_][\w.]*", token[0]):
				names.add(token[0].split(b".")[0].decode("ascii"))
	return names


_module_files: dict = {}


# ================
# find_module_file
#
# A top-level module's file beside the importing file or on sys.path,
# without importing it. Resolutions are kept for the process, keyed by
# the sys.path they were made under: a closure walk asks for the same
# names from every file it visits.
# ================
def find_module_file(name: str, beside: Path) -> Path | None:
	key = (name, str(beside), tuple(sys.path))
	if key not in _module_files:
		found = None
		for folder in [beside, *[Path(entry) for entry in sys.path if entry]]:
			for candidate in (folder / f"{name}.py", folder / name / "__init__.py"):
				if candidate.is_file():
					found = candidate
					break
			if found is not None:
				break
		_module_files[key] = found
	return _module_files[key]


# ================
# code_files
#
# The files a step's behaviour is written in: its defining module, this
# runner, and every local module those can import, transitively.
# ================
def code_files(seed: Path) -> list[Path]:
	found: dict[str, Path] = {}
	queue = [seed, Path(__file__)]
	while queue:
		current = queue.pop()
		key = str(current)
		if key in found or not current.is_file():
			continue
		found[key] = current
		for name in imported_names(current):
			candidate = find_module_file(name, current.parent)
			if candidate is not None and str(candidate) not in found:
				queue.append(candidate)
	return [found[key] for key in sorted(found)]


# ================
# code_key
#
# A digest of the step's code files and of the SRO_* flags they name:
# the step reruns when its code changes or when a flag it names moves.
# ================
def code_key(seeds: list[Path], environ) -> str:
	digest = hashlib.sha256()
	flags: dict[str, str] = {}
	for path in seeds:
		try:
			content = path.read_bytes()
		except OSError:
			continue
		digest.update(str(path).encode("utf-8") + b"\0")
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
# record_reads
#
# Run one step with the read log on; returns (result, the paths read).
# ================
def record_reads(run):
	global _hook_installed
	if not _hook_installed:
		sys.addaudithook(audit_hook)
		_hook_installed = True
	reads: set[str] = set()
	_recording["reads"] = reads
	try:
		return run(), reads
	finally:
		_recording["reads"] = None


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
	def __init__(self, variant_root: Path, roots: list[Path], environ=None):
		self.variant = Path(variant_root)
		self.roots = [Path(root).resolve() for root in roots]
		self.environ = os.environ if environ is None else environ
		self.steps: list[dict] = []
		self.journal: list[dict] | None = None
		self.arguments: dict | None = None
		self.fresh = True
		self.first = 0
		self.state = EMPTY_STATE
		self.records: list[dict] = []
		self._root_fingerprints: dict[str, str] = {}
		self._code_keys: dict[str, str] = {}

	# ================
	# register
	#
	# code_from names the function whose module the step's code lives
	# in, when the callable itself is a closure in the builder. roots
	# names source roots the step reads without opening files under
	# them (the base step links and walks the native tree): they are
	# fingerprinted into its key on every build, audited or not.
	# ================
	def register(self, name: str, run, code_from=None, roots=()):
		self.steps.append({
			"name": name, "run": run, "code_from": code_from or run,
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
	# One source root's fingerprint, computed at most once a build. The
	# stage 1 import is taken by content, as the stamp takes it.
	# ================
	def root_fingerprint(self, root: Path) -> str:
		key = str(root)
		if key not in self._root_fingerprints:
			imported = (self.arguments or {}).get("imported")
			by_content = imported is not None and root == Path(imported).resolve()
			self._root_fingerprints[key] = variant_stamp.tree_fingerprint(root, by_content)
		return self._root_fingerprints[key]

	# ================
	# step_code_key
	# ================
	def step_code_key(self, step: dict) -> str:
		name = step["name"]
		if name not in self._code_keys:
			seed = module_file(step["code_from"].__module__)
			files = code_files(seed) if seed else [Path(__file__)]
			self._code_keys[name] = code_key(files, self.environ)
		return self._code_keys[name]

	# ================
	# reads_key
	#
	# What a step's recorded reads cost now: each source root it read
	# under, refingerprinted, and each loose file outside them, taken
	# again. A step with no record reads as a sentinel nothing matches.
	# ================
	def reads_key(self, recorded: dict | None) -> str:
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
	# A run's raw read paths as the journal holds them: the source roots
	# touched, and the loose files (the pins beside the code, a named
	# source file), each with the key it must keep.
	# ================
	def classify_reads(self, reads: set[str]) -> dict:
		own = str(self.variant.resolve())
		stdlib = os.path.dirname(os.__file__)
		roots: set[str] = set()
		files: dict[str, str] = {}
		for raw in reads:
			try:
				path = Path(raw).resolve()
			except (OSError, RuntimeError):
				continue
			text = str(path)
			if text.startswith(own) or text.startswith(stdlib):
				continue
			owner = next((root for root in self.roots if path == root or root in path.parents), None)
			if owner is not None:
				roots.add(str(owner))
			elif path.is_file():
				files[text] = variant_stamp.file_key(path, True)
		return {"roots": sorted(roots), "files": files}

	# ================
	# key_of
	#
	# The key a step must match to skip, from its recorded reads and
	# the chain state in front of it.
	# ================
	def key_of(self, step: dict, recorded_reads: dict | None, state: str) -> str:
		if recorded_reads is not None and step["roots"]:
			recorded_reads = {
				"roots": sorted(set(recorded_reads.get("roots", [])) | set(step["roots"])),
				"files": recorded_reads.get("files", {}),
			}
		parts = [step["name"], self.step_code_key(step), self.reads_key(recorded_reads), state]
		return hashlib.sha256("\0".join(parts).encode("utf-8")).hexdigest()

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
		for index, (step, entry) in enumerate(zip(self.steps, self.journal)):
			if entry.get("key") != self.key_of(step, entry.get("reads"), state):
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
		step = self.steps[index]
		entry = None if self.fresh else (self.journal or [])[index]
		if entry is not None and index < self.first:
			self.state = chain(self.state, entry["effect"])
			self.records.append(entry)
			return decode_result(entry["result"], self.variant)
		if entry is not None and entry.get("key") == self.key_of(step, entry.get("reads"), self.state):
			for relative, sha in entry["effect"]:
				target = self.variant / relative
				if sha is None:
					target.unlink(missing_ok=True)
				else:
					lay_blob(self.variant, sha, target)
			self.state = chain(self.state, entry["effect"])
			self.records.append(entry)
			return decode_result(entry["result"], self.variant)
		before = snapshot(self.variant)
		value, reads = record_reads(step["run"])
		effect = effect_between(self.variant, before, snapshot(self.variant))
		classified = self.classify_reads(reads)
		try:
			encoded = encode_result(value, self.variant)
			key = self.key_of(step, classified, self.state)
		except Unrecordable:
			encoded, key = None, None
		self.state = chain(self.state, effect)
		self.records.append({
			"name": step["name"], "key": key, "reads": classified, "effect": effect, "result": encoded,
		})
		return value

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
