"""
===========================================================================

variant_stamp.py - an unchanged variant is not rebuilt

build_alexandria_variant.py reran every importer from extraction on every
release, about six minutes, even when nothing it reads had changed since
the last build of the same tree. The variant is a derivation: the same
code, flags and source files give the same tree. This module records what
one successful build read and lets the next one skip when all of it still
matches.

What the stamp covers:

	code       every file the builder can run (codeStamp.mjs codeClosure,
	           the owner the asset build's stamps use), plus the JSON files
	           beside those modules that a module names (the archive pins)
	env        every SRO_* variable except the ones that only name the asset
	           build's outputs or its parallelism (IGNORED_ENV); flags are
	           read under names that come from tables, so all are taken.
	           Only a digest of each value is stored: a shard secret set in
	           the same shell never reaches the stamp file
	arguments  the native, imported, VSRO and variant roots
	sources    every file under each source root: the native extraction,
	           the stage 1 import, the VSRO client, every importer source
	           variable that names an existing path (SOURCE_VARIABLE), and
	           the defaults their owners fall back to (the v1.696 inputs,
	           the Arabia population)

The variant tree itself must also still be what the last build left
(variant_settle.matches_ledger). A tree edited or half-deleted since then is
rebuilt.

A build clears the stamp before it writes anything and stamps only after it
succeeds, with the inputs it read before it started; if those changed during
the build the stamp is not written, so the next run rebuilds.

The large source roots are fingerprinted by (size, mtime), not hashed: the
native extraction and the pinned archives are never rewritten in place, so
a changed stat is at worst a needless rebuild. The stage 1 import is hashed
by content: the release train reruns stage 1 before every variant build
(ops variant_build.py), which rewrites the same bytes with fresh times,
and it is small (about 2,000 files, 150 MiB, about a second). So is any
single source file up to SMALL_FILE (the Arabia population).

===========================================================================
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
from pathlib import Path

import variant_settle
from prepare_client_resources import write_atomically
from sro_paths import REPO_ROOT

STAMP_FORMAT = "sro-variant-stamp"
STAMP_VERSION = 1
STAMP_NAME = "variant-stamp.json"
BUILDER = Path(__file__).with_name("build_alexandria_variant.py")
CODE_STAMP = REPO_ROOT / "scripts" / "build" / "shared" / "codeStamp.mjs"
# SRO_* variables the variant never reads: they name the asset build's own
# outputs (sro_paths resolves them, the importers never open them) or set
# how many workers run, not what they produce.
IGNORED_ENV = frozenset(("SRO_BUILD_JOBS", "SRO_GENERATED_ROOT", "SRO_SERVER_GAME_DATA_ROOT", "SRO_CHECK_FORCE"))
ENV_PREFIX = "SRO_"
# The importers' source variables, by their naming convention:
# SRO_VSRO_ROOT, SRO_V1696_ROOT, SRO_AVATARS_ZIP, SRO_*_DATABASE,
# SRO_ARABIA_NESTS. A machine variable such as SRO_HOME names the whole
# project folder and is never walked.
SOURCE_VARIABLE = re.compile(r"SRO_\w+_(?:ROOT|ZIP|DATABASE|NESTS)")
# The roots that name the asset build's trees, not the variant's sources.
OUTPUT_ROOTS = frozenset(("SRO_GAME_ROOT", "SRO_GENERATED_ROOT", "SRO_SERVER_GAME_DATA_ROOT"))
# A single source file up to this size is hashed by content, not stat.
SMALL_FILE = 64 << 20
# A module's literal naming a JSON file beside it (Path(__file__).with_name).
JSON_LITERAL = re.compile(r"[\"']([\w.-]+\.json)[\"']")
CLOSURE_SCRIPT = (
	"const { pathToFileURL } = await import( 'node:url' );"
	"const [ owner, entry ] = process.argv.slice( 1 );"
	"const { codeClosure } = await import( pathToFileURL( owner ).href );"
	"console.log( JSON.stringify( await codeClosure( entry ) ) );"
)


# ================
# stamp_path
# ================
def stamp_path(variant_root: Path) -> Path:
	return Path(variant_root) / ".state" / STAMP_NAME


# ================
# code_closure
#
# Every file the builder can run, from the one owner of code closures.
# ================
def code_closure() -> list[Path]:
	result = subprocess.run(
		["node", "--input-type=module", "-e", CLOSURE_SCRIPT, str(CODE_STAMP), str(BUILDER)],
		cwd=REPO_ROOT, capture_output=True, text=True, encoding="utf-8", check=False
	)
	if result.returncode != 0:
		raise SystemExit(f"variant stamp: the code closure failed (exit {result.returncode}):\n{result.stderr}")
	return [Path(file) for file in json.loads(result.stdout)]


# ================
# code_digest
#
# sha256 over the closure and the JSON files its modules name beside
# themselves, keyed by repository-relative path.
# ================
def code_digest(closure: list[Path]) -> str:
	files = set(closure)
	for module in closure:
		if module.suffix != ".py":
			continue
		for name in JSON_LITERAL.findall(module.read_text(encoding="utf-8")):
			data = module.with_name(name)
			if data.is_file():
				files.add(data)
	digest = hashlib.sha256()
	for file in sorted(files):
		relative = file.resolve().relative_to(REPO_ROOT.resolve()).as_posix()
		digest.update(relative.encode("utf-8") + b"\0" + file.read_bytes() + b"\0")
	return digest.hexdigest()


# ================
# overlaps
#
# Whether two roots are the same folder or one holds the other.
# ================
def overlaps(a: Path, b: Path) -> bool:
	return a == b or a in b.parents or b in a.parents


# ================
# source_roots
#
# Every file or folder the build may read data from, except the variant
# itself (a variable naming the variant or a folder that holds it is an
# output, not a source).
# ================
def source_roots(native: Path, imported: Path, vsro_root: Path, variant: Path, environ) -> list[Path]:
	# Imported here, not at load: both import build_alexandria_variant, which
	# imports this module.
	import arabia_import
	import skytemple_import
	candidates = [native, imported, vsro_root, skytemple_import.source_root(environ), arabia_import.nests_path(environ)]
	for key, value in environ.items():
		value = value.strip()
		if SOURCE_VARIABLE.fullmatch(key) and key not in OUTPUT_ROOTS and value and Path(value).is_absolute():
			candidates.append(Path(value))
	output = variant.resolve()
	roots = set()
	for candidate in candidates:
		resolved = Path(candidate).resolve()
		if resolved.exists() and not overlaps(resolved, output):
			roots.add(resolved)
	return sorted(roots)


# ================
# file_key
#
# One file's part of a fingerprint: its sha256 when content counts, else
# its size and mtime.
# ================
def file_key(path: Path, content: bool) -> str:
	if content:
		return variant_settle.file_sha256(path)
	stat = path.stat()
	return f"{stat.st_size}\0{stat.st_mtime_ns}"


# ================
# tree_fingerprint
#
# sha256 over (relative path, file key) of a file or of every file under a
# folder. A single file up to SMALL_FILE is always taken by content.
# ================
def tree_fingerprint(root: Path, content: bool = False) -> str:
	digest = hashlib.sha256()
	if root.is_file():
		digest.update(file_key(root, content or root.stat().st_size <= SMALL_FILE).encode())
		return digest.hexdigest()
	rows = []
	for directory, _, names in os.walk(root):
		for name in names:
			absolute = Path(directory, name)
			rows.append(f"{absolute.relative_to(root).as_posix()}\0{file_key(absolute, content)}")
	for row in sorted(rows):
		digest.update(row.encode("utf-8") + b"\n")
	return digest.hexdigest()


# ================
# read_inputs
#
# Everything the stamp covers, as one JSON-able record. The code digest is
# computed unless given (it runs node; a caller that already holds it
# passes it).
# ================
def read_inputs(native: Path, imported: Path, vsro_root: Path, variant: Path, environ=None, code=None) -> dict:
	environ = os.environ if environ is None else environ
	return {
		"code": code_digest(code_closure()) if code is None else code,
		"env": {
			key: hashlib.sha256(environ[key].encode("utf-8")).hexdigest()
			for key in sorted(environ) if key.startswith(ENV_PREFIX) and key not in IGNORED_ENV
		},
		"arguments": {
			"native": str(Path(native).resolve()),
			"imported": str(Path(imported).resolve()),
			"vsro": str(Path(vsro_root).resolve()),
			"variant": str(Path(variant).resolve())
		},
		"sources": {
			str(root): tree_fingerprint(root, root == Path(imported).resolve())
			for root in source_roots(native, imported, vsro_root, variant, environ)
		}
	}


# ================
# differences
#
# The named parts of two input records that differ, for the log.
# ================
def differences(before: dict, after: dict) -> list[str]:
	changed = []
	for part in ("code", "arguments"):
		if before.get(part) != after.get(part):
			changed.append(part)
	for part in ("env", "sources"):
		old, new = before.get(part) or {}, after.get(part) or {}
		changed.extend(f"{part}:{key}" for key in sorted(set(old) | set(new)) if old.get(key) != new.get(key))
	return changed


# ================
# load_stamp
# ================
def load_stamp(variant_root: Path) -> dict | None:
	try:
		document = json.loads(stamp_path(variant_root).read_text(encoding="utf-8"))
	except (OSError, ValueError):
		return None
	if document.get("format") != STAMP_FORMAT or document.get("version") != STAMP_VERSION:
		return None
	return document


# ================
# stale_parts
#
# Why the variant must be rebuilt for these inputs: an empty list when
# the stamp matches and the tree is still what that build left.
# ================
def stale_parts(variant_root: Path, inputs: dict, required_files=()) -> list[str]:
	stamp = load_stamp(variant_root)
	if stamp is None:
		return ["no stamp"]
	changed = differences(stamp.get("inputs") or {}, inputs)
	if changed:
		return changed
	missing = [name for name in required_files if not (Path(variant_root) / name).exists()]
	if missing:
		return [f"missing {name}" for name in missing]
	if not variant_settle.matches_ledger(variant_root):
		return ["the variant tree changed since it was built"]
	return []


# ================
# clear_stamp
#
# Called before a build writes anything, so a failed build leaves no stamp
# vouching for a half-written tree.
# ================
def clear_stamp(variant_root: Path) -> None:
	stamp_path(variant_root).unlink(missing_ok=True)


# ================
# write_stamp
# ================
def write_stamp(variant_root: Path, inputs: dict) -> None:
	path = stamp_path(variant_root)
	path.parent.mkdir(parents=True, exist_ok=True)
	document = {"format": STAMP_FORMAT, "version": STAMP_VERSION, "inputs": inputs}
	write_atomically(path, (json.dumps(document, indent=1, sort_keys=True) + "\n").encode("utf-8"))
