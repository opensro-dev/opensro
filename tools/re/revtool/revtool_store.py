"""
===========================================================================
revtool_store.py - the overlay schema and the SQLite store (meta, history, artifacts)

Part of revtool, the non-destructive symbol overlay for the SRO_Client
pseudocode dump. revtool.py is the entry point; this module owns one
command group or layer and is imported by it (and by revtool_rizin.py
through revtool's re-exports). The dump is an immutable Binary Ninja
hypothesis snapshot: original PE bytes and exact proofs outrank it,
and nothing here edits it.
===========================================================================
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import getpass
import hashlib
import json
import re
import sqlite3
import uuid
from datetime import datetime, timezone

from revtool_paths import REVTOOL_SCHEMA_VERSION, REVTOOL_VERSION
from revtool_syntax import STATIC_WEAK_BASES, TIERS, TIER_RANK, valid_basis

SCHEMA = """
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

-- Hash-bound inputs used to materialize this local database. The database is a
-- disposable projection; these rows make it impossible to mistake a same-size
-- analyzer export for the snapshot that was actually indexed.
CREATE TABLE IF NOT EXISTS artifact_provenance (
    name          TEXT PRIMARY KEY,
    path          TEXT NOT NULL,
    sha256        TEXT NOT NULL,
    size          INTEGER NOT NULL,
    record_count  INTEGER,
    producer      TEXT,
    version       TEXT,
    settings_json TEXT,
    recorded_at   TEXT NOT NULL
);

-- Last collaboration ledger imported/exported by this projection.
CREATE TABLE IF NOT EXISTS sync_state (
    path        TEXT PRIMARY KEY,
    sha256      TEXT NOT NULL,
    imported_at TEXT NOT NULL
);

-- Conflicts are facts. Import never resolves a differing same-key claim by
-- timestamp; it records the conflict and requires an explicit human choice.
CREATE TABLE IF NOT EXISTS sync_conflicts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    table_name  TEXT NOT NULL,
    claim_key   TEXT NOT NULL,
    local_json  TEXT NOT NULL,
    remote_json TEXT NOT NULL,
    detected_at TEXT NOT NULL,
    resolved    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(table_name, claim_key, local_json, remote_json)
);

-- Function index, rebuilt by `index`.
CREATE TABLE IF NOT EXISTS functions (
    addr        TEXT PRIMARY KEY,   -- 8 hex, e.g. 008b3180
    name        TEXT NOT NULL,      -- original symbol, e.g. sub_8b3180
    signature   TEXT NOT NULL,
    start_byte  INTEGER NOT NULL,
    end_byte    INTEGER NOT NULL,
    start_line  INTEGER NOT NULL,
    end_line    INTEGER NOT NULL
);

-- Global renames (functions + data symbols). Scope: whole dump.
CREATE TABLE IF NOT EXISTS globals (
    orig_name TEXT PRIMARY KEY,
    new_name  TEXT NOT NULL,
    kind      TEXT,                 -- func | data | ...
    note      TEXT
);

-- Local renames. Scope: a single function (func_addr).
CREATE TABLE IF NOT EXISTS locals (
    func_addr TEXT NOT NULL,
    orig_name TEXT NOT NULL,
    new_name  TEXT NOT NULL,
    vtype     TEXT,
    note      TEXT,
    PRIMARY KEY (func_addr, orig_name)
);

-- Struct/union types.
CREATE TABLE IF NOT EXISTS types (
    name TEXT PRIMARY KEY,
    note TEXT
);

-- Fields of a type. Scope: the type (offset is the key within the type).
CREATE TABLE IF NOT EXISTS fields (
    type_name TEXT NOT NULL,
    offset    INTEGER NOT NULL,
    new_name  TEXT NOT NULL,
    ftype     TEXT,
    note      TEXT,
    PRIMARY KEY (type_name, offset)
);

-- var -> type bindings. Scope: a single function.
CREATE TABLE IF NOT EXISTS bindings (
    func_addr TEXT NOT NULL,
    var_name  TEXT NOT NULL,        -- ORIGINAL var token (e.g. arg3)
    type_name TEXT NOT NULL,
    PRIMARY KEY (func_addr, var_name)
);

-- Free notes attached to an address (line-level) or a symbol.
CREATE TABLE IF NOT EXISTS notes (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    target TEXT NOT NULL,           -- "008b37ac" | "sub_8b3180" | "sub_8b3180@008b37ac"
    body   TEXT NOT NULL
);
-- symbol_is_annotated() is called once per investigated/caller symbol by
-- coverage/status/review (tens of thousands of times on a mature DB); without this
-- index each call full-scanned `notes`, which turned `review --debt` into a 2+ minute
-- hang. Keyed on target so the exact-match and the `target>='sym@' AND target<'symA'`
-- prefix range (see symbol_is_annotated) are both index range scans.
CREATE INDEX IF NOT EXISTS notes_target ON notes(target);
-- globals.orig_name is the PK (indexed); new_name is not, so the
-- `orig_name=? OR new_name=?` annotation test could not index the second arm.
CREATE INDEX IF NOT EXISTS globals_new_name ON globals(new_name);

-- Append-only audit log: every change to a claim is recorded here so a prior
-- assertion is NEVER silently lost (the "we were wrong" safety net).
CREATE TABLE IF NOT EXISTS history (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT UNIQUE,
    ts     TEXT NOT NULL,
    author TEXT,
    kind   TEXT NOT NULL,           -- global | local | field | bind | note
    ckey   TEXT NOT NULL,           -- canonical claim key, e.g. global:sub_8b3180
    action TEXT NOT NULL,           -- set | rename | tier | dispute | resolve | delete
    old    TEXT,
    new    TEXT,
    reason TEXT,
    target_sha256 TEXT,
    dump_sha256 TEXT,
    tool_version TEXT
);
CREATE INDEX IF NOT EXISTS history_ckey ON history(ckey);

-- Testable factual ASSERTIONS about runtime state/behaviour (NOT names). This is the
-- piece that was missing when we asserted "data_f0673f == 0": a falsifiable proposition
-- with an explicit basis, the method that produced it, that method's blind spots, and a
-- status that a dynamic observation can flip to 'refuted'.
CREATE TABLE IF NOT EXISTS assertions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    subject     TEXT NOT NULL,          -- data_f0673f | sub_8b3180@008b37b1 | MapRenderer+0x26e1
    claim       TEXT NOT NULL,          -- human statement, e.g. "statically 0; no writer"
    predicate   TEXT,                   -- machine-checkable: zero|nonzero|eq:N|range:LO:HI|const|any
    basis       TEXT NOT NULL DEFAULT 'inferred',  -- observed|dynamic|inferred|absence
    method      TEXT,                   -- how it was derived (e.g. "rizin /x absolute-addr")
    blindspots  TEXT,                   -- what `method` cannot see (e.g. "base+disp, dynamic")
    tier        TEXT DEFAULT 'guess',
    status      TEXT DEFAULT 'open',    -- open|confirmed|refuted
    expected    TEXT,                   -- expected value (text)
    observed    TEXT,                   -- last observed value (text)
    source      TEXT,                   -- where observed (trace log:line)
    author      TEXT,
    evidence    TEXT,
    updated_at  TEXT,
    disputed    INTEGER DEFAULT 0,
    dispute_reason TEXT
);
CREATE INDEX IF NOT EXISTS assertions_subject ON assertions(subject);

-- Raw runtime OBSERVATIONS (ground truth from injected traces). Append-only facts; an
-- observation can never be "wrong" - it is what the live process did.
CREATE TABLE IF NOT EXISTS observations (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    subject TEXT NOT NULL,
    value   TEXT NOT NULL,
    source  TEXT,
    author  TEXT,
    ts      TEXT
);
CREATE INDEX IF NOT EXISTS observations_subject ON observations(subject);

-- Investigation ledger: every symbol an engineer `view`s or `xref`s, plus the
-- callers an `xref` reveals. This is what makes RE collaborative instead of a
-- write-once-forget loop: anything investigated but never labeled shows up as
-- "investigation debt" (`review --debt`, `status`) and, in strict mode, blocks
-- the next investigation until it is annotated (even a guess). Local workflow
-- state - intentionally NOT synced; the LABELS it forces (notes/renames) sync.
CREATE TABLE IF NOT EXISTS access_log (
    symbol   TEXT NOT NULL,    -- original dump token, e.g. sub_4df160 / data_f0c914
    kind     TEXT NOT NULL,    -- viewed | xref | caller
    author   TEXT,
    first_ts TEXT,
    last_ts  TEXT,
    count    INTEGER DEFAULT 0,
    PRIMARY KEY (symbol, kind)
);
"""

# Provenance columns added to every claim table by migrate().
PROVENANCE_COLS = [
	("tier", "TEXT DEFAULT 'guess'"),
	("author", "TEXT"),
	("evidence", "TEXT"),
	("updated_at", "TEXT"),
	("disputed", "INTEGER DEFAULT 0"),
	("dispute_reason", "TEXT"),
	# Evidence basis + the method that produced it + that method's blind spots.
	# `basis` is what stops an absence-search from masquerading as a verified fact.
	("basis", "TEXT"),
	("method", "TEXT"),
	("blindspots", "TEXT"),
]
CLAIM_TABLES = ["globals", "locals", "types", "fields", "bindings", "notes"]


def sha256_file(path):
	h = hashlib.sha256()
	with open(path, "rb") as f:
		for block in iter(lambda: f.read(1024 * 1024), b""):
			h.update(block)
	return h.hexdigest()


def stable_json(value):
	return json.dumps(value, sort_keys=True, ensure_ascii=False,
					  separators=(",", ":"))


def meta_get(con, key, default=None):
	row = con.execute("SELECT value FROM meta WHERE key=?", (key,)).fetchone()
	return row[0] if row else default


def meta_set(con, key, value):
	con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES(?,?)",
				(key, str(value)))


def record_artifact(con, name, path, *, record_count=None, producer=None,
					version=None, settings=None):
	path = os.path.abspath(path)
	con.execute(
		"INSERT OR REPLACE INTO artifact_provenance"
		"(name,path,sha256,size,record_count,producer,version,settings_json,recorded_at) "
		"VALUES(?,?,?,?,?,?,?,?,?)",
		(name, path, sha256_file(path), os.path.getsize(path), record_count,
		 producer, version, stable_json(settings) if settings is not None else None,
		 now_iso()))


def enforce_basis_tier(basis, tier):
	"""Reject promoting an absence/inferred claim to a top tier from static work alone.

    This is the guardrail for the data_f0673f miss: "no writer found" (basis=absence)
    must never be recorded as `verified`. Raises SystemExit on violation.
    """
	if not basis:
		return
	valid_basis(basis)
	if basis in STATIC_WEAK_BASES and tier in ("verified", "pipeline"):
		raise SystemExit(
			f"error: a '{basis}' claim cannot be tier '{tier}'.\n"
			f"  Absence-of-evidence and inference have blind spots; cap them at "
			f"'mechanism' until confirmed.\n"
			f"  To carry '{tier}', either cite positive disassembly (--basis observed) "
			f"or confirm at runtime (--basis dynamic, via `observe`/`ingest`).")


def migrate(con):
	"""Upgrade a local projection without treating it as shared authority."""
	for table in CLAIM_TABLES:
		have = {r[1] for r in con.execute(f"PRAGMA table_info({table})")}
		for col, decl in PROVENANCE_COLS:
			if col not in have:
				con.execute(f"ALTER TABLE {table} ADD COLUMN {col} {decl}")
	history_cols = {r[1] for r in con.execute("PRAGMA table_info(history)")}
	for col, decl in (
		("event_id", "TEXT"),
		("target_sha256", "TEXT"),
		("dump_sha256", "TEXT"),
		("tool_version", "TEXT"),
	):
		if col not in history_cols:
			con.execute(f"ALTER TABLE history ADD COLUMN {col} {decl}")
	con.execute("CREATE UNIQUE INDEX IF NOT EXISTS history_event_id ON history(event_id)")
	# Give legacy local events deterministic identities so repeated exports are
	# stable. Their missing artifact lineage remains visibly NULL.
	for row in con.execute(
			"SELECT id,ts,author,kind,ckey,action,old,new,reason FROM history "
			"WHERE event_id IS NULL").fetchall():
		event_id = "legacy-" + hashlib.sha256(stable_json(row).encode("utf-8")).hexdigest()
		con.execute("UPDATE history SET event_id=?,tool_version=? WHERE id=?",
					(event_id, "legacy", row[0]))
	meta_set(con, "schema_version", REVTOOL_SCHEMA_VERSION)
	con.commit()


def open_db(path):
	# Many agents share one DB, so a plain connect() will throw "database is locked"
	# under concurrent writes. A generous busy timeout makes writers queue instead of
	# failing, and WAL lets readers proceed while one writer holds the write lock.
	# A first run on a fresh data dir creates the directory: commands that
	# never touch the overlay (seed verify/apply) must still start.
	parent = os.path.dirname(os.path.abspath(path))
	os.makedirs(parent, exist_ok=True)
	con = sqlite3.connect(path, timeout=30.0)
	con.execute("PRAGMA busy_timeout=30000")
	try:
		con.execute("PRAGMA journal_mode=WAL")
	except sqlite3.OperationalError:
		# WAL needs a real on-disk file; fall back silently for :memory:/tmpfs cases.
		pass
	con.executescript(SCHEMA)
	migrate(con)
	return con


# --------------------------------------------------------------------------- #
# provenance helpers
# --------------------------------------------------------------------------- #
def now_iso():
	return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")


def get_author(args):
	a = getattr(args, "author", None)
	if a:
		return a
	return os.environ.get("REVTOOL_AUTHOR") or getpass.getuser()


def author_is_explicit(args):
	"""True if the author came from --author or $REVTOOL_AUTHOR (not the OS username).

    The per-author exploration budget only isolates agents that carry DISTINCT authors.
    Two concurrent agents both falling back to the same OS username would share one
    budget again - the very bug per-author scoping fixes - so we surface that.
    """
	return bool(getattr(args, "author", None) or os.environ.get("REVTOOL_AUTHOR"))


def valid_tier(t):
	if t not in TIER_RANK:
		raise SystemExit(f"error: tier must be one of {', '.join(TIERS)} (got {t!r})")
	return t


def record_history(con, kind, ckey, action, old, new, author, reason=None):
	con.execute(
		"INSERT INTO history(event_id,ts,author,kind,ckey,action,old,new,reason,"
		"target_sha256,dump_sha256,tool_version) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		(uuid.uuid4().hex, now_iso(), author, kind, ckey, action, old, new, reason,
		 meta_get(con, "target_sha256"), meta_get(con, "dump_sha256"),
		 REVTOOL_VERSION))


# --------------------------------------------------------------------------- #
# Investigation ledger + enforcement (collaborative RE: investigate => label)
# --------------------------------------------------------------------------- #


def _read_jsonl(path):
	if not os.path.exists(path):
		raise SystemExit(f"error: trace log not found: {path}")
	out = []
	with open(path, encoding="utf-8", errors="replace") as f:
		for ln_no, line in enumerate(f, 1):
			line = line.strip()
			if not line or line.startswith("#") or line.startswith("//"):
				continue
			try:
				obj = json.loads(line)
			except json.JSONDecodeError:
				continue
			if isinstance(obj, dict):
				obj.setdefault("_ln", ln_no)
				out.append(obj)
	return out


def _as_int(v):
	"""Parse an address/size that may be hex string ('0x..'/'00..'), int, or decimal str."""
	if v is None:
		return None
	if isinstance(v, int):
		return v
	s = str(v).strip()
	try:
		if s.lower().startswith("0x"):
			return int(s, 16)
		if re.fullmatch(r"[0-9a-fA-F]{8}", s):  # bare 8-hex like the dump uses
			return int(s, 16)
		return int(s, 10)
	except ValueError:
		try:
			return int(s, 16)
		except ValueError:
			return None
