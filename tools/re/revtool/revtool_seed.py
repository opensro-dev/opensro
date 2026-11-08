"""
===========================================================================
revtool_seed.py - the contributor seed: BN's index + revtool's ledger

Part of revtool, shipped with it (#2830). Maintainers hold the
analysis in Binary Ninja databases that embed the retail executable
and can never leave this machine; contributors hold the retail
binary and rizin. The seed is the bridge: one xz file per target
holding only conclusions - every function's address and current
(overlay-applied) name, plus the target's JSONL evidence ledger
verbatim. No executable bytes, no decompiler text: the payload is
JSON, and `seed verify` proves no 16-byte run of the binary's
`.text` section appears in it before anyone ships it. (Names the
binary itself carries as data - its import table - are conclusions
and may of course appear; machine code may not.)

  revtool.py --db revtool.db seed build --target client
  revtool.py seed verify .state/revtool/seed/revtool-seed-client.xz
  revtool.py --binary SRO_Client.exe seed apply \
      .state/revtool/seed/revtool-seed-client.xz --rzdb SRO_Client.rzdb

`apply` needs rzpipe (the pinned venv from
docs/native/REVERSE_ENGINEERING.md): it hand-defines every seed
function in rizin (`af+` with the seed name, one `afb+` block
reaching to the next function's start), saves the project (`Ps`)
and samples the result against the seed before it reports success.
Definitions are starts, names and extents - no analysis: a
contributor who wants blocks and xrefs runs rizin's analysis on
top. (Analysing each function with `af` instead was measured at
~5 ms per function and slowing; the whole client seed applies in
about a minute by hand.) `seed-manifest.json` beside this module
pins the released assets' hashes; refreshes are new release
assets, never git history.
===========================================================================
"""

import hashlib
import json
import lzma
import os
import sqlite3
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from pe_image import PEImage
from revtool_paths import DATA_DIR, DEFAULT_BINARY

SEED_FORMAT = "sro-revtool-seed"
SEED_VERSION = 1
SEED_SIZE_LIMIT = 5_000_000
TEXT_RUN_BYTES = 16
# One rzpipe cmd() call drops commands past a few KB of text (seen
# live: a 400-command rename batch applied only its first ~150), so
# batches are capped by bytes as well as by count.
APPLY_CHUNK_COMMANDS = 200
APPLY_CHUNK_BYTES = 4096
MANIFEST_NAME = "seed-manifest.json"


class SeedError(Exception):
	"""A seed input or payload is not what the format promises."""


# =====================================================================
# payload build
# =====================================================================

def _effective_functions(con):
	"""(va, name) per function, overlay renames applied, by address."""
	renames = {}
	for orig, new in con.execute(
			"SELECT orig_name, new_name FROM globals "
			"WHERE kind = 'func' OR kind IS NULL"):
		renames[orig] = new
	rows = []
	for addr, name in con.execute(
			"SELECT addr, name FROM functions ORDER BY addr"):
		rows.append([int(addr, 16), renames.get(name, name)])
	return rows


def _ledger_lines(path):
	with open(path, encoding="utf-8") as fh:
		return [line.rstrip("\n") for line in fh if line.strip()]


def _ledger_meta(lines):
	for line in lines:
		try:
			row = json.loads(line)
		except json.JSONDecodeError:
			continue
		if isinstance(row, dict) and row.get("_t") == "_meta":
			return row
	return {}


def _sha256_file(path):
	digest = hashlib.sha256()
	with open(path, "rb") as fh:
		for chunk in iter(lambda: fh.read(1 << 20), b""):
			digest.update(chunk)
	return digest.hexdigest()


def resolve_binary_sha(con, ledger_meta, binary_path):
	"""(sha256, source) for the target binary, cross-checked.

The overlay DB's meta and the ledger's `_meta` row both record the
hash of the binary the analysis describes; an explicit binary is
hashed. Every source present must agree - a seed keyed to the
wrong binary is worse than no seed.
	"""
	sources = {}
	try:
		row = con.execute(
			"SELECT value FROM meta WHERE key = 'target_sha256'").fetchone()
	except sqlite3.Error:
		row = None
	if row:
		sources["db meta"] = row[0]
	if ledger_meta.get("target_sha256"):
		sources["ledger meta"] = ledger_meta["target_sha256"]
	if binary_path and os.path.exists(binary_path):
		sources["binary file"] = _sha256_file(binary_path)
	if not sources:
		raise SeedError(
			"no binary hash available: the DB and ledger record none "
			"and no --binary file exists")
	if len(set(sources.values())) != 1:
		detail = ", ".join(f"{k}={v}" for k, v in sorted(sources.items()))
		raise SeedError(f"binary hash sources disagree: {detail}")
	return next(iter(sources.values())), "+".join(sorted(sources))


def build_payload(con, ledger_path, binary_path, target):
	"""The seed payload dict for one target (see module banner)."""
	functions = _effective_functions(con)
	if not functions:
		raise SeedError("the overlay DB holds no function index to seed")
	lines = _ledger_lines(ledger_path) if ledger_path else []
	sha, source = resolve_binary_sha(con, _ledger_meta(lines), binary_path)
	return {
		"format": SEED_FORMAT,
		"version": SEED_VERSION,
		"target": target,
		"binary_sha256": sha,
		"binary_sha_source": source,
		"functions": functions,
		"ledger": lines,
	}


def encode_seed(payload):
	"""Deterministic xz bytes for a payload (no timestamps anywhere)."""
	raw = json.dumps(
		payload, separators=(",", ":"), sort_keys=True,
		ensure_ascii=True).encode("utf-8")
	return lzma.compress(
		raw, format=lzma.FORMAT_XZ, preset=9 | lzma.PRESET_EXTREME)


def load_seed(path):
	"""Parse a seed file back to its payload, format-checked."""
	with open(path, "rb") as fh:
		blob = fh.read()
	try:
		raw = lzma.decompress(blob, format=lzma.FORMAT_XZ)
		payload = json.loads(raw.decode("utf-8"))
	except (lzma.LZMAError, UnicodeDecodeError, json.JSONDecodeError) as exc:
		raise SeedError(f"{path} is not a readable seed: {exc}") from exc
	if payload.get("format") != SEED_FORMAT:
		raise SeedError(f"{path}: format {payload.get('format')!r}, want {SEED_FORMAT!r}")
	if payload.get("version") != SEED_VERSION:
		raise SeedError(f"{path}: seed version {payload.get('version')!r}, want {SEED_VERSION}")
	return payload


# =====================================================================
# the no-executable-bytes scan
# =====================================================================

def _printable_runs(data, minimum):
	"""Maximal printable-ASCII runs of at least `minimum` bytes."""
	runs = []
	start = None
	for at, byte in enumerate(data):
		if 0x20 <= byte <= 0x7E:
			if start is None:
				start = at
		else:
			if start is not None and at - start >= minimum:
				runs.append(data[start:at])
			start = None
	if start is not None and len(data) - start >= minimum:
		runs.append(data[start:])
	return runs


def find_text_run(payload, text_bytes, minimum=TEXT_RUN_BYTES):
	"""A `minimum`-byte run of the binary's `.text` inside the payload.

The payload is JSON text, so any text-section run inside it is
printable; scanning the section's few maximal printable runs and
looking each up in the payload is exact and needs no window table.
	"""
	for run in _printable_runs(text_bytes, minimum):
		at = payload.find(run)
		if at >= 0:
			return run, at
		for off in range(0, len(run) - minimum + 1):
			window = run[off:off + minimum]
			at = payload.find(window)
			if at >= 0:
				return window, at
	return None


def text_section_bytes(binary_path):
	"""The binary's `.text` raw bytes (the scan's scope, #2830)."""
	image = PEImage(binary_path)
	for section in image.sections:
		if section.name == ".text":
			with open(binary_path, "rb") as fh:
				fh.seek(section.raw_offset)
				return fh.read(section.raw_size)
	raise SeedError(f"{binary_path}: no .text section to scan against")


def scan_seed(seed_path, binary_path):
	"""Text-run scan of the shipped bytes and of the payload."""
	with open(seed_path, "rb") as fh:
		blob = fh.read()
	text = text_section_bytes(binary_path)
	payload = json.dumps(
		load_seed(seed_path), separators=(",", ":"), sort_keys=True,
		ensure_ascii=True).encode("utf-8")
	for label, data in (("payload", payload), ("seed file", blob)):
		hit = find_text_run(data, text)
		if hit:
			run, at = hit
			raise SeedError(
				f"a {TEXT_RUN_BYTES}-byte run of the binary's .text appears "
				f"in the seed {label} at offset {at}: {run!r}")


# =====================================================================
# rizin apply
# =====================================================================

def open_session(binary):
	"""One rzpipe connection, no auto-analysis (cf. revtool_rizin)."""
	try:
		import rzpipe
	except ImportError as exc:
		raise SeedError(
			"rzpipe is not importable; use the pinned venv "
			"(docs/native/REVERSE_ENGINEERING.md)") from exc
	return rzpipe.open(binary)


def _batched(pipe, commands):
	batch, size = [], 0
	for command in commands:
		if batch and (len(batch) >= APPLY_CHUNK_COMMANDS
					  or size + len(command) + 1 > APPLY_CHUNK_BYTES):
			pipe.cmd("\n".join(batch))
			batch, size = [], 0
		batch.append(command)
		size += len(command) + 1
	if batch:
		pipe.cmd("\n".join(batch))


def _seed_commands(functions):
	"""af+/afb+ pairs for the seed rows, in address order.

Each function gets one block reaching to the next function's
start (the last reuses the previous extent, 16 at the limit):
starts and names are the seed's facts; extents are the honest
bound those facts imply. A name rizin's command line cannot take
(whitespace, a leading digit) defines under the sub_<hex>
placeholder and is counted by the caller's name check.
	"""
	rows = sorted(functions)
	commands = []
	previous_gap = 16
	for at, (va, name) in enumerate(rows):
		if at + 1 < len(rows):
			previous_gap = rows[at + 1][0] - va
		if not name or name[0].isdigit() or any(c.isspace() for c in name):
			name = f"sub_{va:x}"
		commands.append(f"af+ {name} @ {va:#x}")
		commands.append(f"afb+ {va:#x} {va:#x} {previous_gap:#x}")
	return commands


def apply_seed(payload, binary_path, rzdb_path, limit=0, opener=open_session):
	"""Define the seed's functions in rizin; save the project.

Returns (defined, named, sampled_ok, sampled) counts, read back
from aflj after the definitions land: what rizin refused (an
address inside a neighbour's block) is reported, never assumed.
	The project is saved to a sibling temp file and os.replace()d
	into place under an exclusive lock file, so two applies at one
	.rzdb can never interleave writes into a corrupt project (two
	concurrent applies raced exactly that way before the lock).
	"""
	functions = payload["functions"]
	if limit:
		functions = functions[:limit]
	lock_path = rzdb_path + ".lock"
	try:
		lock_fd = os.open(lock_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY)
	except FileExistsError as exc:
		raise SeedError(
			f"{lock_path} exists - another apply holds {rzdb_path}") from exc
	tmp_path = f"{rzdb_path}.tmp-{os.getpid()}"
	try:
		os.write(lock_fd, f"{os.getpid()}\n".encode())
		os.close(lock_fd)
		lock_fd = None
		pipe = opener(binary_path)
		try:
			pipe.cmd("e scr.color=false")
			_batched(pipe, _seed_commands(functions))
			rows = pipe.cmdj("aflj") or []
			present = {row["offset"]: row.get("name", "") for row in rows}
			defined = [va for va, _ in functions if va in present]
			named = sum(1 for va, name in functions if present.get(va) == name)
			labelled = [(va, name) for va, name in functions
						if va in present and not name.startswith("sub_")]
			sample = labelled[::max(1, len(labelled) // 20)][:20] if labelled else []
			sampled_ok = sum(1 for va, name in sample if present.get(va) == name)
			pipe.cmd(f'Ps "{tmp_path}"')
		finally:
			try:
				pipe.cmd("q!")
			except Exception:
				pass
		if not os.path.exists(tmp_path):
			raise SeedError(f"rizin did not write {tmp_path}")
		os.replace(tmp_path, rzdb_path)
		return len(defined), named, sampled_ok, len(sample)
	finally:
		if lock_fd is not None:
			os.close(lock_fd)
		if os.path.exists(tmp_path):
			os.remove(tmp_path)
		try:
			os.remove(lock_path)
		except FileNotFoundError:
			pass


# =====================================================================
# commands
# =====================================================================

def _default_ledger(db_path):
	stem, _ = os.path.splitext(db_path)
	return stem + "_symbols.jsonl"


def _default_out(target):
	return os.path.join(DATA_DIR, "seed", f"revtool-seed-{target}.xz")


def cmd_seed_build(args, con):
	ledger = args.ledger or _default_ledger(args.db)
	if not os.path.exists(ledger):
		raise SystemExit(f"error: ledger not found: {ledger}")
	binary = args.seed_binary or args.binary
	try:
		payload = build_payload(con, ledger, binary, args.target)
	except SeedError as exc:
		raise SystemExit(f"error: {exc}") from exc
	blob = encode_seed(payload)
	if len(blob) > SEED_SIZE_LIMIT:
		raise SystemExit(
			f"error: seed is {len(blob)} bytes, over the {SEED_SIZE_LIMIT} cap")
	out = args.out or _default_out(args.target)
	os.makedirs(os.path.dirname(out), exist_ok=True)
	with open(out, "wb") as fh:
		fh.write(blob)
	scanned = "not scanned (no --binary file)"
	if binary and os.path.exists(binary):
		try:
			scan_seed(out, binary)
		except SeedError as exc:
			raise SystemExit(f"error: {exc}") from exc
		scanned = "text-run scan clean (.text)"
	print(f"seed {out}")
	print(f"  target {payload['target']}  binary {payload['binary_sha256']} "
		  f"({payload['binary_sha_source']})")
	print(f"  functions {len(payload['functions'])}  ledger lines {len(payload['ledger'])}")
	print(f"  bytes {len(blob)} of {SEED_SIZE_LIMIT} cap  sha256 {hashlib.sha256(blob).hexdigest()}")
	print(f"  {scanned}")
	return 0


def cmd_seed_verify(args, con):
	try:
		payload = load_seed(args.seed)
	except SeedError as exc:
		raise SystemExit(f"error: {exc}") from exc
	with open(args.seed, "rb") as fh:
		blob = fh.read()
	print(f"seed {args.seed}")
	print(f"  format {payload['format']} v{payload['version']}  target {payload['target']}")
	print(f"  binary {payload['binary_sha256']} ({payload['binary_sha_source']})")
	print(f"  functions {len(payload['functions'])}  ledger lines {len(payload['ledger'])}")
	print(f"  bytes {len(blob)} of {SEED_SIZE_LIMIT} cap  sha256 {hashlib.sha256(blob).hexdigest()}")
	if len(blob) > SEED_SIZE_LIMIT:
		raise SystemExit("error: seed is over the size cap")
	manifest = args.manifest
	if manifest is None and os.path.exists(os.path.join(os.path.dirname(os.path.abspath(__file__)), MANIFEST_NAME)):
		manifest = os.path.join(os.path.dirname(os.path.abspath(__file__)), MANIFEST_NAME)
	if manifest:
		with open(manifest, encoding="utf-8") as fh:
			pins = {a["file"]: a for a in json.load(fh)["assets"]}
		pin = pins.get(os.path.basename(args.seed))
		if pin is None:
			print(f"  manifest: no pin for {os.path.basename(args.seed)} (unreleased seed)")
		elif pin["sha256"] != hashlib.sha256(blob).hexdigest() or pin["bytes"] != len(blob):
			raise SystemExit("error: seed bytes do not match the manifest pin")
		else:
			print("  manifest: pin matches")
	binary = args.seed_binary or args.binary
	if binary:
		if not os.path.exists(binary):
			raise SystemExit(f"error: binary not found: {binary}")
		digest = _sha256_file(binary)
		if digest != payload["binary_sha256"]:
			raise SystemExit(
				f"error: binary sha256 {digest} != seed {payload['binary_sha256']}")
		try:
			scan_seed(args.seed, binary)
		except SeedError as exc:
			raise SystemExit(f"error: {exc}") from exc
		print("  binary: hash matches, text-run scan clean (.text)")
	return 0


def cmd_seed_apply(args, con):
	try:
		payload = load_seed(args.seed)
	except SeedError as exc:
		raise SystemExit(f"error: {exc}") from exc
	binary = args.seed_binary or args.binary or DEFAULT_BINARY
	if not os.path.exists(binary):
		raise SystemExit(f"error: binary not found: {binary}")
	digest = _sha256_file(binary)
	if digest != payload["binary_sha256"]:
		raise SystemExit(
			f"error: binary sha256 {digest} != seed {payload['binary_sha256']}; "
			"the seed only describes its own binary")
	rzdb = args.rzdb or os.path.splitext(args.seed)[0] + ".rzdb"
	if os.path.exists(rzdb) and not args.force:
		raise SystemExit(f"error: {rzdb} exists; pass --force to replace it")
	started = time.monotonic()
	try:
		defined, named, sampled_ok, sampled = apply_seed(
			payload, binary, rzdb, limit=args.limit)
	except SeedError as exc:
		raise SystemExit(f"error: {exc}") from exc
	elapsed = time.monotonic() - started
	total = min(len(payload["functions"]), args.limit) if args.limit else len(payload["functions"])
	print(f"applied {os.path.basename(args.seed)} -> {rzdb} in {elapsed:.1f}s")
	merged = f" (rizin refused {total - defined} inside neighbours)" if defined < total else ""
	print(f"  functions defined {defined} of {total} seed rows{merged}")
	print(f"  seed names in place {named}")
	print(f"  sampled labelled names {sampled_ok} of {sampled} match")
	if sampled and sampled_ok != sampled:
		raise SystemExit("error: sampled names do not match the seed")
	return 0
