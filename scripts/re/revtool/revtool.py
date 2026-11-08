"""
revtool - a non-destructive symbol-overlay tool for the SRO_Client pseudocode dump.

Read: revtool_README.md
The pseudocode file (SRO_Client_psuedo.txt) is treated as an IMMUTABLE Binary
Ninja hypothesis snapshot. Original PE bytes and exact proofs outrank it.
This tool never edits it. Instead it keeps a separate SQLite "overlay" of names,
types and notes, and composites them on top of the raw dump when you `view` a
function. The pristine code is always one `--raw` away.

Three naming SCOPES are modelled (this is the whole point - you cannot just
"replace all", because the same token means different things in different places):

  global   sub_8b3180 / data_f0673f          -> renamed everywhere in the dump
  local    var_d0 / eax_3 / arg3 (per func)  -> renamed ONLY inside one function
  field    +0x67e6 on a typed pointer        -> a field of a struct TYPE; resolved
                                                wherever a var is bound to that type

Quick start:
  py -3 scripts/re/revtool/revtool.py index                      # one-time: scan the dump, build func index
  py -3 scripts/re/revtool/revtool.py find fog                   # locate functions/signatures by substring
  py -3 scripts/re/revtool/revtool.py view sub_8b3180            # read a function with the overlay applied
  py -3 scripts/re/revtool/revtool.py view sub_8b3180 --raw      # pristine, untouched
  py -3 scripts/re/revtool/revtool.py xref data_f0673f           # every line that mentions a global symbol

  py -3 scripts/re/revtool/revtool.py rename global sub_8b3180 WorldRenderPass
  py -3 scripts/re/revtool/revtool.py rename local sub_8a7d10 var_d0 zenith_r
  py -3 scripts/re/revtool/revtool.py type new MapRenderer
  py -3 scripts/re/revtool/revtool.py field MapRenderer 0x67e6 fogColor --type uint32
  py -3 scripts/re/revtool/revtool.py bind sub_8b1050 arg3 MapRenderer
  py -3 scripts/re/revtool/revtool.py note add sub_8b3180@008b37ac "FOGEND = data_f06743 (else branch)"
  py -3 scripts/re/revtool/revtool.py export sub_8b3180 > ../draft_sub_8b3180.txt
"""

import argparse
import csv
import hashlib
import os
import re
import sqlite3
import struct
import sys
import mmap
import bisect
import json
import getpass
import subprocess
import textwrap
import uuid
from datetime import datetime, timezone

# Confidence ladder (matches AGENT.md RE tiers). Higher index = more sure.
TIERS = ["unknown", "guess", "mechanism", "pipeline", "verified"]
TIER_RANK = {t: i for i, t in enumerate(TIERS)}
# Compact one-letter badge for the inline/legend view.
TIER_BADGE = {
    "unknown": "?",
    "guess": "g",
    "mechanism": "m",
    "pipeline": "p",
    "verified": "V",
}

# Evidence BASIS - HOW a claim was reached. This is the lesson from the
# data_f0673f "statically 0" miss: a flat confidence tier hid the difference
# between "I saw the write" and "I searched and found nothing".
#   observed  positive disassembly/decompiler evidence (the write/read is right here)
#   dynamic   confirmed by a runtime trace / injected observation
#   inferred  derived from other claims or reasoning (no direct evidence)
#   absence   concluded from a search that found NOTHING (absence of evidence)
BASES = ["observed", "dynamic", "inferred", "absence"]
# A claim resting only on absence-of-evidence or inference CANNOT be 'verified' from
# static work: absence searches have blind spots (e.g. absolute-address /x cannot see
# base+disp stores), and inference can be wrong. Only positive 'observed' evidence or a
# 'dynamic' runtime confirmation may carry the top tier.
STATIC_WEAK_BASES = {"absence", "inferred"}
# Assertion lifecycle.
ASSERT_STATUS = ["open", "confirmed", "refuted"]


def valid_basis(b):
    if b not in BASES:
        raise SystemExit(f"error: basis must be one of {', '.join(BASES)} (got {b!r})")
    return b


def eval_predicate(predicate, observed_value):
    """Check a machine-readable predicate against an observed value.

    Returns (matches, detail): matches is True/False, or None when the predicate is
    not machine-checkable (human review needed). Numeric compares use a small tolerance.

    Supported predicates:
      zero | nonzero
      eq:N | ne:N | lt:N | le:N | gt:N | ge:N
      range:LO:HI            (inclusive)
      const                  (human/multi-sample; not auto-checkable from one value)
      any                    (always matches; a pure observation sink)
    """
    if not predicate:
        return None, "no predicate"
    p = predicate.strip().lower()
    if p in ("const", "any"):
        return (True, "non-falsifiable predicate") if p == "any" else (None, "const needs multiple samples")
    try:
        obs = float(observed_value)
    except (TypeError, ValueError):
        return None, f"observed {observed_value!r} is not numeric"
    tol = 1e-3

    def near(a, b):
        return abs(a - b) <= tol + 1e-6 * max(abs(a), abs(b))

    if p == "zero":
        return near(obs, 0.0), f"{obs} {'==' if near(obs, 0.0) else '!='} 0"
    if p == "nonzero":
        return (not near(obs, 0.0)), f"{obs} {'!=' if not near(obs, 0.0) else '=='} 0"
    if ":" in p:
        op, rest = p.split(":", 1)
        if op == "range":
            lo_s, _, hi_s = rest.partition(":")
            lo, hi = float(lo_s), float(hi_s)
            ok = (lo - tol) <= obs <= (hi + tol)
            return ok, f"{obs} {'in' if ok else 'outside'} [{lo}, {hi}]"
        try:
            target = float(rest)
        except ValueError:
            return None, f"bad predicate operand {rest!r}"
        cmp = {
            "eq": near(obs, target),
            "ne": not near(obs, target),
            "lt": obs < target - tol,
            "le": obs <= target + tol,
            "gt": obs > target + tol,
            "ge": obs >= target - tol,
        }
        if op in cmp:
            return cmp[op], f"{obs} {op} {target}"
    return None, f"unknown predicate {predicate!r}"

# --------------------------------------------------------------------------- #
# Paths
# --------------------------------------------------------------------------- #
HERE = os.path.dirname(os.path.abspath(__file__))
# scripts/re/revtool/ -> scripts/: the one owner of filesystem roots for
# Python tools (sro_paths.py) lives there. The research tree (database,
# ledgers, per-target derived exports) and the game files (binary, dump,
# rizin project) are both outside the checkout and shared by worktrees.
SCRIPTS_DIR = os.path.dirname(os.path.dirname(HERE))
if SCRIPTS_DIR not in sys.path:
    sys.path.insert(0, SCRIPTS_DIR)
import sro_paths  # noqa: E402

RECONSTRUCT_DIR = str(sro_paths.RESEARCH_ROOT)
WORKSPACE_ROOT = str(sro_paths.GAME_ROOT)

# The dump is a ~110 MB export that must NOT be tracked, so it lives in
# gitignored space (temp/dumps/) on this machine while older notes place it at
# the workspace root. Search both rather than hardcoding one: a wrong path here
# makes every `xref` scan silently find nothing (see require_dump).
DUMP_CANDIDATES = (
    os.path.join(WORKSPACE_ROOT, "temp", "dumps", "SRO_Client_psuedo.txt"),
    os.path.join(WORKSPACE_ROOT, "SRO_Client_psuedo.txt"),
)


def resolve_default_dump():
    for candidate in DUMP_CANDIDATES:
        if os.path.exists(candidate):
            return candidate
    return DUMP_CANDIDATES[0]


DEFAULT_DUMP = resolve_default_dump()
DEFAULT_DB = os.path.join(RECONSTRUCT_DIR, "revtool.db")
DEFAULT_BINARY = os.path.join(WORKSPACE_ROOT, "SRO_Client.exe")
DEFAULT_LEDGER = os.path.join(RECONSTRUCT_DIR, "revtool_symbols.jsonl")
DEFAULT_EVIDENCE_LOCK = os.path.join(RECONSTRUCT_DIR, "evidence-lock.json")
REVTOOL_SCHEMA_VERSION = 2
LEDGER_SCHEMA = "sro-revtool-evidence-ledger-v2"
EVIDENCE_LOCK_SCHEMA = "sro-re-evidence-lock-v1"
TARGET_SHA256 = "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a"
REVTOOL_VERSION = "2.0"

# The class/vtable commands and the family planner must ask the same structural
# RTTI reader.  Keeping a second regex-only scanner here caused Revtool to say
# "no primary vftable" for classes that the planner had already bounded, and
# let raw `vtable --count` walk into the next object.  The immutable HLIL dump
# remains semantic evidence, but PE RTTI owns structural class discovery.
if HERE not in sys.path:
    sys.path.insert(0, HERE)
try:
    from msvc_rtti import rtti_direct_bases, rtti_hierarchies, scan_vtables
except ImportError:  # surfaced loudly by structural commands below
    rtti_direct_bases = rtti_hierarchies = scan_vtables = None

# --------------------------------------------------------------------------- #
# Grammar
# --------------------------------------------------------------------------- #
# Every dump line begins with an 8 hex digit address, then whitespace, then code:
#   008b1100      sub_506040(&data_f0f538, 0x16, 3);
ADDR_RE = re.compile(rb"^([0-9a-fA-F]{8})(\s+)(.*)$")

# A function header line, e.g.
#   00401040    int32_t __fastcall sub_401040(struct IRegionManager::VTable** arg1)
HEADER_RE = re.compile(
    rb"__(?:fastcall|thiscall|cdecl|stdcall)\s+(sub_[0-9a-fA-F]+|[A-Za-z_]\w*)\s*\("
)

# Global symbols anywhere in code (text mode):
GLOBAL_TOKEN_RE = re.compile(r"\b((?:sub_|data_|loc_|jump_table_)[0-9a-fA-F]+)\b")
# Names already recovered by the decompiler (imports, CRT helpers, class methods) are
# readable labels, not labeling debt. The index only vouches for debt when its name is
# still one of the decompiler's anonymous function spellings.
ANONYMOUS_FUNCTION_RE = re.compile(
    r"(?:sub_|j_sub_|nullsub_)[0-9a-fA-F]+", re.IGNORECASE)

# Struct field access, displacement form: <ident> + 0xNN  (ident may be a renamed/bound var)
FIELD_ACCESS_RE = re.compile(r"\b([A-Za-z_]\w*)\s*\+\s*0x([0-9a-fA-F]+)\b")

# Struct field access, index form: <ident>[0xNN]. This decompiler dword-indexes typed
# pointers (e.g. self[0x137] == byte +0x4dc), so the byte offset is index*4.
INDEX_ACCESS_RE = re.compile(r"\b([A-Za-z_]\w*)\s*\[\s*0x([0-9a-fA-F]+)\s*\]")

# `*(T*)var + 0xNN` adds the offset to the DEREFERENCED value (a vtable slot / pointed-to
# datum), NOT to `var`. So `var + 0xNN` here is a vtable-style indirection, not a field of
# `var`. We detect the deref-cast immediately preceding the var: `*(...*)`, allowing an
# optional `&` (the `**(T**)&global + 0xNN` vtable-call shape the decompiler emits for a
# global object pointer). Note this does NOT match a plain cast `(char*)var + 0xNN` (no
# leading `*`), which IS a real field access.
DEREF_CAST_TAIL_RE = re.compile(r"\*\s*\([^()]*\*\s*\)\s*&?\s*$")

# Identifier immediately preceding a base token (i.e. `<ident> [*] <base>`). In a C
# array declaration `TYPE name[0xNN];` this captures the type token, letting us tell
# a declaration apart from a true `name[idx]` indexed access.
PRECEDING_IDENT_RE = re.compile(r"([A-Za-z_]\w*)\s*\**\s*$")
# `&` immediately before a base token: `&name[0xNN]` is address-of-element (pointer
# advance / array addressing), not a struct field load. Used to skip the `p = &p[N]`
# row-cursor stride idiom that would otherwise report a phantom field at N*4.
AMP_TAIL_RE = re.compile(r"&\s*$")
# An open subscript bracket immediately before a base token: `base[idx + 0xNN]`. The
# `idx + 0xNN` is subscript (index) arithmetic, not a field of `idx`.
BRACKET_TAIL_RE = re.compile(r"\[\s*$")
# Code symbols and generated jump-table labels can appear in target arithmetic such as
# `sub_86ca90+0x91` or `jump_table_50cce4[2]`. Those are addresses into `.text`/
# jump-table storage, not struct receiver fields. Keep `data_` globals eligible because
# `data_cec870+0x494` is commonly a real singleton/member access in this dump.
NON_FIELD_BASE_PREFIXES = ("sub_", "loc_", "jump_table_")
# A struct-field byte offset has a sane upper bound. The largest genuine field in this
# codebase is CRTNavMeshTerrain+0x1b384 (~111 KB). Anything >= 1 MiB is never a member
# of a receiver object -- it's an absolute .data/.rdata address the decompiler wrote as
# `base + 0xADDR` (e.g. `arg2 + 0xc1bce4`), a sentinel/mask constant (`result + 0x7fffffff`,
# `x + 0xffffffff`), or a WRAPPED NEGATIVE index whose *4 explodes (`ebx[0xffffffff]` ->
# 0x3fffffffc). A 1 MiB ceiling keeps every real field (>=9x margin over the largest known)
# while dropping all of that arithmetic noise so it can't inflate coverage/depth.
FIELD_OFFSET_MAX = 0x100000
# Control-flow keywords that can legitimately precede an indexed access (`return v[i]`)
# and so must NOT be treated as a declaration's type token. (str form of DECL_KEYWORDS.)
DECL_KEYWORD_STRS = {
    "if", "while", "for", "switch", "catch", "return", "else", "do",
    "sizeof", "case", "goto", "default", "try", "throw",
}

# Binary Ninja frequently splits stack aggregates into scalar locals and then
# renders ordinary coordinate/count arithmetic in the same `base + 0xNN`
# spelling it uses for pointer displacement. Those scalars must not create
# fictitious struct-field debt in `coverage`. Pointer declarations deliberately
# do not match because `*` interrupts the required whitespace before the name.
SCALAR_LOCAL_DECL_RE = re.compile(
    r"^\s*(?:(?:const|volatile)\s+)*"
    r"(?:bool|char|wchar_t|float|double|"
    r"u?int(?:8|16|32|64)_t|short|int|long(?:\s+long)?|"
    r"(?:signed|unsigned)(?:\s+(?:char|short|int|long(?:\s+long)?))?)"
    r"\s+([A-Za-z_]\w*)\b"
)


def field_accesses(code):
    """(var, byte_offset) struct-field accesses in a code string, both displacement
    (`var + 0xNN`) and index (`var[0xNN]`) shapes. Non-field arithmetic is filtered so it
    can't be mistaken for a member: vtable-indirect `*(T*)var + 0xNN`, code-symbol bases
    (`sub_`/`loc_`/`jump_table_`), implausibly large offsets (>= FIELD_OFFSET_MAX absolute
    addresses / sentinel constants / wrapped-negative indices), and subscript/declaration/
    address-of forms."""
    out = []
    for mm in FIELD_ACCESS_RE.finditer(code):
        base = mm.group(1)
        if base.startswith(NON_FIELD_BASE_PREFIXES):
            continue
        before = code[:mm.start(1)]
        if DEREF_CAST_TAIL_RE.search(before):
            continue  # `*(T*)var + off` -> vtable/indirect, not a field of var
        # `base[idx + 0xNN]` is INDEX arithmetic on `base`'s element type -- the
        # `+ 0xNN` belongs to the subscript, and `idx` is usually a loop counter,
        # not a pointer. e.g. `ecx_12[j_2 + 0x189]` must NOT yield a `j_2 + 0x189`
        # field. A real field base is preceded by `(`, `*`, `&`, an operator or line
        # start -- never by an open bracket. Skip when preceded by `[`.
        if BRACKET_TAIL_RE.search(before):
            continue
        off = int(mm.group(2), 16)
        if off >= FIELD_OFFSET_MAX:
            continue  # absolute address / sentinel constant, not a struct field
        out.append((base, off))
    for mm in INDEX_ACCESS_RE.finditer(code):
        base = mm.group(1)
        if base.startswith(NON_FIELD_BASE_PREFIXES):
            continue
        before = code[:mm.start(1)]
        # `TYPE name[0xNN];` is a C array DECLARATION (the bracket is the array
        # dimension, not an index) -- e.g. `char var_38[0x30];`. A real indexed
        # access (`eax_36[0x38]`) is preceded by an operator / paren / line start,
        # never by a bare type identifier. Skip when the base ident is preceded by
        # another identifier that is not a control keyword (return foo[i] etc.).
        pm = PRECEDING_IDENT_RE.search(before)
        if pm and pm.group(1) not in DECL_KEYWORD_STRS:
            continue
        # `&name[0xNN]` is ADDRESS arithmetic (take address of an element), the
        # decompiler's pointer-advance idiom `p = &p[0x11]` (stride), NOT a field
        # load. The `&` immediately before the base marks address-of, so skip it.
        if AMP_TAIL_RE.search(before):
            continue
        off = int(mm.group(2), 16) * 4
        if off >= FIELD_OFFSET_MAX:
            continue  # wrapped-negative index (e.g. base[0xffffffff]) exploded by *4
        out.append((base, off))
    return out

# A token we are allowed to rename in-place (locals + globals are matched by the
# overlay map; this is just the generic identifier shape).
IDENT_RE = re.compile(r"[A-Za-z_]\w*")

# Control-flow / statement keywords that look like "name(" but are NOT functions.
# Used to reject false positives when detecting headers structurally (a `<addr> {`
# line preceded by a same-address declaration).
DECL_KEYWORDS = {
    b"if", b"while", b"for", b"switch", b"catch", b"return", b"else", b"do",
    b"sizeof", b"case", b"goto", b"default", b"try", b"throw",
}
# The trailing identifier (function name) just before a '(' on a line.
DECL_NAME_RE = re.compile(rb"([A-Za-z_][\w:~]*)\s*$")
# A compiler annotation the decompiler emits BETWEEN the return type and the function
# name, e.g. `int32_t __convention("regparm") __allshr(int32_t, int32_t, char)`. Its
# argument list is NOT the function's parameter list, so it must be removed before the
# prefix is validated as a return type. Restricted to `__`-prefixed keywords with a
# flat argument list: surveying all 95,290 indexed headers found `__convention` to be
# the only annotation that ever occupies this position.
HEADER_ANNOTATION_RE = re.compile(rb"\b__\w+\s*\([^()]*\)\s*")
# A valid C return type, as emitted by the decompiler: optional qualifiers, an
# identifier (possibly ::-qualified, optionally one template arg), then */& and
# whitespace. Anchored to the whole prefix so expression fragments are rejected.
_RET_TYPE_RE = re.compile(
    rb"^(?:(?:struct|union|enum|const|unsigned|signed|volatile)\s+)*"
    rb"[A-Za-z_][\w:]*(?:\s*<[^>]*>)?[\s\*&]*$"
)
# Operator substrings that can never appear in a return type but do appear in
# multi-line expression / condition continuations (the main false-positive risk
# for the structural `<addr> {` rule).
_NON_TYPE_OPS = (b"&&", b"||", b"==", b"!=", b"<=", b">=", b"(", b")", b"+",
                 b"=", b"!", b"?", b"[", b"]", b"->", b"++", b"--", b"/",
                 b"%", b",", b".")


def header_name_for_line(addr_b, code):
    """Function name for a header line under index rules (a) and (b), else None.

    (a) an explicit calling convention near the line start; (b) a self-referential
    `sub_<own-address>(` definition, which covers convention-less leaf functions.
    Rule (c) - the structural `<addr> {` follower - is header_decl_name(); it needs the
    PREVIOUS line and so stays in the scan loop.

    Extracted from `index` so that the indexer and anything AUDITING the index apply
    one rule rather than two copies of it that agree until one of them is fixed.
    """
    hm = HEADER_RE.search(code)
    if hm and hm.start() < 60:
        return hm.group(1)
    expected = b"sub_" + addr_b.lstrip(b"0")
    if re.search(rb"\b" + expected + rb"\s*\(", code):
        return expected
    return None


def depth0_ident_calls(code):
    """Every `IDENT(` at parenthesis depth 0 on `code`, in order.

    Returns (ident, ident_start, paren_pos) tuples. Depth matters because a function's
    own parameters live at depth >= 1, so the only depth-0 candidates are the parts of
    the DECLARATOR itself: any annotation groups, and the function name.
    """
    out = []
    depth = 0
    for i in range(len(code)):
        c = code[i:i + 1]
        if c == b"(":
            if depth == 0:
                m = DECL_NAME_RE.search(code[:i])
                if m:
                    out.append((m.group(1), m.start(), i))
            depth += 1
        elif c == b")" and depth:
            depth -= 1
    return out


def header_decl_name(code):
    """If `code` (bytes) looks like a function DEFINITION, return its name token.

    A definition is `<return-type> <name>(` -- e.g. `int80_t sub_9c6920(...)`.
    Returns None for calls/statements/control flow (`if (...)`, `foo(x);`) and for
    multi-line expression continuations (`&& sub_x(arg))`), so the structural
    `<addr> {` rule stays false-positive free. (Real headers either carry a calling
    convention or are `sub_<addr>`; this only adds convention-less NAMED functions.)

    The name is taken from the LAST depth-0 `IDENT(`, not the first '(' on the line.
    Scanning to the first '(' names the function after an intervening ANNOTATION: in
      009c68f0  int32_t __convention("regparm") __allshr(int32_t, int32_t, char) __pure
    the first '(' opens the annotation's argument list, so the old scan returned
    `__convention` and 29 distinct addresses were all indexed under that one keyword -
    a name that is a calling-convention marker rather than any function's identity.
    """
    cands = depth0_ident_calls(code)
    if not cands:
        return None
    name, name_start, _paren = cands[-1]
    if name in DECL_KEYWORDS:
        return None
    # The prefix is the return type plus any annotations; drop the annotations so a
    # legitimate `int32_t __convention("regparm")` prefix still reads as `int32_t`.
    rettype = HEADER_ANNOTATION_RE.sub(b" ", code[:name_start]).strip()
    # Must have a return type (bare `name(` is a call) that looks like a real type.
    if not rettype:
        return None
    if any(op in rettype for op in _NON_TYPE_OPS):
        return None
    if not _RET_TYPE_RE.match(rettype):
        return None
    return name


# --------------------------------------------------------------------------- #
# Database
# --------------------------------------------------------------------------- #
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
def iter_dump_symbol_hits(dump, symbol):
    """Yield (line_start, line_bytes) for each dump line referencing `symbol` as a word.

    This is the single scan that decides whether a target is real. `xref` reports its
    results, and the debt ledger asks it whether a charge is payable at all, so both
    consult THIS function rather than each carrying a copy of the rule.

    The boundary test is a `\\b`-anchored regex and NOT the neighbouring-byte test in
    token_occurs_in_dump(): for a needle that starts with a non-word byte (`+ 0x88`)
    the two conditions are exact opposites, so substituting one for the other silently
    reclassifies 15 targets in the live store. They are different questions - that one
    asks whether a RENAME KEY can ever fire, this one asks what `xref` actually finds.
    """
    needle = symbol.encode("utf-8") if isinstance(symbol, str) else symbol
    if not needle or not dump or not os.path.exists(dump):
        return
    word = re.compile(rb"\b" + re.escape(needle) + rb"\b")
    with open(dump, "rb") as f:
        mm = mmap.mmap(f.fileno(), 0, access=mmap.ACCESS_READ)
        try:
            pos = 0
            while True:
                idx = mm.find(needle, pos)
                if idx == -1:
                    break
                line_start = mm.rfind(b"\n", 0, idx) + 1
                line_end = mm.find(b"\n", idx)
                if line_end == -1:
                    line_end = len(mm)
                line = mm[line_start:line_end]
                if word.search(line):
                    yield line_start, line
                pos = idx + len(needle)
        finally:
            mm.close()


# Memo for dump_has_symbol_reference. The dump cannot change while a command runs, and
# a MISS costs a full 115 MB scan (~30 ms), which compute_debt would otherwise repeat
# for every unlabeled target on every call.
_DUMP_HIT_MEMO = {}


def dump_has_symbol_reference(dump, symbol):
    """True if the dump references `symbol` as a whole word (memoised per process)."""
    key = (dump, symbol)
    hit = _DUMP_HIT_MEMO.get(key)
    if hit is None:
        hit = next(iter_dump_symbol_hits(dump, symbol), None) is not None
        _DUMP_HIT_MEMO[key] = hit
    return hit


def resolve_scan_symbol(con, symbol):
    """The dump token that `symbol` actually denotes.

    `xref` accepts three spellings of the same thing - the dump's original sub_/data_
    token, an APPLIED rename name, and a bare address - and only the first occurs in
    the dump, so resolving them to one token is what lets the scan find anything.

    The debt ledger has to resolve IDENTICALLY or it judges the same target differently
    depending on how it happened to be spelled when it was recorded: a row keyed
    `0x401000` by an older `xref` denotes the very real `sub_401000`, and calling that
    a non-symbol would ERASE debt somebody genuinely owes.
    """
    if not symbol:
        return symbol
    scan_sym = symbol
    if not con.execute("SELECT 1 FROM globals WHERE orig_name=? LIMIT 1",
                       (symbol,)).fetchone():
        rev = con.execute(
            "SELECT orig_name FROM globals WHERE new_name=?", (symbol,)).fetchone()
        if rev:
            scan_sym = rev[0]
    if scan_sym == symbol and not con.execute(
            "SELECT 1 FROM functions WHERE name=? LIMIT 1", (symbol,)).fetchone():
        a = norm_addr(symbol)
        if a:
            frow = con.execute(
                "SELECT name FROM functions WHERE addr=?", (a,)).fetchone()
            if frow:
                scan_sym = frow[0]
    return scan_sym


def symbol_is_identifiable(con, symbol, dump=None, *, found_in_dump=False,
                           consult_dump=True):
    """True if the overlay can identify `symbol` as a labelable program symbol.

    Overlay rename keys vouch for their entries directly. The function index vouches
    for anonymous sub_/j_sub_/nullsub_ entries; an indexed import such as
    GetGlyphOutlineW is already a readable label and needs no second rename.
    The text dump is different: xref deliberately accepts arbitrary search probes
    (opcodes, enum names, string literals, field names), so a whole-word hit alone
    does NOT prove the probe is something an engineer can rename. Only the dump's
    canonical anonymous-symbol spellings (sub_/data_/loc_/jump_table_) are label
    targets merely by occurring in the dump.

    This is the test `xref` applies before it says "not a known symbol", and it is now
    also the test the DEBT LEDGER applies before it bills one. Charging debt against a
    search term such as `0x61a4`, `UIIT_CTL_NO`, or `snd_activate` creates a row
    that can never be discharged by labeling a program symbol, because those strings
    are already the readable evidence being searched. Excluding those rows stops the
    billing while leaving the row in place, so the audit trail survives intact.

    `found_in_dump` lets a caller that has ALREADY run the identical scan pass its
    result in; `consult_dump=False` says that caller's scan was authoritative, so a
    miss is a miss and re-scanning could only produce the same answer.
    """
    target = resolve_scan_symbol(con, symbol)
    if con.execute("SELECT 1 FROM globals WHERE orig_name=? LIMIT 1",
                   (target,)).fetchone():
        return True
    if con.execute("SELECT 1 FROM functions WHERE name=? LIMIT 1",
                   (target,)).fetchone():
        return bool(ANONYMOUS_FUNCTION_RE.fullmatch(target))
    # The caller already performed the authoritative dump scan. A hit vouches only
    # for canonical anonymous-symbol syntax; a miss vouches for nothing.
    if not consult_dump:
        return bool(found_in_dump and GLOBAL_TOKEN_RE.fullmatch(target))
    if dump is None:
        dump = dump_path_from_meta(con, must_exist=True)
    if not dump or not os.path.exists(dump):
        # The question cannot be answered honestly. Guessing "not a symbol" would ERASE
        # real debt, which is the one outcome worse than keeping noise, so bill as before.
        return True
    # A dump hit proves only that text was found. `xref` is also a free-text research
    # tool, and readable constants/string keys/import names are not anonymous symbols
    # that need a second label. Canonical decompiler tokens are the only dump-only
    # label targets.
    if not GLOBAL_TOKEN_RE.fullmatch(target):
        return False
    if found_in_dump:
        return True
    return dump_has_symbol_reference(dump, target)


def symbol_is_annotated(con, symbol):
    """True if `symbol` carries ANY deliberate label, so it won't read as un-RE'd.

    A global rename, a symbol-level note (with or without an @addr suffix), or an
    assertion about the symbol all count - even a tier-`guess` hypothesis. This is
    what an investigation must produce to discharge its debt.
    """
    if con.execute("SELECT 1 FROM globals WHERE orig_name=? OR new_name=?",
                   (symbol, symbol)).fetchone():
        return True
    applied = None
    r = con.execute("SELECT new_name FROM globals WHERE orig_name=?", (symbol,)).fetchone()
    if r:
        applied = r[0]
    for s in [symbol] + ([applied] if applied else []):
        # Match a symbol note (`target = s`) or a line note (`target = s@ADDR`). The `s@%`
        # form is expressed as the half-open range [s+'@', s+'A') so it uses the
        # notes(target) index: '@' is 0x40 and 'A' is 0x41, so every `s@...` sorts inside
        # the range and nothing else does. A bare `LIKE 's@%'` could not use the index and
        # made this O(rows) per call (see the notes_target index comment).
        if con.execute("SELECT 1 FROM notes WHERE target=? OR (target>=? AND target<?) "
                       "LIMIT 1", (s, s + "@", s + "A")).fetchone():
            return True
    if con.execute("SELECT 1 FROM assertions WHERE subject=? OR subject LIKE ?",
                   (symbol, symbol + "%")).fetchone():
        return True
    return False


def symbol_is_renamed(con, symbol):
    """True only if `symbol` has an APPLIED rename, so the disassembly view shows a
    readable name instead of the raw sub_/data_ token. A `seen` guess produces a note
    (which `symbol_is_annotated` accepts and which discharges debt) but NOT a rename, so
    the view still prints the raw token. This is the gap between 'labeled' and 'readable'.
    """
    return con.execute(
        "SELECT 1 FROM globals WHERE orig_name=? AND new_name IS NOT NULL",
        (symbol,)).fetchone() is not None


def record_access(con, symbol, kind, author):
    """Upsert one investigative touch into the ledger."""
    now = now_iso()
    if con.execute("SELECT 1 FROM access_log WHERE symbol=? AND kind=?",
                   (symbol, kind)).fetchone():
        con.execute("UPDATE access_log SET count=count+1, last_ts=?, author=? "
                    "WHERE symbol=? AND kind=?", (now, author, symbol, kind))
    else:
        con.execute("INSERT INTO access_log(symbol,kind,author,first_ts,last_ts,count) "
                    "VALUES(?,?,?,?,?,1)", (symbol, kind, author, now, now))


def prior_investigation(con, symbol):
    """Prior investigative touches of `symbol` already in the ledger.

    Returns the (kind,count,first_ts,last_ts,author) rows recorded BEFORE the current
    command. Used to warn the next engineer that someone already burned time here, so
    abandoned (unlabeled) work becomes visible at the moment it would be repeated.
    """
    return con.execute(
        "SELECT kind,count,first_ts,last_ts,author FROM access_log "
        "WHERE symbol=? AND kind IN ('viewed','xref') ORDER BY last_ts", (symbol,)).fetchall()


def warn_if_previously_investigated(con, symbol, *, stream=None):
    """If `symbol` was already investigated but never labeled, surface it loudly.

    This is the collaborative payoff: the failure mode is an engineer who reads code,
    walks away without labeling, and the NEXT person silently redoes the work. Firing
    this banner the instant they open the symbol turns that lost effort into a signal.
    Returns True if a banner was printed.
    """
    out = stream or sys.stdout
    if symbol_is_annotated(con, symbol):
        return False
    rows = prior_investigation(con, symbol)
    if not rows:
        return False
    total = sum(r[1] for r in rows)
    last_ts = max((r[3] for r in rows), default="")
    who = next((r[4] for r in reversed(rows) if r[4]), "someone")
    kinds = "+".join(sorted({r[0] for r in rows}))
    print("!! ALREADY INVESTIGATED, NEVER LABELED "
          f"({kinds} x{total}, last {last_ts} by {who}).", file=out)
    print("   You may be repeating lost work. Label it now (or ask the author) so the "
          "next person doesn't redo it again:", file=out)
    print(f'     revtool seen {symbol} "<what it does / your best guess>"', file=out)
    return True


def compute_debt(con, author=None):
    """Distinct symbols directly investigated (viewed/xref'd) that still have no label.

    Aggregated PER SYMBOL: a symbol that was both xref'd and viewed is ONE unit of
    debt, not two, so the exploration budget counts symbols you owe a label for - not
    raw ledger rows. Returns (symbol, kinds, total_count, last_ts, author) sorted by
    when it was last touched.

    When `author` is given, only that author's investigative touches are counted. This
    is what makes the exploration budget PER-AGENT on a shared DB: many agents write to
    the same access_log, but each one's budget is scoped to the work IT did. A symbol
    labeled by ANY author is discharged for everyone (symbol_is_annotated is global), so
    shared knowledge still clears shared debt.

    A row whose target is NOT A SYMBOL is skipped, via the same symbol_is_identifiable()
    test `xref` uses to decide whether to charge in the first place. Those rows assert
    "author X investigated `+ 0x88`"; there is no such symbol, so no label can ever
    discharge them, yet they counted toward the cap that blocks work at 40. They are
    false as DEBT but true as HISTORY - somebody really did run that xref - so they stay
    in access_log and simply stop billing. Deleting them would fix the ledger by
    destroying the record.
    """
    sql = ("SELECT symbol,kind,count,last_ts,author FROM access_log "
           "WHERE kind IN ('viewed','xref')")
    params = ()
    if author is not None:
        sql += " AND author=?"
        params = (author,)
    sql += " ORDER BY last_ts"
    dump = dump_path_from_meta(con, must_exist=True)
    agg = {}
    for sym, kind, count, last_ts, author in con.execute(sql, params):
        if symbol_is_annotated(con, sym):
            continue
        if not symbol_is_identifiable(con, sym, dump):
            continue
        cur = agg.get(sym)
        if cur is None:
            agg[sym] = {"kinds": {kind}, "count": count, "last_ts": last_ts, "author": author}
        else:
            cur["kinds"].add(kind)
            cur["count"] += count
            if last_ts >= cur["last_ts"]:
                cur["last_ts"], cur["author"] = last_ts, author
    out = [(sym, "+".join(sorted(d["kinds"])), d["count"], d["last_ts"], d["author"])
           for sym, d in agg.items()]
    out.sort(key=lambda r: r[3])
    return out


def strict_enabled(args, con):
    """Strict enforcement is ON by default; --no-strict, env, or meta can disable."""
    if getattr(args, "no_strict", False):
        return False
    if getattr(args, "strict", False):
        return True
    env = os.environ.get("REVTOOL_STRICT")
    if env is not None:
        return env.strip().lower() not in ("0", "", "false", "off", "no")
    row = con.execute("SELECT value FROM meta WHERE key='strict'").fetchone()
    if row and row[0] is not None:
        return row[0].strip().lower() not in ("0", "off", "false", "no", "")
    return True


DEFAULT_DEBT_LIMIT = 3


def debt_limit(con):
    """How many unlabeled symbols you may roam before the gate blocks.

    RE is non-linear: you read a function plus a few of its neighbours before any
    of them makes sense. The budget (default 3) lets you explore a small call-graph
    neighbourhood, then forces you to park labels before the backlog grows. Tunable
    via `config debt_limit N`, $REVTOOL_DEBT_LIMIT, or set to 1 for label-after-each.
    """
    env = os.environ.get("REVTOOL_DEBT_LIMIT")
    if env is not None:
        try:
            return max(1, int(env))
        except ValueError:
            pass
    row = con.execute("SELECT value FROM meta WHERE key='debt_limit'").fetchone()
    if row and row[0]:
        try:
            return max(1, int(row[0]))
        except ValueError:
            pass
    return DEFAULT_DEBT_LIMIT


def investigation_gate(con, args, current_symbol):
    """In strict mode, block a NEW investigation once the unlabeled backlog hits the
    budget. Returns an exit code (2) to abort, or None to proceed.

    The symbol currently being investigated is always exempt (you must read it to
    label it), and you may carry up to `debt_limit` unlabeled symbols so a normal
    neighbourhood sweep is never blocked mid-thought. `--force` bypasses once and is
    recorded in history so bypasses stay auditable.
    """
    if not strict_enabled(args, con):
        return None
    if getattr(args, "force", False):
        record_history(con, "access", f"access:{current_symbol}", "force-bypass",
                       None, current_symbol, get_author(args), "strict bypass")
        con.commit()
        return None
    author = get_author(args)
    debt = [d for d in compute_debt(con, author=author) if d[0] != current_symbol]
    limit = debt_limit(con)
    if len(debt) < limit:
        return None
    print(f"REFUSED (strict): exploration budget full ({len(debt)}/{limit}) for author "
          f"'{author}'. Label some of what you already investigated before opening "
          f"{current_symbol}.", file=sys.stderr)
    if not author_is_explicit(args):
        print(f"  NOTE: author '{author}' is your OS username (no --author/$REVTOOL_AUTHOR). "
              "If several agents run as this user they SHARE one budget - set a distinct "
              "REVTOOL_AUTHOR per agent to isolate them.", file=sys.stderr)
    for sym, kind, count, _ts, _a in debt[:20]:
        print(f"    {sym:<26} ({kind}, x{count})", file=sys.stderr)
    if len(debt) > 20:
        print(f"    ... {len(debt) - 20} more  (revtool review --debt)", file=sys.stderr)
    print("  Discharge with a REAL one-liner (what it does / your best guess):", file=sys.stderr)
    print('    revtool seen <symbol> "thin forwarder to vtable+0x10 (builds cam view)"',
          file=sys.stderr)
    print("    revtool rename global <symbol> <Name> --tier guess --evidence \"...\"", file=sys.stderr)
    print('    revtool note add <symbol> "<behaviour>" --tier mechanism', file=sys.stderr)
    print(f"  Raise the budget: revtool config debt_limit {limit + 3}   |   "
          "bypass once: --force   |   off: config strict off", file=sys.stderr)
    return 2


def print_debt_footer(con, current_symbol, author=None):
    """Loud, unmissable reminder printed after every view/xref, with the live budget.

    Budget is scoped to `author` so the number shown matches what actually gates this
    agent on a shared DB (see compute_debt).
    """
    total = compute_debt(con, author=author)
    current_labeled = symbol_is_annotated(con, current_symbol)
    others = [d for d in total if d[0] != current_symbol]
    if current_labeled and not others:
        return
    limit = debt_limit(con)
    n = len(total)
    print("-" * 78)
    if not current_labeled:
        print(f"!! UNLABELED: you investigated {current_symbol} but it has no annotation.")
        print("   Label it (a real one-liner) so the next engineer doesn't redo it:")
        print(f'     revtool seen {current_symbol} "<what it does / your best guess>"')
        print(f"     revtool rename global {current_symbol} <Name> --tier guess --evidence \"...\"")
        print(f'     revtool note add {current_symbol} "<behaviour>" --tier mechanism')
    if others:
        print(f"   other unlabeled: " +
              ", ".join(d[0] for d in others[:8]) +
              (f", +{len(others) - 8} more" if len(others) > 8 else ""))
    # Live budget so the block is never a surprise.
    if n >= limit:
        print(f"   ** budget FULL ({n}/{limit}) - the next view/xref is BLOCKED until you "
              "label. (review --debt)")
    else:
        room = limit - n
        print(f"   budget {n}/{limit} - {room} more investigation(s) before you must label.")


# --------------------------------------------------------------------------- #
# depth-on-confidence: a CONFIDENT function name must carry >=1 structural detail
# --------------------------------------------------------------------------- #
DEEP_TIERS = {"mechanism", "pipeline", "verified"}
SHALLOW_MARK = "[shallow-ack]"
STRUCTURE_MIN_OFFSETS = 4  # field offsets before a function "has structure worth naming"


def function_field_offsets(lines):
    """Distinct (base, byte-offset) struct-field accesses in a body (both shapes)."""
    pairs = set()
    scalar_locals = set()
    for ln in lines:
        m = re.match(r"^[0-9a-fA-F]{8}\s+", ln)
        code = ln[m.end():] if m else ln
        decl = SCALAR_LOCAL_DECL_RE.match(code)
        if decl:
            scalar_locals.add(decl.group(1))
        pairs.update(field_accesses(code))
    # drop non-struct bases: thread block + raw stack/frame pointers (esp+N / ebp+N are
    # stack locals, not struct fields). Also drop locals positively declared as
    # scalar values; their additive constants are arithmetic, not displacement.
    return {
        p for p in pairs
        if p[0] not in ("fsbase", "ExceptionList", "esp", "ebp")
        and p[0] not in scalar_locals
    }


def depth_satisfied(con, name, func_addr):
    """True once the function carries at least one structural breadcrumb (or shallow ack)."""
    if con.execute("SELECT 1 FROM locals WHERE func_addr=?", (func_addr,)).fetchone():
        return True
    if con.execute("SELECT 1 FROM bindings WHERE func_addr=?", (func_addr,)).fetchone():
        return True
    if con.execute("SELECT 1 FROM notes WHERE target=? AND body LIKE ?",
                   (name, SHALLOW_MARK + "%")).fetchone():
        return True
    return False


def depth_gate(con, args, dump, orig_name, tier):
    """A confident (mechanism+) function claim must capture some of what was understood.

    The failure this fixes: a bare function name discharges all debt, so the rich,
    compounding knowledge (the `this`/main object, struct fields) is never written down
    and a `mechanism` name even discourages the next engineer from digging. So a
    confident claim on a STRUCTURED function (>= STRUCTURE_MIN_OFFSETS field accesses)
    must leave >=1 breadcrumb: a local rename, a binding, a named field, or `--shallow`.
    `guess`/`unknown` stay frictionless. Returns exit code 2 to abort, else None.
    """
    if not strict_enabled(args, con):
        return None
    if tier not in DEEP_TIERS:
        return None
    row = resolve_func(con, orig_name)
    if not row:
        return None  # not a function (e.g. data_ symbol) -> no depth requirement
    func_addr, orig = row[0], row[1]
    if getattr(args, "shallow", False):
        con.execute(
            "INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
            (orig, SHALLOW_MARK + " no locals/fields worth naming",
             get_author(args), "guess", now_iso()))
        con.commit()
        return None
    if depth_satisfied(con, orig, func_addr):
        return None
    offs = function_field_offsets(read_func_lines(dump, row[3], row[4]))
    if len(offs) < STRUCTURE_MIN_OFFSETS:
        return None  # trivial function: nothing structural to capture
    print(f"REFUSED (strict): '{orig}' -> {tier} asserts you understand it, but it "
          f"touches {len(offs)} struct-field offset(s) and has ZERO named "
          "locals/fields/bindings.", file=sys.stderr)
    print("  A bare name discharges debt but loses the real knowledge. Leave one "
          "breadcrumb:", file=sys.stderr)
    print(f"    revtool rename local {orig} <var> <name>      # e.g. the this/main pointer",
          file=sys.stderr)
    print(f"    revtool bind {orig} <var> <Type>  (+ revtool field <Type> 0xNN <name>)",
          file=sys.stderr)
    print("  Genuinely nothing worth naming? say so (honest + audited):", file=sys.stderr)
    print(f"    revtool rename global {orig} <Name> --tier {tier} --shallow", file=sys.stderr)
    print("  Not actually sure? claim it as a guess instead: --tier guess", file=sys.stderr)
    return 2


def locate_claim(con, target):
    """Resolve a claim reference string to a concrete row handle.

    Forms:
      global:<orig|applied>        a sub_/data_ rename
      local:<func>:<orig>          a function-local rename
      field:<type>:<0xoff>         a struct field
      bind:<func>:<var>            a function-local type binding
      note:<id>                    a note
    Returns dict(kind, table, where, params, ckey, label) or None.
    """
    if ":" not in target:
        # bare convenience: treat as a global symbol
        target = "global:" + target
    kind, rest = target.split(":", 1)

    if kind == "global":
        name = rest
        row = con.execute("SELECT orig_name FROM globals WHERE orig_name=?", (name,)).fetchone()
        if not row:
            row = con.execute("SELECT orig_name FROM globals WHERE new_name=?", (name,)).fetchone()
        if not row:
            # ...and by ADDRESS, which `view`, `coverage` and `bind` all accept via
            # resolve_func. Without this, mark/dispute/resolve/history refused a
            # spelling the rest of the tool takes, and `history` turned that refusal
            # into a silent empty audit trail.
            f = resolve_func(con, name)
            if f:
                row = con.execute(
                    "SELECT orig_name FROM globals WHERE orig_name=?", (f[1],)).fetchone()
        if not row:
            return None
        orig = row[0]
        return dict(kind="global", table="globals", where="orig_name=?",
                    params=[orig], ckey=f"global:{orig}", label=f"global {orig}")

    if kind == "local":
        if ":" not in rest:
            return None
        func, orig = rest.split(":", 1)
        f = resolve_func(con, func)
        if not f:
            return None
        faddr = f[0]
        ok = con.execute(
            "SELECT 1 FROM locals WHERE func_addr=? AND orig_name=?", (faddr, orig)).fetchone()
        if not ok:
            return None
        return dict(kind="local", table="locals", where="func_addr=? AND orig_name=?",
                    params=[faddr, orig], ckey=f"local:{faddr}:{orig}",
                    label=f"local {f[1]}:{orig}")

    if kind == "field":
        if ":" not in rest:
            return None
        tname, off_s = rest.split(":", 1)
        off = int(off_s, 16)
        ok = con.execute(
            "SELECT 1 FROM fields WHERE type_name=? AND offset=?", (tname, off)).fetchone()
        if not ok:
            return None
        return dict(kind="field", table="fields", where="type_name=? AND offset=?",
                    params=[tname, off], ckey=f"field:{tname}:0x{off:x}",
                    label=f"field {tname}+0x{off:x}")

    if kind == "bind":
        if ":" not in rest:
            return None
        func, var = rest.split(":", 1)
        f = resolve_func(con, func)
        if not f:
            return None
        faddr = f[0]
        ok = con.execute(
            "SELECT 1 FROM bindings WHERE func_addr=? AND var_name=?",
            (faddr, var)).fetchone()
        if not ok:
            return None
        return dict(kind="bind", table="bindings", where="func_addr=? AND var_name=?",
                    params=[faddr, var], ckey=f"bind:{faddr}:{var}",
                    label=f"bind {f[1]}:{var}")

    if kind == "note":
        nid = int(rest)
        ok = con.execute("SELECT 1 FROM notes WHERE id=?", (nid,)).fetchone()
        if not ok:
            return None
        return dict(kind="note", table="notes", where="id=?", params=[nid],
                    ckey=f"note:{nid}", label=f"note {nid}")

    if kind == "assert":
        aid = int(rest)
        row = con.execute("SELECT subject FROM assertions WHERE id=?", (aid,)).fetchone()
        if not row:
            return None
        return dict(kind="assert", table="assertions", where="id=?", params=[aid],
                    ckey=f"assert:{aid}", label=f"assert #{aid} ({row[0]})")
    return None


def norm_addr(s):
    """Normalise an address-ish string to 8-hex lowercase, or None."""
    s = s.strip().lower()
    if s.startswith("0x"):
        s = s[2:]
    if re.fullmatch(r"[0-9a-f]+", s):
        return s.zfill(8)
    return None


# --------------------------------------------------------------------------- #
# index
# --------------------------------------------------------------------------- #
def cmd_index(args, con):
    dump = args.dump
    if not os.path.exists(dump):
        print(f"error: dump not found: {dump}", file=sys.stderr)
        return 1

    print(f"indexing {dump} ...")
    headers = []  # (addr, name, signature, start_byte, start_line)
    seen_addr = set()

    def add_header(addr_b, name_b, code_b, off, ln):
        addr = addr_b.decode("ascii").lower()
        if addr in seen_addr:
            return False
        seen_addr.add(addr)
        headers.append((addr, name_b.decode("ascii"),
                        code_b.decode("utf-8", "replace").rstrip(), off, ln))
        return True

    with open(dump, "rb") as f:
        offset = 0
        line_no = 0
        # Last non-blank addr line, kept so a following `<addr> {` can promote a
        # convention-less declaration into a header.
        prev = None  # (addr_b, code_b, offset, line_no)
        for raw in f:
            line_no += 1
            m = ADDR_RE.match(raw)
            if not m:
                # Blank lines keep adjacency (header/brace are separated by one);
                # any other non-addr line breaks it.
                if raw.strip():
                    prev = None
                offset += len(raw)
                continue

            addr_b = m.group(1).lower()
            code = m.group(3)
            is_header = False

            # Rules (a) and (b), see header_name_for_line().
            name_b = header_name_for_line(addr_b, code)
            if name_b is not None:
                if add_header(addr_b, name_b, code, offset, line_no):
                    is_header = True

            if is_header:
                prev = None
                offset += len(raw)
                continue

            # Rule (c): a `<addr> {` line whose same-address predecessor is a
            # function declaration we have not captured yet.
            if code.strip() == b"{" and prev is not None and prev[0] == addr_b:
                pname = header_decl_name(prev[1])
                if pname:
                    add_header(prev[0], pname, prev[1], prev[2], prev[3])
                    prev = None
                    offset += len(raw)
                    continue

            prev = (addr_b, code, offset, line_no)
            offset += len(raw)
        total_bytes = offset
        total_lines = line_no

    # Headers must be in file order for the end-byte (next header) computation.
    headers.sort(key=lambda h: h[3])

    # Compute end_byte/end_line as the start of the next header.
    con.execute("DELETE FROM functions")
    rows = []
    for i, (addr, name, sig, sb, sl) in enumerate(headers):
        if i + 1 < len(headers):
            eb = headers[i + 1][3]
            el = headers[i + 1][4] - 1
        else:
            eb = total_bytes
            el = total_lines
        rows.append((addr, name, sig, sb, eb, sl, el))
    con.executemany(
        "INSERT OR REPLACE INTO functions "
        "(addr,name,signature,start_byte,end_byte,start_line,end_line) "
        "VALUES (?,?,?,?,?,?,?)",
        rows,
    )
    con.execute(
        "INSERT OR REPLACE INTO meta(key,value) VALUES('dump',?)", (os.path.abspath(dump),)
    )
    con.execute(
        "INSERT OR REPLACE INTO meta(key,value) VALUES('dump_size',?)", (str(total_bytes),)
    )
    dump_hash = sha256_file(dump)
    meta_set(con, "dump_sha256", dump_hash)
    meta_set(con, "dump_line_count", total_lines)
    meta_set(con, "dump_function_count", len(rows))
    binary = get_binary(con, args)
    if binary and os.path.isfile(binary):
        target_hash = sha256_file(binary)
        meta_set(con, "target_sha256", target_hash)
        record_artifact(con, "target", binary, producer="PE file")
    record_artifact(
        con, "binary-ninja-hlil", dump, record_count=len(rows),
        producer="Binary Ninja HLIL linear-view export",
        version="legacy-unrecorded",
        settings={"format": "linear-hlil-text", "line_count": total_lines},
    )
    con.commit()
    print(f"indexed {len(rows)} functions across {total_lines} lines; "
          f"dump sha256={dump_hash}.")
    return 0


# --------------------------------------------------------------------------- #
# overlay loading
# --------------------------------------------------------------------------- #
def load_globals(con):
    # tuple: (new, kind, note, tier, disputed, dispute_reason)
    return {r[0]: (r[1], r[2], r[3], r[4], r[5], r[6]) for r in con.execute(
        "SELECT orig_name,new_name,kind,note,tier,disputed,dispute_reason FROM globals")}


def load_locals(con, func_addr):
    # tuple: (new, vtype, note, tier, disputed, dispute_reason)
    return {r[0]: (r[1], r[2], r[3], r[4], r[5], r[6]) for r in con.execute(
        "SELECT orig_name,new_name,vtype,note,tier,disputed,dispute_reason "
        "FROM locals WHERE func_addr=?", (func_addr,))}


def load_bindings(con, func_addr):
    return {r[0]: r[1] for r in con.execute(
        "SELECT var_name,type_name FROM bindings WHERE func_addr=? AND disputed=0",
        (func_addr,))}


def load_fields(con, type_name):
    # tuple: (new, ftype, note, tier, disputed, dispute_reason)
    return {r[0]: (r[1], r[2], r[3], r[4], r[5], r[6]) for r in con.execute(
        "SELECT offset,new_name,ftype,note,tier,disputed,dispute_reason "
        "FROM fields WHERE type_name=?", (type_name,))}


def load_line_notes(con, func_addr, func_name):
    """Notes attached to addresses inside this function -> {addr: [bodies]}."""
    out = {}
    like1 = func_name + "@%"
    for target, body in con.execute(
        "SELECT target,body FROM notes WHERE target LIKE ? OR target GLOB ?",
        (like1, "[0-9a-f][0-9a-f][0-9a-f][0-9a-f]*"),
    ):
        addr = None
        if "@" in target:
            addr = norm_addr(target.split("@", 1)[1])
        else:
            addr = norm_addr(target)
        if addr:
            out.setdefault(addr, []).append(body)
    return out


_TIER_RANK = {"verified": 0, "pipeline": 1, "mechanism": 2, "guess": 3,
              "unknown": 4, None: 5, "": 5}


def collect_related_notes(con, targets):
    """Symbol-level notes (notes table) whose target is any symbol this function
    touches: the function itself, the types bound here, the fields it accesses, and
    the globals/locals used. EXCLUDES @addr line notes (shown inline in the body).

    This is the anti-burial mechanism: a wrong/important note on a *field* or *global*
    (e.g. m_objDistance) now surfaces the moment you `view` any function that uses it,
    instead of hiding until someone runs `note list`.

    Returns list of (target, tier, body) sorted verified-first.
    """
    targets = sorted({t for t in targets if t})
    if not targets:
        return []
    rows = con.execute(
        "SELECT target, tier, body FROM notes WHERE target IN (%s) "
        "AND target NOT LIKE '%%@%%'" % ",".join("?" for _ in targets),
        targets).fetchall()
    rows.sort(key=lambda r: (_TIER_RANK.get(r[1], 5), r[0]))
    return rows


def render_notes_banner(rows, field_notes):
    """Loud, unmissable NOTES box for `view`. `rows` = [(target,tier,body)],
    `field_notes` = [(label, tier, body)]. Prints nothing if both empty."""
    items = [(t, tier, b) for (t, tier, b) in rows]
    items += [(lbl, tier, b) for (lbl, tier, b) in field_notes]
    if not items:
        return
    bar = "=" * 78
    disputed = sum(1 for _, tier, b in items
                   if tier in (None, "", "guess", "unknown")
                   or "CORRECTION" in (b or "") or "WRONG" in (b or "").upper())
    head = f">>> NOTES ({len(items)}) on this function & everything it touches - READ BEFORE TRUSTING THE CODE"
    print(head)
    if disputed:
        print(f"    ({disputed} low-confidence/correction note(s) below - verify, don't assume)")
    print("-" * 78)
    for target, tier, body in items:
        tag = f"[{tier or '?'}]"
        for j, seg in enumerate(textwrap.wrap(body or "", 92) or [""]):
            if j == 0:
                print(f"  * {tag} {target}:")
                print(f"      {seg}")
            else:
                print(f"      {seg}")
    print(bar)


# --------------------------------------------------------------------------- #
# function lookup
# --------------------------------------------------------------------------- #
def resolve_func(con, ident):
    """Resolve a function by original name, applied global name, or address."""
    row = con.execute(
        "SELECT addr,name,signature,start_byte,end_byte,start_line,end_line "
        "FROM functions WHERE name=?", (ident,)).fetchone()
    if row:
        return row
    # by applied (renamed) global name
    g = con.execute("SELECT orig_name FROM globals WHERE new_name=?", (ident,)).fetchone()
    if g:
        row = con.execute(
            "SELECT addr,name,signature,start_byte,end_byte,start_line,end_line "
            "FROM functions WHERE name=?", (g[0],)).fetchone()
        if row:
            return row
    # by address
    a = norm_addr(ident)
    if a:
        row = con.execute(
            "SELECT addr,name,signature,start_byte,end_byte,start_line,end_line "
            "FROM functions WHERE addr=?", (a,)).fetchone()
        if row:
            return row
    return None


NOTE_SYMBOL_TOKEN_RE = re.compile(r"^(?:sub|data)_[0-9a-fA-F]{4,}$")
NOTE_IDENT_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


def note_target_is_symbol_shaped(base):
    """True if `base` can only sensibly be a symbol reference, not a text search.

    `note list` doubles as a substring search over note targets, so a zero-result
    prose query is a legitimate answer. A mistyped SYMBOL is not: it is a question the
    tool cannot evaluate. Only three shapes qualify, deliberately narrowly, so word
    searches keep their current exit status:
      * a `sub_`/`data_` + hex token,
      * an address (0x-prefixed, or >=6 bare hex digits),
      * a bare identifier containing `_` (the shape of every applied name here).
    """
    if not base:
        return False
    if NOTE_SYMBOL_TOKEN_RE.match(base):
        return True
    a = norm_addr(base)
    if a and (base.strip().lower().startswith("0x") or len(base.strip()) >= 6):
        return True
    return bool(NOTE_IDENT_RE.match(base)) and "_" in base


def token_occurs_in_dump(dump, token):
    """True if `token` occurs in the dump as a WHOLE WORD.

    This is the ground truth for "can a rename keyed on this ever fire?", because
    apply_renames() rewrites identifier tokens found in the dump text and nothing else.

    It exists because both cheaper tests are wrong in opposite directions. Testing the
    `functions` table condemns all 33,193 data_ symbols, which are real dump tokens that
    a table of FUNCTIONS legitimately does not contain - that false positive would fire
    on 749 valid data_ renames. Testing the SHAPE of the token instead accepts anything
    that merely looks right, which silently passes 85 keys in this store that are
    well-formed and simply absent from the dump (78 data_, 7 sub_).

    Uses mmap.find rather than a regex: a regex scan of the 115MB dump costs ~1s on a
    miss, while find is a C-level substring search, and the word-boundary check only has
    to run at the few candidate offsets.
    """
    if not token or not dump or not os.path.exists(dump):
        return None  # unknown - caller must not treat this as "absent"
    needle = token.encode("utf-8")
    with open(dump, "rb") as f:
        mm = mmap.mmap(f.fileno(), 0, access=mmap.ACCESS_READ)
        try:
            pos = 0
            while True:
                idx = mm.find(needle, pos)
                if idx == -1:
                    return False
                before = mm[idx - 1:idx] if idx else b""
                after = mm[idx + len(needle):idx + len(needle) + 1]
                if not (before.isalnum() or before == b"_") and \
                   not (after.isalnum() or after == b"_"):
                    return True
                pos = idx + 1
        finally:
            mm.close()


def rename_key_can_fire(con, dump, key):
    """(can_fire, hint) for a `rename global` key.

    can_fire is True/False, or None when the dump is unavailable and the question
    cannot honestly be answered - in which case the caller stays silent rather than
    warning on a guess.
    """
    if con.execute("SELECT 1 FROM functions WHERE name=? LIMIT 1", (key,)).fetchone():
        return True, ""
    present = token_occurs_in_dump(dump, key)
    if present:
        return True, ""
    hint = ""
    a = norm_addr(key)
    if a:
        frow = con.execute("SELECT name FROM functions WHERE addr=?", (a,)).fetchone()
        if frow:
            hint = f" did you mean `{frow[0]}`?"
        else:
            for cand in (f"data_{a.lstrip('0') or '0'}", f"sub_{a.lstrip('0') or '0'}"):
                if token_occurs_in_dump(dump, cand):
                    hint = f" did you mean `{cand}`?"
                    break
    elif con.execute("SELECT 1 FROM globals WHERE new_name=? LIMIT 1", (key,)).fetchone():
        hint = (" that is an APPLIED name, not a dump token; renaming it "
                "chains a second rename instead of retargeting the first.")
    elif NOTE_SYMBOL_TOKEN_RE.match(key):
        hint = (" it is a well-formed token but it does NOT occur in the dump, so "
                "apply_renames can never match it.")
    return present, hint


def note_target_aliases(con, target):
    """Every spelling under which notes for `target` may have been filed.

    A note's target is whatever string its author typed - the original `sub_XXXXXX` /
    `data_XXXXXX` token, the applied friendly name, or a bare address - so matching
    only the spelling the CALLER typed reports "no notes" for research that plainly
    exists. That false negative fires exactly when someone is checking whether their
    work would duplicate prior art, which is the worst possible moment to be told
    there is none.

    This mirrors the orig->applied expansion `symbol_is_annotated` already performs
    for the debt ledger, but in BOTH directions and including address forms, so every
    subcommand that resolves a note target agrees with the ledger and with `view`.

    Returns (aliases, known, ambiguous):
      aliases   - spellings to search; always contains `target` itself first.
      known     - True when the overlay can actually identify this symbol (function
                  index or a global rename). False means the tool has never heard of
                  it and must SAY so rather than return an empty result.
      ambiguous - originals sharing an applied name we expanded through. `new_name`
                  carries no UNIQUE constraint and 97 names really do collide in the
                  live store, so callers report the collision instead of silently
                  picking one origin.
    """
    seen = set()
    aliases = []

    def push(x):
        if x and x not in seen:
            seen.add(x)
            aliases.append(x)

    base = (target or "").split("@")[0].strip()
    if not base:
        return [], False, []
    push(base)
    known = False
    ambiguous = []

    # original token -> applied name. Renames CHAIN in this store (a symbol renamed,
    # then its new name renamed again), so follow a bounded number of hops rather
    # than assuming one.
    frontier = [base]
    for _hop in range(4):
        nxt = []
        for name in frontier:
            for (new,) in con.execute(
                    "SELECT new_name FROM globals WHERE orig_name=?", (name,)):
                known = True
                if new not in seen:
                    push(new)
                    nxt.append(new)
        if not nxt:
            break
        frontier = nxt

    # applied name -> every original that maps to it. Expansion is deliberately
    # ASYMMETRIC: a shared label is ambiguous so we take all of its origins, but a
    # specific sub_/data_ token is not, so we never walk back down into a SIBLING
    # symbol that merely happens to carry the same label.
    for name in list(aliases):
        rows = con.execute(
            "SELECT orig_name FROM globals WHERE new_name=?", (name,)).fetchall()
        if rows:
            known = True
        if len(rows) > 1:
            for (orig,) in rows:
                if orig not in ambiguous:
                    ambiguous.append(orig)
        if name == base:
            for (orig,) in rows:
                push(orig)
    ambiguous.sort()

    # address <-> symbol, both ways, in every spelling notes are actually filed
    # under (the store holds targets like `8626d0` as well as `sub_8626d0`).
    def push_addr_forms(addr8):
        push(addr8)
        push("0x" + addr8)
        trimmed = addr8.lstrip("0") or "0"
        push(trimmed)
        push("0x" + trimmed)

    for name in list(aliases):
        row = con.execute("SELECT addr FROM functions WHERE name=?", (name,)).fetchone()
        if row:
            known = True
            push_addr_forms(row[0])
    a = norm_addr(base)
    if a:
        row = con.execute("SELECT name FROM functions WHERE addr=?", (a,)).fetchone()
        if row:
            known = True
            push(row[0])
            push_addr_forms(a)
            for (new,) in con.execute(
                    "SELECT new_name FROM globals WHERE orig_name=?", (row[0],)):
                push(new)
    if not known and con.execute(
            "SELECT 1 FROM functions WHERE name=? LIMIT 1", (base,)).fetchone():
        known = True
    return aliases, known, ambiguous


def note_rows_for_aliases(con, aliases, substring=None):
    """Notes filed under any alias, as an exact target or as an `alias@ADDR` line note.

    The `alias@...` arm is expressed as the half-open range [alias+'@', alias+'A') for
    the same reason `symbol_is_annotated` does it: '@' is 0x40 and 'A' is 0x41, so the
    range covers every line note and nothing else while still using the
    notes(target) index. A trailing LIKE arm preserves `note list`'s long-standing
    substring-over-targets behaviour for the caller's literal argument.
    """
    clauses, params = [], []
    if aliases:
        clauses.append("target IN (%s)" % ",".join("?" for _ in aliases))
        params.extend(aliases)
        for a in aliases:
            clauses.append("(target>=? AND target<?)")
            params.extend([a + "@", a + "A"])
    if substring is not None:
        clauses.append("target LIKE ?")
        params.append(f"%{substring}%")
    if not clauses:
        return []
    return list(con.execute(
        "SELECT id,target,body,author,tier FROM notes WHERE "
        + " OR ".join(clauses) + " ORDER BY target", params))


def read_func_lines(dump, start_byte, end_byte):
    with open(dump, "rb") as f:
        f.seek(start_byte)
        blob = f.read(end_byte - start_byte)
    return blob.decode("utf-8", "replace").splitlines()


# --------------------------------------------------------------------------- #
# view
# --------------------------------------------------------------------------- #
def build_rename_map(globals_map, locals_map):
    """Combined original-token -> new-name map (globals + this func's locals)."""
    rename = {}
    for orig, vals in globals_map.items():
        rename[orig] = vals[0]
    for orig, vals in locals_map.items():
        rename[orig] = vals[0]  # locals win over globals on collision
    return rename


def apply_renames(code, rename):
    if not rename:
        return code
    def repl(m):
        tok = m.group(0)
        return rename.get(tok, tok)
    return IDENT_RE.sub(repl, code)


def annotate_fields(code, bindings, fields_by_type, rename):
    """Return a list of 'var->field [tier]' annotations for fields on this line.

    Handles both decompiler shapes: displacement `var + 0xNN` (byte offset) and index
    `var[0xNN]` (dword index -> byte offset = idx*4). The index*4 assumption only ever
    *adds* an annotation when it lands exactly on a defined byte-offset field, so a
    non-dword array index simply misses rather than mislabels.
    """
    hits = []
    seen = set()

    def emit(var, off):
        tname = bindings.get(var)
        if not tname:
            return
        fmap = fields_by_type.get(tname)
        if not fmap:
            return
        fld = fmap.get(off)  # (new, ftype, note, tier, disputed, reason)
        if not fld:
            return
        key = (var, off)
        if key in seen:
            return
        seen.add(key)
        shown_var = rename.get(var, var)
        badge = TIER_BADGE.get(fld[3], "?")
        if fld[4]:  # disputed
            hits.append(f"{shown_var}->{fld[0]} [!DISPUTED: {fld[5] or 'see history'}]")
        else:
            hits.append(f"{shown_var}->{fld[0]} [{badge}]")

    for var, off in field_accesses(code):
        emit(var, off)
    return hits


def cmd_view(args, con):
    dump = get_dump(con, args)
    row = resolve_func(con, args.target)
    if not row:
        print(f"error: function not found: {args.target}", file=sys.stderr)
        print("  (run `index` first, or try `find`)", file=sys.stderr)
        return 1
    addr, name, sig, sb, eb, sl, el = row
    gate = investigation_gate(con, args, name)
    if gate is not None:
        return gate
    lines = read_func_lines(dump, sb, eb)

    # optional --lines A:B filter on dump line numbers
    lo_line, hi_line = None, None
    if getattr(args, "lines", None):
        parts = args.lines.split(":")
        lo_line = int(parts[0]) if parts[0] else None
        hi_line = int(parts[1]) if len(parts) > 1 and parts[1] else None
        # Warn early if the requested window does not overlap this function, so a
        # mistyped range doesn't look like an empty/broken function.
        if (lo_line is not None and lo_line > el) or (hi_line is not None and hi_line < sl):
            print(f"warning: --lines {args.lines} is outside {name} "
                  f"(dump lines {sl}-{el}); nothing to show.", file=sys.stderr)

    def in_range(i):
        n = sl + i  # dump line number of lines[i]
        if lo_line is not None and n < lo_line:
            return False
        if hi_line is not None and n > hi_line:
            return False
        return True

    # Only charge investigation debt when the view actually rendered something. An
    # out-of-range --lines window shows nothing, and you can't label what you never saw.
    any_shown = any(in_range(i) for i in range(len(lines)))

    # header / overlay summary
    globals_map = load_globals(con)
    g_self = globals_map.get(name)
    applied_name = g_self[0] if g_self else None

    if args.raw:
        if any_shown:
            warn_if_previously_investigated(con, name, stream=sys.stderr)
        print("\n".join(ln for i, ln in enumerate(lines) if in_range(i)))
        if any_shown:
            record_access(con, name, "viewed", get_author(args))
            con.commit()
            print_debt_footer(con, name, author=get_author(args))
        return 0

    locals_map = load_locals(con, addr)
    bindings = load_bindings(con, addr)
    types_needed = set(bindings.values())
    fields_by_type = {t: load_fields(con, t) for t in types_needed}
    rename = build_rename_map(globals_map, locals_map)
    line_notes = load_line_notes(con, addr, name)

    # --- anti-burial note collection -------------------------------------- #
    # Gather notes on EVERYTHING this function touches (itself, the types bound
    # here, their fields, and the globals/locals it references) so a note on a
    # field/global (e.g. the old m_objDistance "view-depth" claim) can no longer
    # hide until someone runs `note list`. Previously `view` only showed notes
    # whose target was the function name.
    body_text = "\n".join(ln for i, ln in enumerate(lines) if in_range(i))
    body_tokens = set(re.findall(r"[A-Za-z_]\w*", body_text))
    note_targets = {name, applied_name}
    note_targets |= set(types_needed)
    field_note_items = []  # (label, tier, body) from the fields.note column
    for tname, fmap in fields_by_type.items():
        for off, (fnew, ftype, fnote, ftier, fdisp, freason) in fmap.items():
            note_targets.add(fnew)
            if fnote:
                field_note_items.append((f"{tname}+0x{off:x} {fnew}", ftier, fnote))
    for orig, vals in {**globals_map, **locals_map}.items():
        if orig in body_tokens or (vals[0] and vals[0] in body_tokens):
            note_targets.add(orig)
            note_targets.add(vals[0])
    related_notes = collect_related_notes(con, note_targets)

    # Header banner
    print("=" * 78)
    title = name + (f"  ->  {applied_name}" if applied_name else "")
    tier_tag = f"  [{g_self[3]}]" if g_self else ""
    print(f"{title}   @ 0x{addr}   (lines {sl}-{el}){tier_tag}")
    if g_self and g_self[4]:
        print(f"  !! DISPUTED: {g_self[5] or 'see `revtool history`'}")
    if g_self and g_self[2]:
        print(f"  role: {g_self[2]}")
    if bindings:
        b = ", ".join(f"{v}:{t}" for v, t in sorted(bindings.items()))
        print(f"  bindings: {b}")
    # Surface any disputed symbol used in this function up front.
    disputed_used = []
    for orig, vals in {**{k: v for k, v in globals_map.items()},
                       **{k: v for k, v in locals_map.items()}}.items():
        if vals[4] and re.search(r"\b" + re.escape(orig) + r"\b", "\n".join(lines)):
            disputed_used.append((orig, vals[0], vals[5]))
    if disputed_used:
        print("  ---- DISPUTED symbols used here (do not trust): ----")
        for orig, new, reason in disputed_used:
            print(f"     {new} <- {orig}: {reason or 'see history'}")
    if any_shown:
        warn_if_previously_investigated(con, name)
    print("=" * 78)

    # Loud NOTES box: prior RE on this function and everything it references.
    render_notes_banner(related_notes, field_note_items)

    # Body
    used = {}
    for i, ln in enumerate(lines):
        if not in_range(i):
            continue
        m = re.match(r"^([0-9a-fA-F]{8})(\s+)(.*)$", ln)
        if not m:
            print(ln)
            continue
        laddr, ws, code = m.group(1).lower(), m.group(2), m.group(3)
        field_hits = annotate_fields(code, bindings, fields_by_type, rename)
        shown = apply_renames(code, rename) if not args.no_apply else code
        # track which renames were actually used (for legend)
        if not args.no_apply:
            for orig in rename:
                if re.search(r"\b" + re.escape(orig) + r"\b", code):
                    used[orig] = rename[orig]
        suffix = ""
        ann = []
        if field_hits:
            ann.extend(field_hits)
        if laddr in line_notes:
            ann.extend("NOTE: " + n for n in line_notes[laddr])
        if ann:
            suffix = "    // " + " | ".join(ann)
        print(f"{laddr}{ws}{shown}{suffix}")

    # Legend (with tier + dispute markers)
    if used and not args.no_legend:
        print("-" * 78)
        print("renamed in this view (new <- orig  [tier]):")
        for orig, new in sorted(used.items(), key=lambda kv: kv[1]):
            vals = globals_map.get(orig) or locals_map.get(orig)
            if vals and vals[4]:
                mark = f"[!DISPUTED: {vals[5] or 'see history'}]"
            else:
                mark = f"[{vals[3]}]" if vals else ""
            print(f"  {new} <- {orig}  {mark}")
    if not args.raw:
        print("-" * 78)
        print("(use --raw for the pristine original)")
    if any_shown:
        record_access(con, name, "viewed", get_author(args))
        con.commit()
        print_debt_footer(con, name, author=get_author(args))
    return 0


# --------------------------------------------------------------------------- #
# find
# --------------------------------------------------------------------------- #
def cmd_find(args, con):
    pat = args.pattern.lower()
    rows = con.execute(
        "SELECT f.addr,f.name,f.signature FROM functions f "
        "LEFT JOIN globals g ON g.orig_name=f.name "
        "WHERE lower(f.name) LIKE ? OR lower(f.signature) LIKE ? "
        "OR lower(IFNULL(g.new_name,'')) LIKE ? "
        "ORDER BY f.addr",
        (f"%{pat}%", f"%{pat}%", f"%{pat}%"),
    ).fetchall()
    if not rows:
        print("(no matches)")
        return 0
    gmap = load_globals(con)
    for addr, name, sig in rows[: args.limit]:
        applied = gmap.get(name)
        tag = f"  [{applied[0]}]" if applied else ""
        # Notes badge so RE'd functions advertise their notes at search time
        # (target = name, applied name, or any <name>@addr line note).
        ncount = con.execute(
            "SELECT COUNT(*) FROM notes WHERE target=? OR target=? OR target LIKE ?",
            (name, applied[0] if applied else name, name + "@%")).fetchone()[0]
        badge = f"  <{ncount} note{'s' if ncount != 1 else ''} - `view`/`note list {name}`>" if ncount else ""
        print(f"0x{addr}  {name}{tag}{badge}")
        print(f"    {sig.strip()}")
    if len(rows) > args.limit:
        print(f"... {len(rows) - args.limit} more (use --limit)")
    return 0


# --------------------------------------------------------------------------- #
# xref  (fast scan over the whole dump via mmap)
# --------------------------------------------------------------------------- #
def cmd_xref(args, con):
    dump = get_dump(con, args)
    require_dump(dump)
    gmap = load_globals(con)
    sym = args.symbol
    # Resolve an applied (renamed) name or a bare address back to the dump's ORIGINAL
    # token, so `xref <newname>` and `xref 0x401000` behave like `view` does. Shared
    # with the debt ledger so both agree on what a target denotes.
    scan_sym = resolve_scan_symbol(con, sym)
    gate = investigation_gate(con, args, scan_sym)
    if gate is not None:
        return gate
    # function start offsets, file order, for "which function" mapping
    frows = con.execute(
        "SELECT start_byte,addr,name FROM functions ORDER BY start_byte").fetchall()
    starts = [r[0] for r in frows]

    hits = []
    for line_start, line in iter_dump_symbol_hits(dump, scan_sym):
        fi = bisect.bisect_right(starts, line_start) - 1
        fname = frows[fi][2] if 0 <= fi < len(frows) else "?"
        laddr = line[:8].decode("ascii", "replace")
        text = line.decode("utf-8", "replace")
        # strip the leading "<addr><spaces>" so we don't print it twice
        tm = re.match(r"^[0-9a-fA-F]{8}\s+", text)
        text = text[tm.end():] if tm else text.strip()
        hits.append((laddr, fname, text))

    applied = gmap.get(scan_sym)
    label = scan_sym + (f" ({applied[0]})" if applied else "")
    if scan_sym != sym:
        label = f"{sym} -> {label}"
    print(f"xref {label}: {len(hits)} line(s)")
    # Can the overlay identify this target as something labelable? A canonical
    # sub_/data_/loc_/jump_table_ hit proves that; arbitrary text hits remain useful
    # xref results but are search probes, not rename debt. The scan above IS the dump
    # arm of that test, so its result is passed in rather than repeated - and the
    # ledger asks the same question through the same function.
    identified = symbol_is_identifiable(con, scan_sym, dump,
                                        found_in_dump=bool(hits), consult_dump=False)
    if not identified:
        if hits:
            print(f"note: {scan_sym} matched dump text but is not a labelable anonymous "
                  f"symbol; treating it as a research query, not rename debt.",
                  file=sys.stderr)
        else:
            # Zero hits for a symbol the overlay cannot identify means the scan never
            # had a valid symbol needle. That is not "nothing references this".
            print(f"warning: {scan_sym} is not a known symbol, so 0 lines is NOT "
                  f"evidence that it is unreferenced - check the spelling (`find`).",
                  file=sys.stderr)
    if identified:
        warn_if_previously_investigated(con, scan_sym)
    last_fn = None
    for laddr, fname, text in hits[: args.limit]:
        fn_new = gmap.get(fname)
        fn_disp = fname + (f"/{fn_new[0]}" if fn_new else "")
        if fname != last_fn:
            tag = ""
            if fname not in ("?", scan_sym):
                tag = "  [labeled]" if symbol_is_annotated(con, fname) else "  [NEEDS RE]"
            print(f"\n  in {fn_disp}:{tag}")
            last_fn = fname
        print(f"    {laddr}  {text}")
    if len(hits) > args.limit:
        print(f"\n... {len(hits) - args.limit} more (use --limit)")

    # Ledger: record this xref and surface the callers it revealed as discovered
    # work, so a caller never silently reads as "never RE'd". Unlabeled callers
    # land in `review --debt`; this xref itself is debt until `scan_sym` is labeled.
    #
    # BUT NOT FOR A TARGET THAT IS NOT A LABELABLE SYMBOL. Charging debt against a
    # text query creates a row that cannot be discharged by labeling the program -
    # and then the footer demands a name for an opcode, enum, path, or string key.
    # Keep the xref results and access history, but do not bill the probe.
    author = get_author(args)
    if not identified:
        print(f"\nno investigation debt charged: {scan_sym} is a search probe, not an "
              f"anonymous program symbol; there is nothing to label.",
              file=sys.stderr)
        return 0
    record_access(con, scan_sym, "xref", author)
    callers = []
    for _laddr, fname, _text in hits:
        if fname in ("?", scan_sym) or fname in callers:
            continue
        callers.append(fname)
    n_unlabeled = 0
    for fname in callers:
        if not symbol_is_annotated(con, fname):
            n_unlabeled += 1
            record_access(con, fname, "caller", author)
    con.commit()
    if callers:
        print(f"\ncallers: {len(callers)}  |  unlabeled (need RE): {n_unlabeled}")
    print_debt_footer(con, scan_sym, author=get_author(args))
    return 0


# --------------------------------------------------------------------------- #
# rename / type / field / bind / note
# --------------------------------------------------------------------------- #
def cmd_rename(args, con):
    author = get_author(args)
    tier = valid_tier(args.tier)
    ts = now_iso()
    basis = getattr(args, "basis", None)
    enforce_basis_tier(basis, tier)
    method = getattr(args, "method", None)
    blindspots = getattr(args, "blindspots", None)
    if args.scope == "global":
        orig, new = args.a, args.b
        # A global rename is keyed on the token as it appears IN THE DUMP, because
        # that is what apply_renames() rewrites. Address-shaped sub_ keys are also
        # consumed as semantic claims by dossiers/planners even when an analyzer
        # gave the dump entry a different token. Keep those claims, but distinguish
        # their ledger use from the text-overlay behavior of `view`.
        # `functions` indexes FUNCTIONS only, so a data_ token is legitimately absent
        # from it; testing membership alone would cry wolf on every data rename. The
        # test is therefore whether the key OCCURS IN THE DUMP, which is exactly what
        # apply_renames needs and is right for both kinds of symbol. Accepting the
        # token on SHAPE instead - the previous rule - stayed silent for 85 keys in
        # this store that are well-formed and absent from the dump.
        can_fire, hint = rename_key_can_fire(con, get_dump(con, args), orig)
        if can_fire is False:
            print(f"warning: {orig} does not occur in the dump. The claim will be "
                  f"stored for address-based dossiers/planners, but this key cannot "
                  f"rewrite dump text and will not appear in `view` output.{hint}",
                  file=sys.stderr)
        gate = depth_gate(con, args, get_dump(con, args), orig, tier)
        if gate is not None:
            return gate
        kind = "func" if orig.startswith("sub_") else ("data" if orig.startswith("data_") else None)
        prev = con.execute("SELECT new_name FROM globals WHERE orig_name=?", (orig,)).fetchone()
        con.execute(
            "INSERT INTO globals(orig_name,new_name,kind,note,tier,author,evidence,updated_at,"
            "basis,method,blindspots) VALUES(?,?,?,NULL,?,?,?,?,?,?,?) "
            "ON CONFLICT(orig_name) DO UPDATE SET new_name=excluded.new_name, "
            "tier=excluded.tier, author=excluded.author, evidence=excluded.evidence, "
            "updated_at=excluded.updated_at, basis=excluded.basis, method=excluded.method, "
            "blindspots=excluded.blindspots",
            (orig, new, kind, tier, author, args.evidence, ts, basis, method, blindspots))
        record_history(con, "global", f"global:{orig}",
                       "rename" if prev else "set",
                       prev[0] if prev else None, new, author)
        con.commit()
        print(f"global: {orig} -> {new}  [{tier}] by {author}")
    elif args.scope == "local":
        func = resolve_func(con, args.func)
        if not func:
            print(f"error: function not found: {args.func}", file=sys.stderr)
            return 1
        faddr = func[0]
        prev = con.execute(
            "SELECT new_name FROM locals WHERE func_addr=? AND orig_name=?",
            (faddr, args.a)).fetchone()
        con.execute(
            "INSERT INTO locals(func_addr,orig_name,new_name,tier,author,evidence,updated_at) "
            "VALUES(?,?,?,?,?,?,?) "
            "ON CONFLICT(func_addr,orig_name) DO UPDATE SET new_name=excluded.new_name, "
            "tier=excluded.tier, author=excluded.author, evidence=excluded.evidence, "
            "updated_at=excluded.updated_at",
            (faddr, args.a, args.b, tier, author, args.evidence, ts))
        record_history(con, "local", f"local:{faddr}:{args.a}",
                       "rename" if prev else "set",
                       prev[0] if prev else None, args.b, author)
        con.commit()
        print(f"local @ {func[1]} (0x{faddr}): {args.a} -> {args.b}  [{tier}] by {author}")
    return 0


def cmd_type(args, con):
    if args.action == "new":
        note = getattr(args, "note", None)
        author = get_author(args)
        tier = valid_tier(getattr(args, "tier", "guess"))
        basis = getattr(args, "basis", None)
        enforce_basis_tier(basis, tier)
        existed = con.execute("SELECT 1 FROM types WHERE name=?", (args.name,)).fetchone()
        con.execute(
            "INSERT OR IGNORE INTO types(name,note,tier,author,evidence,updated_at,basis) "
            "VALUES(?,?,?,?,?,?,?)",
            (args.name, note, tier, author, getattr(args, "evidence", None),
             now_iso(), basis))
        if note:  # also backfill if the type was auto-created earlier with NULL note
            con.execute("UPDATE types SET note=? WHERE name=? AND note IS NULL",
                        (note, args.name))
        if not existed:
            record_history(con, "type", f"type:{args.name}", "set", None,
                           args.name, author, note)
        con.commit()
        print(f"type: {args.name}  [{tier}] by {author}")
    elif args.action == "rename":
        old, new = args.name, getattr(args, "newname", None)
        if not old or not new:
            print("usage: type rename <oldName> <newName>", file=sys.stderr)
            return 1
        if not con.execute("SELECT 1 FROM types WHERE name=?", (old,)).fetchone():
            print(f"error: type not found: {old}", file=sys.stderr)
            return 1
        if con.execute("SELECT 1 FROM types WHERE name=?", (new,)).fetchone():
            print(f"error: target type already exists: {new}", file=sys.stderr)
            return 1
        author = get_author(args)
        # Rename the type itself + every structured reference to it. A pointer/array
        # C type like "OldName*" is rewritten token-wise so "OldNameFoo" is untouched.
        con.execute("UPDATE types SET name=? WHERE name=?", (new, old))
        con.execute("UPDATE fields SET type_name=? WHERE type_name=?", (new, old))
        con.execute("UPDATE bindings SET type_name=? WHERE type_name=?", (new, old))
        tok = re.compile(r"\b" + re.escape(old) + r"\b")
        ftx = 0
        for tname, off, ft in con.execute(
                "SELECT type_name,offset,ftype FROM fields WHERE ftype LIKE ?",
                (f"%{old}%",)).fetchall():
            newft = tok.sub(new, ft)
            if newft != ft:
                con.execute("UPDATE fields SET ftype=? WHERE type_name=? AND offset=?",
                            (newft, tname, off))
                ftx += 1
        record_history(con, "type", f"type:{old}", "rename", old, new, author)
        con.commit()
        print(f"type: {old} -> {new}  (ftype refs updated: {ftx})  by {author}")
    elif args.action == "list":
        for name, note in con.execute("SELECT name,note FROM types ORDER BY name"):
            nf = con.execute("SELECT COUNT(*) FROM fields WHERE type_name=?", (name,)).fetchone()[0]
            print(f"{name}  ({nf} fields){'  - ' + note if note else ''}")
    elif args.action == "show":
        rows = con.execute(
            "SELECT offset,new_name,ftype,note FROM fields WHERE type_name=? ORDER BY offset",
            (args.name,)).fetchall()
        if not rows:
            print(f"(type {args.name} has no fields)")
            return 0
        print(f"type {args.name}:")
        for off, nm, ft, note in rows:
            print(f"  +0x{off:x}  {nm}{'  : ' + ft if ft else ''}{'  // ' + note if note else ''}")
    return 0


def cmd_field(args, con):
    off = int(args.offset, 16)
    author = get_author(args)
    tier = valid_tier(args.tier)
    basis = getattr(args, "basis", None)
    enforce_basis_tier(basis, tier)
    method = getattr(args, "method", None)
    blindspots = getattr(args, "blindspots", None)
    con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (args.type,))
    prev = con.execute(
        "SELECT new_name FROM fields WHERE type_name=? AND offset=?", (args.type, off)).fetchone()
    con.execute(
        "INSERT INTO fields(type_name,offset,new_name,ftype,note,tier,author,evidence,updated_at,"
        "basis,method,blindspots) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) "
        "ON CONFLICT(type_name,offset) DO UPDATE SET new_name=excluded.new_name, "
        "ftype=excluded.ftype, note=excluded.note, tier=excluded.tier, "
        "author=excluded.author, evidence=excluded.evidence, updated_at=excluded.updated_at, "
        "basis=excluded.basis, method=excluded.method, blindspots=excluded.blindspots",
        (args.type, off, args.name, args.ftype, args.note, tier, author, args.evidence, now_iso(),
         basis, method, blindspots))
    record_history(con, "field", f"field:{args.type}:0x{off:x}",
                   "rename" if prev else "set", prev[0] if prev else None, args.name, author)
    con.commit()
    print(f"field: {args.type}+0x{off:x} = {args.name}  [{tier}] by {author}")
    return 0


def cmd_bind(args, con):
    # A binding names the pointee struct. The renderer and field resolver add the
    # pointer indirection themselves. Accepting `CIFWnd*` here silently creates a
    # distinct type literally named `CIFWnd*` and then models the local as
    # `CIFWnd**`, which is especially easy for a newcomer to miss in a long HLIL
    # view. Reject C declarators instead of storing a plausible-looking bad claim.
    if re.search(r"[*&\[\]]", args.type):
        print(
            f"error: bind expects a pointee type name, not a C declarator: {args.type!r}; "
            "pass the struct name without '*', '&', or array brackets",
            file=sys.stderr,
        )
        return 1
    func = resolve_func(con, args.func)
    if not func:
        print(f"error: function not found: {args.func}", file=sys.stderr)
        return 1
    faddr = func[0]
    author = get_author(args)
    tier = valid_tier(args.tier)
    con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (args.type,))
    con.execute(
        "INSERT INTO bindings(func_addr,var_name,type_name,tier,author,updated_at) "
        "VALUES(?,?,?,?,?,?) "
        "ON CONFLICT(func_addr,var_name) DO UPDATE SET type_name=excluded.type_name, "
        "tier=excluded.tier, author=excluded.author, updated_at=excluded.updated_at",
        (faddr, args.var, args.type, tier, author, now_iso()))
    record_history(con, "bind", f"bind:{faddr}:{args.var}", "set", None, args.type, author)
    con.commit()
    print(f"bind @ {func[1]} (0x{faddr}): {args.var} : {args.type}*  [{tier}] by {author}")
    return 0


def cmd_note(args, con):
    author = get_author(args)
    if args.action == "add":
        # Contradiction guard (color0x18c lesson): notes on the same symbol silently
        # accumulated into competing claims. Surface existing notes on the same base
        # symbol BEFORE the new one lands, so the author either reconciles or
        # explicitly supersedes them.
        base = (args.target or "").split("@")[0]
        existing = []
        if base:
            # Resolve the target to every spelling it may have been filed under. The
            # guard used to match only the spelling being written, so adding under
            # `sub_XXXXXX` gave NO heads-up about research already filed under the
            # applied name - the guard was blind precisely when it mattered.
            aliases, known, ambiguous = note_target_aliases(con, base)
            existing = [(nid, tgt, body) for nid, tgt, body, _a, _t
                        in note_rows_for_aliases(con, aliases)]
            if ambiguous:
                print(f"  warning: {base} is an AMBIGUOUS name shared by "
                      f"{', '.join(ambiguous)}; notes shown span all of them.",
                      file=sys.stderr)
            if not known and note_target_is_symbol_shaped(base):
                # Not an error: 603 live notes are attached to expressions rather
                # than symbols, so any target string stays legal. But a mistyped
                # symbol files research where nobody will ever search for it.
                print(f"  warning: {base} is not in the function index and has no "
                      f"rename - check the spelling, or this note may be filed where "
                      f"nobody will look.", file=sys.stderr)
        cur = con.execute(
            "INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
            (args.target, args.body, author, valid_tier(args.tier), now_iso()))
        new_id = cur.lastrowid
        record_history(con, "note", f"note:{new_id}", "set", None, args.body, author)
        # --supersedes: retire outdated claims VISIBLY instead of letting them coexist.
        superseded = []
        if getattr(args, "supersedes", None):
            for tok in str(args.supersedes).split(","):
                tok = tok.strip().lstrip("#")
                if not tok.isdigit():
                    continue
                oid = int(tok)
                old = con.execute("SELECT body FROM notes WHERE id=?", (oid,)).fetchone()
                if not old:
                    print(f"  warning: --supersedes {oid}: no such note", file=sys.stderr)
                    continue
                if not old[0].startswith("[SUPERSEDED"):
                    con.execute("UPDATE notes SET body=?, updated_at=? WHERE id=?",
                                (f"[SUPERSEDED by #{new_id}] " + old[0], now_iso(), oid))
                    record_history(con, "note", f"note:{oid}", "supersede",
                                   old[0], f"superseded by #{new_id}", author)
                superseded.append(oid)
        con.commit()
        print(f"note [{new_id}] on {args.target} by {author}")
        if superseded:
            print(f"  superseded note(s): {', '.join(f'#{i}' for i in superseded)}")
        live_others = [(nid, tgt, body) for nid, tgt, body in existing
                       if nid not in superseded and not body.startswith("[SUPERSEDED")]
        if live_others:
            print(f"  heads-up: {len(live_others)} other live note(s) on {base}* - reconcile or "
                  f"supersede if any now contradict:")
            for nid, tgt, body in live_others[:8]:
                print(f"    [{nid}] {tgt}: {body[:100]}{'...' if len(body) > 100 else ''}")
    elif args.action == "list":
        if not args.target:
            rows = list(con.execute(
                "SELECT id,target,body,author,tier FROM notes ORDER BY target"))
            aliases, known, ambiguous = [], True, []
        else:
            base = args.target.split("@")[0].strip()
            aliases, known, ambiguous = note_target_aliases(con, args.target)
            rows = note_rows_for_aliases(con, aliases, substring=args.target)
        # Note lines keep their exact original format on STDOUT, so anything parsing
        # this output is unaffected. Everything the fix adds goes to STDERR.
        for nid, target, body, auth, tier in rows:
            who = f" ({auth})" if auth else ""
            tg = f" [{tier}]" if tier else ""
            print(f"[{nid}]{tg} {target}{who}: {body}")
        if args.target:
            if ambiguous:
                print(f"warning: {base} is an AMBIGUOUS name shared by "
                      f"{', '.join(ambiguous)}; results span all of them.",
                      file=sys.stderr)
            if rows:
                # State the question that was actually evaluated. An answer whose
                # scope is invisible is how an empty result gets read as "no prior
                # art exists" when it only ever meant "not under that spelling".
                extra = [a for a in aliases if a != base]
                if extra:
                    print(f"note list {args.target}: {len(rows)} note(s) via "
                          f"{len(aliases)} spelling(s): {', '.join(aliases)}",
                          file=sys.stderr)
            elif known:
                # A real symbol that genuinely carries no notes. This is a FACT, and
                # a different one from "no such symbol" - the two must never read the
                # same way.
                print(f"note list: {base} is a KNOWN symbol with 0 notes "
                      f"(searched {len(aliases)} spelling(s): {', '.join(aliases)}).",
                      file=sys.stderr)
            elif note_target_is_symbol_shaped(base):
                print(f"error: no notes, and no such symbol: {args.target}",
                      file=sys.stderr)
                print("  nothing was searched beyond the literal string - this is NOT "
                      "evidence that the symbol has no notes.", file=sys.stderr)
                print("  (run `index` first, or try `find`)", file=sys.stderr)
                return 1
            else:
                print(f"note list: 0 notes whose TARGET matches {args.target!r} "
                      f"(note bodies are not searched).", file=sys.stderr)
    elif args.action == "rm":
        tok = str(args.target or "").strip().lstrip("#")
        if not tok.isdigit():
            print(f"error: note rm takes a numeric note id (got {args.target!r}); "
                  f"use `note list <symbol>` to find it.", file=sys.stderr)
            return 1
        nid = int(tok)
        row = con.execute("SELECT body FROM notes WHERE id=?", (nid,)).fetchone()
        if not row:
            # Previously this printed "removed note N" whether or not the note
            # existed - a confident report of work never done.
            print(f"error: no such note: {nid} (nothing was removed)", file=sys.stderr)
            return 1
        con.execute("DELETE FROM notes WHERE id=?", (nid,))
        # Keep the deleted body in the audit log; the history table exists so a prior
        # claim is never silently lost, and recording None lost it.
        record_history(con, "note", f"note:{nid}", "delete", row[0], None, author)
        con.commit()
        print(f"removed note {nid}")
    return 0


def cmd_seen(args, con):
    """One-shot triage: label an investigated symbol with a quick hypothesis.

    The frictionless way to discharge investigation debt when you are not yet sure
    what something is - a tier-`guess` note is enough to keep the next engineer
    from treating it as un-RE'd.
    """
    author = get_author(args)
    tier = valid_tier(args.tier)
    body = (args.body or "").strip()
    # Anti-garbage floor: an empty / near-empty triage is fake coverage, which is
    # worse than honest blank. Require a real one-liner.
    if len(body) < 8:
        print("error: write a real one-liner (>=8 chars) - what it does or your best guess.",
              file=sys.stderr)
        print('  e.g. revtool seen sub_4df5b0 "per-frame cam advance; raw delta, no title curve"',
              file=sys.stderr)
        return 1
    # `seen` writes a note, so a mistyped symbol files the triage where no lookup
    # will reach it AND leaves the real symbol still reading as un-RE'd.
    _al, sym_known, _amb = note_target_aliases(con, args.symbol)
    if not sym_known and note_target_is_symbol_shaped(args.symbol):
        print(f"warning: {args.symbol} is not in the function index and has no "
              f"rename - check the spelling, or this triage will not discharge the "
              f"debt you meant to clear.", file=sys.stderr)
    cur = con.execute(
        "INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
        (args.symbol, body, author, tier, now_iso()))
    record_history(con, "note", f"note:{cur.lastrowid}", "set", None, body, author, "triage")
    con.commit()
    print(f"triaged {args.symbol} [{tier}] -> note [{cur.lastrowid}] by {author}")
    remaining = compute_debt(con, author=author)
    print(f"investigation debt remaining (you): {len(remaining)}")
    return 0


def cmd_config(args, con):
    """Persist tool settings (strict enforcement, exploration budget) in the overlay db."""
    setting = args.setting
    if args.value is None:
        if setting == "strict":
            row = con.execute("SELECT value FROM meta WHERE key='strict'").fetchone()
            current = row[0] if row and row[0] is not None else "on (default)"
            print(f"strict = {current}")
        elif setting == "debt_limit":
            print(f"debt_limit = {debt_limit(con)}")
        elif setting == "binary":
            row = con.execute("SELECT value FROM meta WHERE key='binary'").fetchone()
            cur = row[0] if row and row[0] else f"{DEFAULT_BINARY} (default)"
            print(f"binary = {cur}  ({'present' if os.path.exists(cur.split(' (default')[0]) else 'MISSING'})")
        return 0
    if setting == "binary":
        path = os.path.abspath(args.value)
        if not os.path.exists(path):
            print(f"error: no such file: {path}", file=sys.stderr)
            return 1
        digest = sha256_file(path)
        if digest != TARGET_SHA256:
            print(f"error: PE hash {digest} is not the pinned SRO_Client.exe v1.150 "
                  f"hash {TARGET_SHA256}", file=sys.stderr)
            return 1
        con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('binary',?)", (path,))
        meta_set(con, "target_sha256", digest)
        record_artifact(con, "target", path, producer="PE file")
        con.commit()
        print(f"binary -> {path}  sha256={digest}")
        return 0
    if setting == "strict":
        val = "on" if args.value.strip().lower() in ("on", "1", "true", "yes") else "off"
        con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('strict',?)", (val,))
        con.commit()
        print(f"strict -> {val}")
    elif setting == "debt_limit":
        try:
            n = max(1, int(args.value))
        except ValueError:
            print("error: debt_limit must be a positive integer", file=sys.stderr)
            return 1
        con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('debt_limit',?)", (str(n),))
        con.commit()
        print(f"debt_limit -> {n}")
    return 0


def cmd_audit(args, con):
    """Scan a file (doc, source, anything) for sub_/data_ tokens that have no label.

    Catches the OTHER big way RE work gets lost: a writeup, a Ghidra/IDA session, or
    a code comment names a pile of functions that were never captured in the overlay.
    This surfaces every symbol mentioned in `path` that revtool still reads as un-RE'd
    so you can port the findings in. Exit code is the number of unlabeled symbols (so
    it can gate CI / pre-commit). `--quiet` prints only the unlabeled list.
    """
    try:
        text = open(args.path, encoding="utf-8", errors="replace").read()
    except OSError as e:
        print(f"error: cannot read {args.path}: {e}", file=sys.stderr)
        return 255
    toks = sorted({m.lower() for m in
                   re.findall(r"\b(?:sub|data)_[0-9a-fA-F]{4,}\b", text)})
    if not toks:
        if not args.quiet:
            print(f"no sub_/data_ symbols found in {args.path}")
        return 0
    unlabeled = []
    for t in toks:
        if not symbol_is_annotated(con, t):
            is_fn = con.execute("SELECT 1 FROM functions WHERE name=?", (t,)).fetchone()
            unlabeled.append((t, "func" if is_fn else "data"))
    if not args.quiet:
        print(f"{args.path}: {len(toks)} unique symbol(s), "
              f"{len(toks) - len(unlabeled)} labeled, {len(unlabeled)} UNLABELED")
    if unlabeled:
        if not args.quiet:
            print("--- unlabeled (capture these so the work isn't lost) ---")
        for sym, kind in unlabeled:
            print(f"  {sym:<16} ({kind})")
    return len(unlabeled)


def cmd_coverage(args, con):
    """Report how thoroughly ONE function is RE'd: % of referenced symbols labeled,
    plus structural depth (field offsets named, the function's own label/depth).

    "RE'd" here means the function's *direct* references are captured: every callee it
    invokes and every data global it touches has a label, the function itself is named,
    and its struct accesses are bound/named. Exit code = #unlabeled references (scriptable).
    """
    row = resolve_func(con, args.target)
    if not row:
        print(f"error: function not found: {args.target}", file=sys.stderr)
        return 255
    addr, name, sig, sb, eb, sl, el = row
    dump = get_dump(con, args)
    lines = read_func_lines(dump, sb, eb)
    body = "\n".join(lines)

    toks = set(GLOBAL_TOKEN_RE.findall(body))
    callees = sorted(t for t in toks if t.startswith("sub_") and t != name)
    datasyms = sorted(t for t in toks if t.startswith("data_"))
    refs = callees + datasyms
    unl_callees = [t for t in callees if not symbol_is_annotated(con, t)]
    unl_data = [t for t in datasyms if not symbol_is_annotated(con, t)]
    n_unl = len(unl_callees) + len(unl_data)
    sym_pct = 100.0 if not refs else 100.0 * (len(refs) - n_unl) / len(refs)
    # annotated-but-not-renamed: counts as "labeled" (discharges debt) yet the view still
    # shows the raw token, so it's not actually readable. Surface it separately.
    noteonly = [t for t in refs
                if t not in unl_callees and t not in unl_data
                and not symbol_is_renamed(con, t)]
    n_noteonly = len(noteonly)
    renamed = len(refs) - n_unl - n_noteonly
    read_pct = 100.0 if not refs else 100.0 * renamed / len(refs)

    # structural depth: distinct field offsets and how many are named via a binding here
    offsets = function_field_offsets(lines)
    bindings = load_bindings(con, addr)
    fields_by_type = {t: load_fields(con, t) for t in set(bindings.values())}
    named_off = 0
    unl_offsets = []
    for var, off in sorted(offsets):
        t = bindings.get(var)
        if t and fields_by_type.get(t, {}).get(off):
            named_off += 1
        else:
            unl_offsets.append((var, off, t))
    depth_pct = 100.0 if not offsets else 100.0 * named_off / len(offsets)

    self_named = symbol_is_annotated(con, name)
    self_deep = depth_satisfied(con, name, addr)
    structured = len(offsets) >= STRUCTURE_MIN_OFFSETS
    # a function with < STRUCTURE_MIN_OFFSETS field accesses has no depth obligation:
    # "flat", not "shallow". Don't read absence-of-obligation as a deficiency.
    if self_deep:
        depth_label = "deep"
    elif not structured:
        depth_label = "flat (no struct depth to capture)"
    else:
        depth_label = "SHALLOW (owes a breadcrumb)"
    applied = (load_globals(con).get(name) or [None])[0]

    disp = name + (f" -> {applied}" if applied else "")
    print(f"coverage of {disp} @ 0x{addr}")
    print(f"  self:        {'named' if self_named else 'UNNAMED'} / {depth_label}")
    print(f"  callees:     {len(callees) - len(unl_callees)}/{len(callees)} labeled")
    print(f"  data globals:{len(datasyms) - len(unl_data)}/{len(datasyms)} labeled")
    print(f"  field depth: {named_off}/{len(offsets)} offsets named ({depth_pct:.0f}%)")
    print(f"  SYMBOL COVERAGE: {sym_pct:.0f}%  ({len(refs) - n_unl}/{len(refs)} labeled)")
    print(f"  READABLE:        {read_pct:.0f}%  ({renamed}/{len(refs)} renamed; "
          f"{n_noteonly} note-only)")
    self_ok = self_named and (self_deep or not structured)
    all_named = (n_unl == 0) and self_ok and named_off == len(offsets)
    if all_named and n_noteonly == 0:
        verdict = "FULLY REd"
    elif all_named:
        verdict = f"covered, but {n_noteonly} ref(s) note-only -> raw token still in view"
    else:
        verdict = "incomplete"
    print(f"  VERDICT: {verdict}")
    if unl_callees:
        print("  unlabeled callees:")
        for t in unl_callees:
            print(f"    {t}")
    if unl_data:
        print("  unlabeled data:")
        for t in unl_data:
            print(f"    {t}")
    if noteonly:
        print("  labeled but NOT renamed (view still shows the raw token):")
        for t in noteonly:
            print(f"    {t}")
    if unl_offsets:
        print("  unnamed field offsets:")
        for var, off, t in unl_offsets:
            where = f"{var} : {t}" if t else f"{var} (unbound - bind it to a type)"
            print(f"    +0x{off:x}   {where}")
    return n_unl


# --------------------------------------------------------------------------- #
# confidence / dispute / history / review
# --------------------------------------------------------------------------- #
def cmd_mark(args, con):
    """Set the confidence tier of an existing claim."""
    author = get_author(args)
    tier = valid_tier(args.tier)
    claim = locate_claim(con, args.target)
    if not claim:
        print(f"error: claim not found: {args.target}", file=sys.stderr)
        print("  forms: global:NAME | local:FUNC:orig | field:TYPE:0xoff | bind:FUNC:var | note:ID | assert:ID",
              file=sys.stderr)
        return 1
    # Same depth-on-confidence gate as `rename`: promoting a function claim to a
    # confident tier must capture some structure (or pass --shallow).
    if claim["kind"] == "global":
        gate = depth_gate(con, args, get_dump(con, args), claim["label"].split()[0], tier)
        if gate is not None:
            return gate
    # Guardrail: don't let an absence/inferred claim be promoted to a top tier. Use the
    # stored basis, or one supplied on this command.
    basis = args.basis
    if basis is None and "basis" in {r[1] for r in con.execute(
            f"PRAGMA table_info({claim['table']})")}:
        brow = con.execute(
            f"SELECT basis FROM {claim['table']} WHERE {claim['where']}", claim["params"]).fetchone()
        basis = brow[0] if brow else None
    enforce_basis_tier(basis, tier)
    old = con.execute(
        f"SELECT tier FROM {claim['table']} WHERE {claim['where']}", claim["params"]).fetchone()
    sets = "tier=?, author=?, updated_at=?"
    vals = [tier, author, now_iso()]
    if args.evidence is not None:
        sets += ", evidence=?"
        vals.append(args.evidence)
    if args.basis is not None:
        sets += ", basis=?"
        vals.append(valid_basis(args.basis))
    con.execute(f"UPDATE {claim['table']} SET {sets} WHERE {claim['where']}",
                vals + claim["params"])
    record_history(con, claim["kind"], claim["ckey"], "tier",
                   old[0] if old else None, tier, author, args.evidence)
    con.commit()
    print(f"{claim['label']}: tier -> {tier} by {author}")
    return 0


def cmd_dispute(args, con):
    """Flag a claim as wrong/suspect WITHOUT deleting it (stays visible)."""
    author = get_author(args)
    claim = locate_claim(con, args.target)
    if not claim or claim["kind"] == "note":
        print(f"error: cannot dispute: {args.target} "
              f"(disputable: global/local/field/bind/assert)", file=sys.stderr)
        return 1
    con.execute(
        f"UPDATE {claim['table']} SET disputed=1, dispute_reason=?, author=?, updated_at=? "
        f"WHERE {claim['where']}",
        [args.reason, author, now_iso()] + claim["params"])
    record_history(con, claim["kind"], claim["ckey"], "dispute", None, None, author, args.reason)
    con.commit()
    print(f"!! DISPUTED {claim['label']}: {args.reason} (by {author})")
    print("   (still visible in `view`; use `resolve` once corrected)")
    return 0


def cmd_resolve(args, con):
    """Clear a dispute, optionally re-tiering the claim."""
    author = get_author(args)
    claim = locate_claim(con, args.target)
    if not claim:
        print(f"error: claim not found: {args.target}", file=sys.stderr)
        return 1
    sets = "disputed=0, dispute_reason=NULL, author=?, updated_at=?"
    vals = [author, now_iso()]
    if args.tier:
        sets += ", tier=?"
        vals.append(valid_tier(args.tier))
    con.execute(f"UPDATE {claim['table']} SET {sets} WHERE {claim['where']}",
                vals + claim["params"])
    record_history(con, claim["kind"], claim["ckey"], "resolve", None, args.tier, author)
    con.commit()
    print(f"resolved {claim['label']}" + (f" -> [{args.tier}]" if args.tier else ""))
    return 0


def cmd_history(args, con):
    """Show the audit trail for a claim (or recent global activity)."""
    if args.target:
        claim = locate_claim(con, args.target)
        ckey = claim["ckey"] if claim else args.target
        rows = con.execute(
            "SELECT ts,author,action,old,new,reason FROM history WHERE ckey=? ORDER BY id",
            (ckey,)).fetchall()
        print(f"history for {ckey}:")
        if not rows and claim is None and note_target_is_symbol_shaped(args.target):
            # The ckey fallback means an unresolvable target queries a key that cannot
            # exist, and "(none)" then reads as "this claim has no history". Those are
            # different facts, and this is the audit trail - the one place where a
            # silently empty answer is least acceptable.
            print(f"error: no claim matches {args.target!r} - this is an empty LOOKUP, "
                  f"not an empty history.", file=sys.stderr)
            print("  (use the sub_/data_ token, the applied name, an address, or an "
                  "explicit form: global:NAME | note:ID)", file=sys.stderr)
            return 1
    else:
        rows = con.execute(
            "SELECT ts,author,action,old,new,reason FROM history ORDER BY id DESC LIMIT ?",
            (args.limit,)).fetchall()
        rows = list(reversed(rows))
        print(f"recent activity (last {args.limit}):")
    if not rows:
        print("  (none)")
        return 0
    for ts, author, action, old, new, reason in rows:
        change = ""
        if old is not None or new is not None:
            change = f"  {old!r} -> {new!r}"
        rs = f"  // {reason}" if reason else ""
        print(f"  {ts}  {author or '?':<12} {action}{change}{rs}")
    return 0


def cmd_review(args, con):
    """List claims needing attention: low tier, disputed, by author, or needs-dynamic."""
    where_extra = ""
    if getattr(args, "debt", False):
        debt = compute_debt(con)
        print("=== INVESTIGATION DEBT (investigated via view/xref, not yet labeled) ===")
        if not debt:
            print("  (none - every investigated symbol carries at least a hypothesis)")
        for sym, kind, count, last_ts, author in debt:
            print(f"  {sym:<26} {kind:<6} x{count:<3} last {last_ts or '?'} by {author or '?'}")
        # Callers an xref revealed that nobody has reviewed yet (non-blocking backlog).
        # `debt` can be large; membership-test it as a set, not a per-caller linear scan
        # (this loop runs once per caller row - tens of thousands on a mature DB).
        debt_syms = {d[0] for d in debt}
        pending = []
        for sym, count in con.execute(
                "SELECT symbol,count FROM access_log WHERE kind='caller' ORDER BY last_ts"):
            if sym not in debt_syms and not symbol_is_annotated(con, sym):
                pending.append((sym, count))
        if pending:
            print("\n=== CALLERS revealed by xref, not yet reviewed ===")
            for sym, count in pending[:40]:
                print(f"  {sym:<26} seen x{count}")
            if len(pending) > 40:
                print(f"  ... {len(pending) - 40} more")
        return 0
    if getattr(args, "needs_dynamic", False):
        # The standing "things that might be lying to us" report: high-tier claims that
        # rest on absence/inference and were NEVER confirmed at runtime. The data_f0673f
        # miss would have been the headline here.
        print("=== high-confidence claims NOT confirmed dynamically (review these) ===")
        any_hit = False
        # Assertions: open/refuted, or weak-basis at high tier.
        for aid, subj, claim, basis, tier, status, blind in con.execute(
            "SELECT id,subject,claim,basis,tier,status,blindspots FROM assertions "
            "WHERE status!='confirmed' ORDER BY status DESC, id"):
            any_hit = True
            bs = f"  blindspots: {blind}" if blind else ""
            print(f"  assert #{aid} [{tier}/{basis}/{status}] {subj}: {claim}{bs}")
        # Renames/fields recorded with a weak basis at a high tier.
        for kind, table, keyexpr in (
            ("global", "globals", "'global:'||orig_name"),
            ("field", "fields", "'field:'||type_name||':0x'||printf('%x',offset)"),
        ):
            for ck, new, basis, tier in con.execute(
                f"SELECT {keyexpr},new_name,basis,tier FROM {table} "
                f"WHERE basis IN ('absence','inferred') "
                f"AND tier IN ('pipeline','verified')"):
                any_hit = True
                print(f"  {ck} (= {new}) [{tier}/{basis}] -- weak basis at high tier")
        if not any_hit:
            print("  (none - every high-confidence claim is observed or dynamically confirmed)")
        return 0
    if args.disputed:
        print("=== DISPUTED claims ===")
        rows = []
        for kind, table, keyexpr in (
            ("global", "globals", "'global:'||orig_name"),
            ("local", "locals", "'local:'||func_addr||':'||orig_name"),
            ("field", "fields", "'field:'||type_name||':0x'||printf('%x',offset)"),
        ):
            for ck, new, reason, auth in con.execute(
                f"SELECT {keyexpr},new_name,dispute_reason,author FROM {table} WHERE disputed=1"):
                print(f"  {ck}  (= {new}) by {auth or '?'}: {reason or 'see history'}")
                rows.append(ck)
        for aid, subj, claim, reason, auth in con.execute(
                "SELECT id,subject,claim,dispute_reason,author FROM assertions WHERE disputed=1"):
            print(f"  assert:{aid}  ({subj}: {claim}) by {auth or '?'}: {reason or 'see history'}")
            rows.append(f"assert:{aid}")
        if not rows:
            print("  (none)")
        return 0

    tier = args.tier or "guess"
    valid_tier(tier)
    print(f"=== claims at tier '{tier}'" +
          (f" by {args.by_author}" if args.by_author else "") + " ===")
    auth_clause = " AND author=?" if args.by_author else ""
    auth_p = [args.by_author] if args.by_author else []
    any_hit = False
    for kind, table, keyexpr in (
        ("global", "globals", "'global:'||orig_name"),
        ("local", "locals", "'local:'||func_addr||':'||orig_name"),
        ("field", "fields", "'field:'||type_name||':0x'||printf('%x',offset)"),
    ):
        for ck, new, auth, upd in con.execute(
            f"SELECT {keyexpr},new_name,author,updated_at FROM {table} "
            f"WHERE tier=?{auth_clause} ORDER BY updated_at", [tier] + auth_p):
            print(f"  {ck}  (= {new}) by {auth or '?'}  {upd or ''}")
            any_hit = True
    if not any_hit:
        print("  (none)")
    return 0


# --------------------------------------------------------------------------- #
# assertions / observations  (the static<->dynamic reconciliation loop)
# --------------------------------------------------------------------------- #
def reconcile_assertions(con, subject, value, source, author):
    """Check every assertion on `subject` against an observed `value`.

    Confirms or REFUTES each machine-checkable assertion; a refuted assertion is also
    auto-disputed so it screams in `view`/`review`. Returns a list of result dicts.
    """
    results = []
    rows = con.execute(
        "SELECT id,claim,predicate,basis,status FROM assertions WHERE subject=?",
        (subject,)).fetchall()
    for aid, claim, predicate, basis, status in rows:
        matches, detail = eval_predicate(predicate, value)
        if matches is None:
            results.append(dict(id=aid, verdict="needs-human", detail=detail, claim=claim))
            con.execute(
                "UPDATE assertions SET observed=?, source=?, updated_at=? WHERE id=?",
                (str(value), source, now_iso(), aid))
            continue
        new_status = "confirmed" if matches else "refuted"
        if matches:
            con.execute(
                "UPDATE assertions SET status=?, observed=?, source=?, basis='dynamic', "
                "updated_at=? WHERE id=?",
                (new_status, str(value), source, now_iso(), aid))
            record_history(con, "assert", f"assert:{aid}", "resolve", status, "confirmed",
                           author, f"observed {subject}={value}: {detail}")
        else:
            reason = f"runtime {subject}={value} contradicts predicate {predicate!r} ({detail})"
            con.execute(
                "UPDATE assertions SET status='refuted', observed=?, source=?, disputed=1, "
                "dispute_reason=?, updated_at=? WHERE id=?",
                (str(value), source, reason, now_iso(), aid))
            record_history(con, "assert", f"assert:{aid}", "dispute", status, "refuted",
                           author, reason)
        results.append(dict(id=aid, verdict=new_status, detail=detail, claim=claim))
    return results


def cmd_assert(args, con):
    author = get_author(args)
    if args.action == "add":
        tier = valid_tier(args.tier)
        basis = valid_basis(args.basis)
        enforce_basis_tier(basis, tier)
        if basis == "absence" and not args.blindspots:
            print("warning: absence claims should record --blindspots "
                  "(what your search method cannot see), e.g. 'base+disp, computed-ptr, dynamic'.",
                  file=sys.stderr)
        cur = con.execute(
            "INSERT INTO assertions(subject,claim,predicate,basis,method,blindspots,tier,"
            "expected,author,evidence,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
            (args.subject, args.claim, args.predicate, basis, args.method, args.blindspots,
             tier, args.expected, author, args.evidence, now_iso()))
        aid = cur.lastrowid
        record_history(con, "assert", f"assert:{aid}", "set", None, args.claim, author,
                       f"basis={basis} predicate={args.predicate}")
        con.commit()
        flag = "" if basis not in STATIC_WEAK_BASES else "  (NEEDS DYNAMIC CONFIRMATION)"
        print(f"assert [{aid}] {args.subject}: {args.claim}  "
              f"[{tier}/{basis}]{flag} by {author}")
        return 0
    if args.action == "list":
        q = ("SELECT id,subject,claim,predicate,basis,tier,status,observed,disputed,"
             "dispute_reason FROM assertions")
        clauses, params = [], []
        if args.subject:
            clauses.append("subject LIKE ?")
            params.append(f"%{args.subject}%")
        if args.status:
            clauses.append("status=?")
            params.append(args.status)
        if clauses:
            q += " WHERE " + " AND ".join(clauses)
        q += " ORDER BY status DESC, id"
        any_hit = False
        for aid, subj, claim, pred, basis, tier, status, observed, disp, reason in \
                con.execute(q, params):
            any_hit = True
            mark = {"open": "[ ]", "confirmed": "[OK]", "refuted": "[XX]"}.get(status, "[?]")
            obs = f"  observed={observed}" if observed is not None else ""
            dr = f"  !! {reason}" if disp else ""
            print(f"{mark} #{aid} {subj}  ({pred or 'no-pred'}) "
                  f"[{tier}/{basis}]{obs}{dr}")
            print(f"      {claim}")
        if not any_hit:
            print("(no assertions)")
        return 0
    return 0


def cmd_observe(args, con):
    """Record a single runtime observation and reconcile it against assertions."""
    author = get_author(args)
    con.execute(
        "INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
        (args.subject, args.value, args.source, author, now_iso()))
    results = reconcile_assertions(con, args.subject, args.value, args.source, author)
    con.commit()
    print(f"observed {args.subject} = {args.value}"
          + (f"  (source: {args.source})" if args.source else ""))
    if not results:
        print("  (no assertions on this subject)")
    for r in results:
        tag = {"confirmed": "CONFIRMED", "refuted": "REFUTED !!", "needs-human": "needs review"}\
            .get(r["verdict"], r["verdict"])
        print(f"  assert #{r['id']}: {tag}  ({r['detail']})  -- {r['claim']}")
    return 0


# Parsers for native-trace log lines -> (subject, value) observations.
_TRACE_GLOBAL_RE = re.compile(r"(data_[0-9a-fA-F]+)\([^)]*\)=(-?[0-9]+(?:\.[0-9]+)?)")
_TRACE_FOGRS_RE = re.compile(
    r"FOG_RS\s+(\w+)\s+state=0x[0-9A-Fa-f]+\s+value=0x[0-9A-Fa-f]+\s+int=(-?\d+)\s+float=(\S+)")


def cmd_ingest(args, con):
    """Bulk-observe from a native-trace log, reconciling against assertions.

    Parses two line shapes:
      WORLD_RENDER_PASS ... data_f0673f(near)=1692.86[...] data_f06743(far)=2500[...]
      FOG_RS FOGSTART state=0x24 value=0x.. int=.. float=1692.86 caller=..
    Distinct (subject,value) pairs become observations; each reconciles assertions.
    """
    if not os.path.exists(args.log):
        print(f"error: log not found: {args.log}", file=sys.stderr)
        return 1
    author = get_author(args)
    seen = {}  # subject -> set of distinct values (keep first line for source)
    src_line = {}
    with open(args.log, encoding="utf-8", errors="replace") as f:
        for ln_no, line in enumerate(f, 1):
            for m in _TRACE_GLOBAL_RE.finditer(line):
                subj, val = m.group(1), m.group(2)
                seen.setdefault(subj, set()).add(val)
                src_line.setdefault((subj, val), f"{os.path.basename(args.log)}:{ln_no}")
            fm = _TRACE_FOGRS_RE.search(line)
            if fm:
                state, fval = fm.group(1), fm.group(3)
                if fval.lower() in ("-nan", "nan", "inf", "-inf"):
                    continue  # packed-color states are not floats; skip the bogus reinterpret
                subj = f"D3DRS_{state}"
                seen.setdefault(subj, set()).add(fval)
                src_line.setdefault((subj, fval), f"{os.path.basename(args.log)}:{ln_no}")

    n_obs = n_conf = n_ref = 0
    for subj, vals in sorted(seen.items()):
        for val in sorted(vals, key=lambda v: float(v) if _isnum(v) else 0.0):
            source = src_line.get((subj, val), os.path.basename(args.log))
            con.execute(
                "INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
                (subj, val, source, author, now_iso()))
            n_obs += 1
            for r in reconcile_assertions(con, subj, val, source, author):
                if r["verdict"] == "confirmed":
                    n_conf += 1
                elif r["verdict"] == "refuted":
                    n_ref += 1
                    print(f"  REFUTED assert #{r['id']} by {subj}={val}: {r['claim']}")
    con.commit()
    distinct = sum(len(v) for v in seen.values())
    print(f"ingest {args.log}: {len(seen)} subjects, {distinct} distinct values, "
          f"{n_obs} observations.")
    print(f"  assertions: {n_conf} confirmed, {n_ref} refuted.")
    if n_ref:
        print("  -> refuted assertions are now DISPUTED; run `revtool review --disputed`.")
    return 0


def _isnum(s):
    try:
        float(s)
        return True
    except (TypeError, ValueError):
        return False


# --------------------------------------------------------------------------- #
# sync  (git-mergeable JSONL export/import for multiple engineers)
# --------------------------------------------------------------------------- #
SYNC_TABLES = {
    "globals": ["orig_name", "new_name", "kind", "note", "tier", "author",
                "evidence", "updated_at", "disputed", "dispute_reason",
                "basis", "method", "blindspots"],
    "locals": ["func_addr", "orig_name", "new_name", "vtype", "note", "tier",
               "author", "evidence", "updated_at", "disputed", "dispute_reason",
               "basis", "method", "blindspots"],
    "types": ["name", "note", "tier", "author", "evidence", "updated_at",
              "disputed", "dispute_reason", "basis", "method", "blindspots"],
    "fields": ["type_name", "offset", "new_name", "ftype", "note", "tier",
               "author", "evidence", "updated_at", "disputed", "dispute_reason",
               "basis", "method", "blindspots"],
    "bindings": ["func_addr", "var_name", "type_name", "tier", "author", "updated_at"],
    "notes": ["target", "body", "author", "tier", "updated_at"],
    "assertions": ["subject", "claim", "predicate", "basis", "method", "blindspots",
                   "tier", "status", "expected", "observed", "source", "author",
                   "evidence", "updated_at", "disputed", "dispute_reason"],
    "observations": ["subject", "value", "source", "author", "ts"],
}
SYNC_KEYS = {
    "globals": ["orig_name"],
    "locals": ["func_addr", "orig_name"],
    "types": ["name"],
    "fields": ["type_name", "offset"],
    "bindings": ["func_addr", "var_name"],
    "notes": ["target", "body"],
    "assertions": ["subject", "claim"],
    "observations": ["subject", "value", "source", "author", "ts"],
}


def default_sync_path(db_path):
    """Keep each overlay's collaboration ledger beside and named after it."""
    root, _ = os.path.splitext(os.path.abspath(db_path))
    return root + "_symbols.jsonl"


def claim_key(table, rec):
    return tuple(rec.get(k) for k in SYNC_KEYS[table])


def read_ledger(path):
    meta = None
    claims = {}
    events = {}
    if not os.path.exists(path):
        return meta, claims, events
    with open(path, encoding="utf-8") as f:
        for line_no, ln in enumerate(f, 1):
            if not ln.strip():
                continue
            try:
                rec = json.loads(ln)
            except json.JSONDecodeError as exc:
                raise ValueError(f"{path}:{line_no}: invalid JSON: {exc}") from exc
            table = rec.get("_t")
            if table == "_meta":
                if meta is not None:
                    raise ValueError(f"{path}:{line_no}: duplicate ledger metadata")
                meta = rec
            elif table == "_event":
                event_id = rec.get("event_id")
                if not event_id:
                    raise ValueError(f"{path}:{line_no}: event has no event_id")
                events[event_id] = rec
            elif table in SYNC_TABLES:
                # v1 did not serialize provenance columns for `types`.
                # Rehydrate the database defaults so upgrading a legacy ledger
                # does not manufacture 17 false conflicts (NULL versus the
                # migrated defaults `guess` / 0).
                if "tier" not in rec:
                    rec["tier"] = "guess"
                if "disputed" in SYNC_TABLES[table] and "disputed" not in rec:
                    rec["disputed"] = 0
                for column in SYNC_TABLES[table]:
                    rec.setdefault(column, None)
                key = (table, claim_key(table, rec))
                if key in claims:
                    # Legacy ledgers occasionally contain the same serialized
                    # row twice.  That carries no competing information and is
                    # safe to collapse.  A second value for the same semantic
                    # key is still an error: choosing one would be an implicit
                    # last-writer-wins merge.
                    if stable_json(claims[key]) == stable_json(rec):
                        continue
                    raise ValueError(f"{path}:{line_no}: conflicting duplicate claim {key}")
                claims[key] = rec
    return meta, claims, events


def db_claims(con):
    result = {}
    for table, cols in SYNC_TABLES.items():
        for row in con.execute(f"SELECT {','.join(cols)} FROM {table}"):
            rec = {"_t": table}
            rec.update(dict(zip(cols, row)))
            result[(table, claim_key(table, rec))] = rec
    return result


def db_events(con):
    cols = ["event_id", "ts", "author", "kind", "ckey", "action", "old",
            "new", "reason", "target_sha256", "dump_sha256", "tool_version"]
    result = {}
    for row in con.execute(f"SELECT {','.join(cols)} FROM history WHERE event_id IS NOT NULL"):
        rec = {"_t": "_event"}
        rec.update(dict(zip(cols, row)))
        result[rec["event_id"]] = rec
    return result


def ledger_meta(con):
    return {
        "_t": "_meta",
        "schema": LEDGER_SCHEMA,
        "target_sha256": meta_get(con, "target_sha256") or TARGET_SHA256,
        "dump_sha256": meta_get(con, "dump_sha256"),
        "revtool_schema_version": REVTOOL_SCHEMA_VERSION,
        "projection": "claims-plus-append-only-events",
    }


def sync_diff(con, path):
    remote_meta, remote, remote_events = read_ledger(path)
    local = db_claims(con)
    local_events = db_events(con)
    local_only = sorted(set(local) - set(remote), key=str)
    remote_only = sorted(set(remote) - set(local), key=str)
    differing = sorted(
        (key for key in set(local) & set(remote)
         if stable_json(local[key]) != stable_json(remote[key])), key=str)
    return {
        "meta": remote_meta,
        "local": local,
        "remote": remote,
        "local_events": local_events,
        "remote_events": remote_events,
        "local_only": local_only,
        "remote_only": remote_only,
        "differing": differing,
        "local_event_only": sorted(set(local_events) - set(remote_events)),
        "remote_event_only": sorted(set(remote_events) - set(local_events)),
    }


def validate_ledger_meta(con, meta, path):
    if meta is None:
        return  # legacy v1 snapshot; next export upgrades it.
    if meta.get("schema") != LEDGER_SCHEMA:
        raise ValueError(f"{path}: unsupported ledger schema {meta.get('schema')!r}")
    target = meta.get("target_sha256")
    local_target = meta_get(con, "target_sha256") or TARGET_SHA256
    if target != local_target:
        raise ValueError(f"{path}: ledger target hash differs from this projection")
    dump_hash = meta.get("dump_sha256")
    local_dump = meta_get(con, "dump_sha256")
    if dump_hash and local_dump and dump_hash != local_dump:
        raise ValueError(f"{path}: ledger Binary Ninja dump hash differs from this projection")


def cmd_sync(args, con):
    if args.action in ("status", "conflicts"):
        path = args.path or default_sync_path(args.db)
        diff = sync_diff(con, path)
        unresolved = con.execute(
            "SELECT id,table_name,claim_key,local_json,remote_json,detected_at "
            "FROM sync_conflicts WHERE resolved=0 ORDER BY id").fetchall()
        print(f"ledger: {path}  ({'present' if os.path.exists(path) else 'MISSING'})")
        print(f"claims: local-only={len(diff['local_only'])} "
              f"remote-only={len(diff['remote_only'])} differing={len(diff['differing'])}")
        print(f"events: local-only={len(diff['local_event_only'])} "
              f"remote-only={len(diff['remote_event_only'])}")
        print(f"unresolved conflicts: {len(unresolved)}")
        if args.action == "conflicts":
            for row in unresolved:
                print(f"  #{row[0]} {row[1]} {row[2]} detected={row[5]}")
                print(f"     local : {row[3]}")
                print(f"     remote: {row[4]}")
        return 1 if unresolved or diff["remote_only"] or diff["differing"] else 0

    if args.action == "resolve":
        if not args.path or not str(args.path).isdigit() or args.take not in ("local", "remote"):
            print("usage: revtool sync resolve <conflict-id> --take local|remote",
                  file=sys.stderr)
            return 1
        conflict_id = int(args.path)
        row = con.execute(
            "SELECT table_name,local_json,remote_json FROM sync_conflicts "
            "WHERE id=? AND resolved=0", (conflict_id,)).fetchone()
        if not row:
            print(f"error: unresolved conflict #{conflict_id} not found", file=sys.stderr)
            return 1
        table, local_json, remote_json = row
        if args.take == "remote":
            rec = json.loads(remote_json)
            keys = SYNC_KEYS[table]
            where = " AND ".join(f"{k}=?" for k in keys)
            kv = [rec.get(k) for k in keys]
            cols = [c for c in SYNC_TABLES[table] if c in rec]
            con.execute(f"UPDATE {table} SET {','.join(f'{c}=?' for c in cols)} "
                        f"WHERE {where}", [rec[c] for c in cols] + kv)
        con.execute("UPDATE sync_conflicts SET resolved=1 WHERE id=?", (conflict_id,))
        record_history(con, "sync", f"sync-conflict:{conflict_id}",
                       f"resolve-{args.take}", local_json, remote_json,
                       get_author(args))
        con.commit()
        print(f"resolved conflict #{conflict_id}: kept {args.take}")
        return 0

    if args.action == "export":
        path = args.path or default_sync_path(args.db)
        diff = sync_diff(con, path)
        validate_ledger_meta(con, diff["meta"], path)
        # Export may add local work, but it may never erase or silently rewrite
        # teammate work. Import/resolve first when the existing ledger differs.
        unresolved_differences = []
        for key in diff["differing"]:
            table = key[0]
            local_json = stable_json(diff["local"][key])
            remote_json = stable_json(diff["remote"][key])
            claim_json = stable_json(key[1])
            accepted = con.execute(
                "SELECT 1 FROM sync_conflicts WHERE table_name=? AND claim_key=? "
                "AND local_json=? AND remote_json=? AND resolved=1",
                (table, claim_json, local_json, remote_json)).fetchone()
            if not accepted:
                unresolved_differences.append(key)
        if diff["remote_only"] or unresolved_differences or diff["remote_event_only"]:
            print("error: tracked ledger contains work not present identically in this DB; "
                  "run `sync import` and resolve conflicts before export.", file=sys.stderr)
            print(f"  remote-only claims={len(diff['remote_only'])} "
                  f"differing claims={len(unresolved_differences)} "
                  f"remote-only events={len(diff['remote_event_only'])}", file=sys.stderr)
            return 2
        records = [ledger_meta(con)]
        records.extend(diff["local"][key] for key in sorted(diff["local"], key=str))
        records.extend(diff["local_events"][key] for key in sorted(diff["local_events"]))
        lines = [stable_json(rec) for rec in records]
        with open(path, "w", encoding="utf-8") as f:
            f.write("\n".join(lines) + ("\n" if lines else ""))
        digest = sha256_file(path)
        con.execute("INSERT OR REPLACE INTO sync_state(path,sha256,imported_at) VALUES(?,?,?)",
                    (os.path.abspath(path), digest, now_iso()))
        con.commit()
        print(f"exported {len(diff['local'])} claims + {len(diff['local_events'])} "
              f"append-only events -> {path}")
        print(f"  ledger sha256={digest}")
        return 0

    if args.action == "import":
        path = args.path or default_sync_path(args.db)
        if not os.path.exists(path):
            print(f"error: not found: {path}", file=sys.stderr)
            return 1
        meta, remote, events = read_ledger(path)
        validate_ledger_meta(con, meta, path)
        local = db_claims(con)
        added = skipped = conflicts = event_added = 0
        for key, rec in remote.items():
            table, key_values = key
            existing = local.get(key)
            if existing is not None:
                if stable_json(existing) == stable_json(rec):
                    skipped += 1
                    continue
                local_json, remote_json = stable_json(existing), stable_json(rec)
                con.execute(
                    "INSERT OR IGNORE INTO sync_conflicts"
                    "(table_name,claim_key,local_json,remote_json,detected_at) VALUES(?,?,?,?,?)",
                    (table, stable_json(key_values), local_json, remote_json, now_iso()))
                conflicts += 1
                continue
            cols = [c for c in SYNC_TABLES[table] if c in rec]
            con.execute(
                f"INSERT INTO {table}({','.join(cols)}) VALUES({','.join('?' for _ in cols)})",
                [rec[c] for c in cols])
            added += 1
        event_cols = ["event_id", "ts", "author", "kind", "ckey", "action", "old",
                      "new", "reason", "target_sha256", "dump_sha256", "tool_version"]
        for event_id, rec in events.items():
            before = con.total_changes
            con.execute(
                f"INSERT OR IGNORE INTO history({','.join(event_cols)}) "
                f"VALUES({','.join('?' for _ in event_cols)})",
                [rec.get(c) for c in event_cols])
            event_added += con.total_changes - before
        digest = sha256_file(path)
        if conflicts == 0:
            con.execute("INSERT OR REPLACE INTO sync_state(path,sha256,imported_at) VALUES(?,?,?)",
                        (os.path.abspath(path), digest, now_iso()))
        con.commit()
        print(f"import: {added} claims added, {event_added} events added, "
              f"{skipped} unchanged, {conflicts} CONFLICTS.")
        if conflicts:
            print("  No conflicting row was overwritten. Inspect sync_conflicts and resolve "
                  "the claim explicitly before export.", file=sys.stderr)
            return 2
        return 0
    return 0


# --------------------------------------------------------------------------- #
# export  (seed a verified_*/notes_* style doc from the overlay)
# --------------------------------------------------------------------------- #
def cmd_export(args, con):
    dump = get_dump(con, args)
    row = resolve_func(con, args.target)
    if not row:
        print(f"error: function not found: {args.target}", file=sys.stderr)
        return 1
    addr, name, sig, sb, eb, sl, el = row
    globals_map = load_globals(con)
    g_self = globals_map.get(name)
    locals_map = load_locals(con, addr)
    bindings = load_bindings(con, addr)

    print("=" * 78)
    print(f"{name}" + (f" -> {g_self[0]}" if g_self else ""))
    print(f"@ 0x{addr}   lines {sl}-{el}")
    print("=" * 78)
    print(f"\nSIGNATURE\n  {sig.strip()}")
    if g_self and g_self[2]:
        print(f"\nROLE\n  {g_self[2]}")
    if bindings:
        print("\nBINDINGS")
        for v, t in sorted(bindings.items()):
            print(f"  {v} : {t}*")
            for off, nm, ft, note, tier, disp, reason in con.execute(
                "SELECT offset,new_name,ftype,note,tier,disputed,dispute_reason "
                "FROM fields WHERE type_name=? ORDER BY offset", (t,)):
                mark = f"  [!DISPUTED: {reason}]" if disp else f"  [{tier}]"
                print(f"    +0x{off:x}  {nm}{'  : ' + ft if ft else ''}{mark}"
                      f"{'  // ' + note if note else ''}")
    if locals_map:
        print("\nLOCALS")
        for orig, vals in sorted(locals_map.items()):
            new, vt, note, tier, disp, reason = vals
            mark = f"  [!DISPUTED: {reason}]" if disp else f"  [{tier}]"
            print(f"  {orig} -> {new}{'  : ' + vt if vt else ''}{mark}"
                  f"{'  // ' + note if note else ''}")
    applied = g_self[0] if g_self else None
    notes = con.execute(
        "SELECT target,body FROM notes "
        "WHERE target LIKE ? OR target=? OR target LIKE ? OR target=? ORDER BY target",
        (name + "@%", name, (applied or name) + "@%", applied or name)).fetchall()
    if notes:
        print("\nNOTES")
        for target, body in notes:
            print(f"  {target}: {body}")
    print("\n" + "-" * 78)
    print("Annotated body (revtool view):")
    print("-" * 78)
    # reuse view renderer
    class _A:
        pass
    a = _A()
    a.target = name
    a.raw = False
    a.no_apply = False
    a.no_legend = True
    a.dump = dump
    cmd_view(a, con)
    return 0


# --------------------------------------------------------------------------- #
# helpers
# --------------------------------------------------------------------------- #
def dump_path_from_meta(con, *, must_exist=False):
    """The indexed dump for this store, for callers that have no argparse namespace.

    The debt ledger needs the dump to answer "is this target a symbol at all?", and it
    is reached from places (compute_debt) that never see `args`, so the lookup lives
    here and `get_dump` delegates to it rather than the two drifting apart.

    `must_exist=True` returns None instead of falling back to DEFAULT_DUMP when the
    store names a dump that is not on disk. Falling back is right for a CLI default,
    but it is WRONG for the debt rule: judging "this is not a symbol" against some
    OTHER file is not an answer, and acting on it would erase real debt.
    """
    row = con.execute("SELECT value FROM meta WHERE key='dump'").fetchone()
    configured = row[0] if row and row[0] else None
    if configured:
        if os.path.exists(configured):
            return configured
        if must_exist:
            return None
    if must_exist and not os.path.exists(DEFAULT_DUMP):
        return None
    return DEFAULT_DUMP


def get_dump(con, args):
    if getattr(args, "dump", None):
        return args.dump
    return dump_path_from_meta(con)


def require_dump(dump):
    """Fail LOUDLY when the whole-dump scan has no dump to scan.

    A missing/empty overlay dump makes every mmap scan (iter_dump_symbol_hits)
    return nothing, so `xref` would print "0 line(s)" for EVERY symbol - which
    reads as "no callers" and quietly produces false RE conclusions (the trap that
    sent a Rizin session to `/x` byte-pattern search). The dump is a ~115 MB
    whole-program decompiler export, not something this tool regenerates, so the
    only safe behaviour is to stop and say exactly what is missing.
    """
    if not dump or not os.path.exists(dump):
        raise SystemExit(
            f"error: pseudocode dump not found: {dump}\n"
            f"  `xref` scans this whole-program dump; without it the scan finds "
            f"NOTHING for every symbol,\n"
            f"  which would read as 'no callers' and is NOT evidence of absence.\n"
            f"  Regenerate it: produce a whole-program pseudocode decompilation of "
            f"SRO_Client.exe and save it as\n"
            f"  {DEFAULT_DUMP} (see tools/re/pseudo/README.md). Then rerun "
            f"`py -3 scripts/re/revtool/revtool.py index`.\n"
            f"  Meanwhile use raw Rizin `/x` byte-pattern search for cross-references "
            f"(NOT `/c` - that is crypto search on this build).")
    if os.path.getsize(dump) == 0:
        raise SystemExit(
            f"error: pseudocode dump is EMPTY (0 bytes): {dump}\n"
            f"  An empty dump makes `xref` find NOTHING for every symbol - that is a "
            f"broken export, not 'no callers'.\n"
            f"  Regenerate it (whole-program decompilation of SRO_Client.exe -> "
            f"{DEFAULT_DUMP}; see tools/re/pseudo/README.md),\n"
            f"  then rerun `py -3 scripts/re/revtool/revtool.py index`.")


# --------------------------------------------------------------------------- #
# binary-backed reads (vtable / data resolution the text dump cannot do)
#
# The pseudocode dump has NO data arrays: a vtable's slots, an .rdata descriptor,
# a stamped vtable address - none of that is in the text. Resolving a virtual call
# `(*(*obj + 0xNN))()` to a concrete function therefore needs the PE image. This is
# a tiny, dependency-free PE32 reader (DOS->PE->optional->section map) so revtool
# can do `vtable`/`deref` itself instead of dropping the engineer into raw rizin.
# --------------------------------------------------------------------------- #
class PEImage:
    def __init__(self, path):
        self.path = path
        with open(path, "rb") as f:
            self.data = f.read()
        d = self.data
        if d[:2] != b"MZ":
            raise ValueError("not a PE (no MZ)")
        pe = int.from_bytes(d[0x3c:0x40], "little")
        if d[pe:pe + 4] != b"PE\x00\x00":
            raise ValueError("not a PE (no PE\\0\\0)")
        n_sections = int.from_bytes(d[pe + 6:pe + 8], "little")
        opt_size = int.from_bytes(d[pe + 20:pe + 22], "little")
        opt = pe + 24
        magic = int.from_bytes(d[opt:opt + 2], "little")
        if magic == 0x10b:        # PE32
            self.image_base = int.from_bytes(d[opt + 28:opt + 32], "little")
        elif magic == 0x20b:      # PE32+
            self.image_base = int.from_bytes(d[opt + 24:opt + 32], "little")
        else:
            raise ValueError(f"unknown optional-header magic {magic:#x}")
        self.image_size = int.from_bytes(d[opt + 56:opt + 60], "little")
        sec = opt + opt_size
        self.sections = []  # (name, va, vsize, raw_ptr, raw_size)
        for i in range(n_sections):
            h = sec + i * 40
            name = d[h:h + 8].rstrip(b"\x00").decode("ascii", "replace")
            vsize = int.from_bytes(d[h + 8:h + 12], "little")
            va = int.from_bytes(d[h + 12:h + 16], "little")
            rsize = int.from_bytes(d[h + 16:h + 20], "little")
            rptr = int.from_bytes(d[h + 20:h + 24], "little")
            self.sections.append((name, va, vsize, rptr, rsize))

    def section_of(self, va):
        rva = va - self.image_base
        for name, sva, vsize, rptr, rsize in self.sections:
            span = max(vsize, rsize)
            if sva <= rva < sva + span:
                return (name, sva, vsize, rptr, rsize)
        return None

    def _offset(self, va):
        s = self.section_of(va)
        if not s:
            return None
        _name, sva, _vsize, rptr, rsize = s
        rva = va - self.image_base
        off = rva - sva + rptr
        if off + 4 > rptr + rsize:      # in virtual-only tail (uninitialised)
            return None
        return off

    def classify(self, va):
        """Why a read can't resolve, so callers can give a useful hint instead of '----':
          'ok'       - readable raw bytes
          'uninit'   - inside a section but in its virtual-only tail (.bss-style):
                       zero in the image, POPULATED AT RUNTIME -> find the writer
          'unmapped' - not inside any section
        """
        s = self.section_of(va)
        if not s:
            return "unmapped"
        _name, sva, _vsize, rptr, rsize = s
        off = (va - self.image_base) - sva + rptr
        return "ok" if off + 4 <= rptr + rsize else "uninit"

    def read_bytes(self, va, n):
        off = self._offset(va)
        if off is None:
            return None
        return self.data[off:off + n]

    def read_u32(self, va):
        b = self.read_bytes(va, 4)
        if b is None or len(b) < 4:
            return None
        return int.from_bytes(b, "little")

    def in_image(self, va):
        return self.image_base <= va < self.image_base + self.image_size


def get_binary(con, args):
    """Resolve the PE path: --binary flag > meta 'binary' > default SRO_Client.exe."""
    cand = getattr(args, "binary", None)
    if cand:
        return cand
    row = con.execute("SELECT value FROM meta WHERE key='binary'").fetchone()
    if row and row[0] and os.path.exists(row[0]):
        return row[0]
    return DEFAULT_BINARY


def open_pe(con, args):
    path = get_binary(con, args)
    if not os.path.exists(path):
        print(f"error: PE binary not found: {path}\n"
              f"  point revtool at it once:  revtool config binary <path-to-SRO_Client.exe>",
              file=sys.stderr)
        return None
    try:
        return PEImage(path)
    except (OSError, ValueError) as e:
        print(f"error: cannot read PE {path}: {e}", file=sys.stderr)
        return None


def resolve_addr(con, va):
    """Map a code VA to (label, status_tag). Exact function header if possible, else
    the containing function with a +offset. status_tag mirrors xref: [name], [labeled],
    [NEEDS RE], or [data/unknown]."""
    a = "%08x" % va
    row = con.execute("SELECT name FROM functions WHERE addr=?", (a,)).fetchone()
    off = 0
    if not row:
        # containing function: greatest header addr <= va (8-wide hex sorts numerically)
        row = con.execute(
            "SELECT name,addr FROM functions WHERE addr<=? ORDER BY addr DESC LIMIT 1",
            (a,)).fetchone()
        if row:
            off = va - int(row[1], 16)
            # guard against "previous function" false positives for far targets
            if off > 0x4000:
                return (f"(0x{va:08x} - no function here)", "[data/unknown]")
        else:
            return (f"(0x{va:08x} - no function here)", "[data/unknown]")
    orig = row[0]
    applied = con.execute(
        "SELECT new_name FROM globals WHERE orig_name=?", (orig,)).fetchone()
    name = applied[0] if applied and applied[0] else orig
    suffix = f"+0x{off:x}" if off else ""
    if applied and applied[0]:
        tag = "[labeled]"
    elif symbol_is_annotated(con, orig):
        tag = "[labeled]"
    else:
        tag = "[NEEDS RE]"
    return (name + suffix, tag)


def _resolve_vtable_slot(pe, con, base, slot_off, class_name=None, for_base=None):
    sec = pe.section_of(base)
    secname = sec[0] if sec else "?"
    target = pe.read_u32(base + slot_off)
    info = {
        "base": base,
        "slot": slot_off,
        "section": secname,
        "class": class_name,
        "for_base": for_base,
        "target": target,
        "name": None,
        "tag": None,
        "error": None,
    }
    if target is None:
        kind = pe.classify(base + slot_off)
        hint = ("uninitialised (.bss/virtual) -> runtime-populated; this is a "
                "RUNTIME vtable/field, find the writer (rizin_recipes.md recipe 1/2)"
                if kind == "uninit" else "address not in any section")
        info["error"] = hint
        return info
    name, tag = resolve_addr(con, target)
    info["name"] = name
    info["tag"] = tag
    return info


def _vtable_label(info):
    parts = []
    if info.get("class"):
        if info.get("for_base"):
            parts.append(f"{info['class']}{{for {info['for_base']}}}")
        else:
            parts.append(info["class"])
    parts.append(f"vtable 0x{info['base']:08x} ({info['section']})")
    return " ".join(parts)


def _collect_vtable_addrs(args, con, pe):
    entries = []
    for raw in args.addr:
        base = _as_int(raw)
        if base is None:
            raise ValueError(f"bad address {raw!r}")
        entries.append({"addr": base, "class": None, "for_base": None, "count": None})

    if getattr(args, "base", None):
        idx = _best_rtti_index(args, con, pe)
        classes = idx["classes"]
        base_cls = args.base
        if base_cls not in classes:
            hit = [c for c in classes if c.lower() == base_cls.lower()]
            if not hit:
                raise ValueError(f"class not found in RTTI: {base_cls}")
            base_cls = hit[0]
        family = {base_cls} | idx["desc"].get(base_cls, set())
        for cls in sorted(family):
            for vaddr, for_base in idx.get("class_vtables", {}).get(cls, []):
                # Default family comparison follows the primary inheritance-chain
                # vtable for each class. Secondary subobject vtables from multiple
                # inheritance (for example CAnimationCallback) use the same byte
                # offsets for a different receiver type, so including them would
                # falsely make unrelated slots look polymorphic.
                if not getattr(args, "all_subobjects", False):
                    if for_base is not None:
                        continue
                record = idx.get("vtable_records", {}).get(vaddr)
                entries.append({
                    "addr": vaddr, "class": cls, "for_base": for_base,
                    "count": len(record.targets) if record is not None else None,
                })

    # Preserve order while dropping duplicate concrete vtable addresses.
    seen = set()
    uniq = []
    for e in entries:
        if e["addr"] in seen:
            continue
        seen.add(e["addr"])
        uniq.append(e)
    return uniq


def _print_vtable_slot_classification(pe, con, entries, slot_off):
    infos = [
        _resolve_vtable_slot(pe, con, e["addr"], slot_off, e.get("class"), e.get("for_base"))
        for e in entries
    ]
    print(f"vtable slot classification +0x{slot_off:x} across {len(infos)} vtable(s)")
    targets = {}
    unreadable = []
    for info in infos:
        label = _vtable_label(info)
        if info["target"] is None:
            unreadable.append(info)
            print(f"  {label} -> <unreadable - {info['error']}>")
            continue
        print(f"  {label} -> 0x{info['target']:08x}  {info['name']}  {info['tag']}")
        targets.setdefault(info["target"], []).append(info)

    if unreadable:
        print(f"\nverdict: UNKNOWN ({len(unreadable)} unreadable slot(s); resolve runtime writer first)")
        return 1

    if not targets:
        print("\nverdict: UNKNOWN (no readable targets)")
        return 1

    if len(targets) == 1:
        target, hit_infos = next(iter(targets.items()))
        name, tag = resolve_addr(con, target)
        print(f"\nverdict: MONOMORPHIC ({len(hit_infos)} vtable(s), one target)")
        print(f"devirtualize candidate: direct call 0x{target:08x}  {name}  {tag}")
        print("wip rule: call the exported WIP function directly; keep receiver/vtable/slot evidence in comments.")
        return 0

    print(f"\nverdict: POLYMORPHIC ({len(targets)} targets across {len(infos)} vtable(s))")
    for target, hit_infos in sorted(targets.items()):
        name, tag = resolve_addr(con, target)
        vt_list = ", ".join(f"0x{i['base']:08x}" for i in hit_infos)
        print(f"  0x{target:08x}  {name}  {tag}  from {vt_list}")
    print("wip rule: keep an interface/dispatch shape; add JSDoc @see {@link ...} for each concrete WIP target.")
    return 0


def cmd_vtable(args, con):
    """Dump a vtable from the PE and resolve each slot to a (possibly renamed) function.

    Automates the manual rizin recipe for virtual-call resolution:
      `vtable 0xbcc27c` -> +0x2c  0x004fb7e0  CIFConsole_Update  [labeled]
    Pair with `--slot 0xNN` to read exactly the slot a `(*(*obj+0xNN))()` call uses.
    """
    pe = open_pe(con, args)
    if pe is None:
        return 1
    try:
        entries = _collect_vtable_addrs(args, con, pe)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        return 1
    if not entries:
        print("error: provide a vtable VA or --base <Class>", file=sys.stderr)
        return 1
    if len(entries) > 1 and args.slot is None:
        print("error: comparing multiple vtables requires --slot 0xNN", file=sys.stderr)
        return 1
    base = entries[0]["addr"]
    sec = pe.section_of(base)
    secname = sec[0] if sec else "?"
    if args.slot is not None:
        slot_off = _as_int(args.slot)
        if slot_off is None:
            print(f"error: bad slot {args.slot!r}", file=sys.stderr)
            return 1
        if len(entries) > 1:
            return _print_vtable_slot_classification(pe, con, entries, slot_off)
        target = pe.read_u32(base + slot_off)
        if target is None:
            kind = pe.classify(base + slot_off)
            hint = ("uninitialised (.bss/virtual) -> runtime-populated; this is a "
                    "RUNTIME vtable/field, find the writer (rizin_recipes.md recipe 1/2)"
                    if kind == "uninit" else "address not in any section")
            print(f"slot +0x{slot_off:x}: <unreadable - {hint}>")
            return 1
        name, tag = resolve_addr(con, target)
        print(f"vtable 0x{base:08x} ({secname})  +0x{slot_off:x}  ->  "
              f"0x{target:08x}  {name}  {tag}")
        return 0
    structural_count = entries[0].get("count")
    count = min(args.count, structural_count) if structural_count is not None else args.count
    bound = " structurally bounded" if structural_count is not None else " user-limited"
    print(f"vtable 0x{base:08x} ({secname})  -  {count} slots ({bound.strip()})")
    printed = 0
    for i in range(count):
        slot_off = i * 4
        target = pe.read_u32(base + slot_off)
        if target is None:
            if i == 0 and pe.classify(base) == "uninit":
                print(f"  +0x{slot_off:<4x} <uninitialised (.bss/virtual) -> RUNTIME vtable; "
                      f"find the writer, rizin_recipes.md recipe 1/2>")
            else:
                print(f"  +0x{slot_off:<4x} <unreadable - past section>")
            break
        if not pe.in_image(target):
            # a 0 or non-code word usually marks the end of the vtable (RTTI/next obj)
            print(f"  +0x{slot_off:<4x} 0x{target:08x}  (non-code - vtable likely ends here)")
            if target == 0:
                break
            continue
        name, tag = resolve_addr(con, target)
        print(f"  +0x{slot_off:<4x} 0x{target:08x}  {name}  {tag}")
        printed += 1
    if printed:
        print(f"\ntip: the GWnd/CObjTree update slot is +0x2c; bind the object's type and "
              f"`coverage` the resolved fn next.")
    return 0


def cmd_deref(args, con):
    """Read N little-endian dwords from a VA in the PE (the `pxw` equivalent).

    For .rdata descriptors, class records, jump tables - anything the text dump omits.
    """
    pe = open_pe(con, args)
    if pe is None:
        return 1
    try:
        base = int(args.addr, 16)
    except ValueError:
        print(f"error: bad address {args.addr!r}", file=sys.stderr)
        return 1
    sec = pe.section_of(base)
    print(f"deref 0x{base:08x} ({sec[0] if sec else '?'})")
    per_row = 4
    any_readable = False
    for r in range((args.count + per_row - 1) // per_row):
        va = base + r * per_row * 4
        words, ascii_bytes = [], b""
        for c in range(per_row):
            if r * per_row + c >= args.count:
                break
            w = pe.read_u32(va + c * 4)
            if w is None:
                words.append("--------")
                ascii_bytes += b"...."
            else:
                any_readable = True
                words.append(f"{w:08x}")
                ascii_bytes += w.to_bytes(4, "little")
        printable = "".join(chr(b) if 32 <= b < 127 else "." for b in ascii_bytes)
        print(f"  0x{va:08x}  {' '.join(words):<44}  {printable}")
    if not any_readable:
        kind = pe.classify(base)
        if kind == "uninit":
            print(f"\n  note: 0x{base:08x} is in '{sec[0] if sec else '?'}' but ZERO in the image "
                  f"(.bss-style virtual tail) -> POPULATED AT RUNTIME.\n"
                  f"  A static deref can't see it. Two ways forward:\n"
                  f"    - read it LIVE:  revtool runtime 0x{base:08x}   (launches the client, "
                  f"polls, resolves the value to a function, kills it)\n"
                  f"    - find the writer statically: rizin_recipes.md recipe 1/2.")
        elif kind == "unmapped":
            print(f"\n  note: 0x{base:08x} is not inside any PE section (bad address?).")
    return 0


# --------------------------------------------------------------------------- #
# d3dstate - fixed-function pipeline state timeline for a function
# --------------------------------------------------------------------------- #
# Born from the color0x18c incident (notes 363/368/372): a SetMaterial right
# before a draw was assumed consumed by that draw, but D3DRS_LIGHTING was OFF,
# making the material inert. The consumer of a device binding can only be
# judged against the PIPELINE STATE at the draw, never by call proximity.
# This command makes that state visible so the mistake is structurally hard.

# D3DRENDERSTATETYPE ids seen via SetRenderState(Cached) - common subset.
D3DRS_NAMES = {
    7: "ZENABLE", 8: "FILLMODE", 9: "SHADEMODE", 14: "ZWRITEENABLE",
    15: "ALPHATESTENABLE", 16: "LASTPIXEL", 19: "SRCBLEND", 20: "DESTBLEND",
    22: "CULLMODE", 23: "ZFUNC", 24: "ALPHAREF", 25: "ALPHAFUNC",
    26: "DITHERENABLE", 27: "ALPHABLENDENABLE", 28: "FOGENABLE",
    29: "SPECULARENABLE", 34: "FOGCOLOR", 35: "FOGTABLEMODE", 36: "FOGSTART",
    37: "FOGEND", 38: "FOGDENSITY", 48: "RANGEFOGENABLE", 52: "STENCILENABLE",
    60: "TEXTUREFACTOR", 136: "CLIPPING", 137: "LIGHTING", 139: "AMBIENT",
    140: "FOGVERTEXMODE", 141: "COLORVERTEX", 142: "LOCALVIEWER",
    143: "NORMALIZENORMALS", 145: "DIFFUSEMATERIALSOURCE",
    146: "SPECULARMATERIALSOURCE", 147: "AMBIENTMATERIALSOURCE",
    148: "EMISSIVEMATERIALSOURCE", 152: "VERTEXBLEND", 153: "CLIPPLANEENABLE",
}

# IDirect3DDevice9 vtable byte offsets (slot*4) for state/draw calls.
# 0x158/0x15c added after the color0x1c0 incident (notes 347 vs 373): an undecoded
# `(*(*dev+0x15c))(dev, X)` was misread as a texture bind; it is SetVertexDeclaration.
D3D9_VTBL = {
    0xb0: ("SetTransform", "transform"), 0xbc: ("SetViewport", "state"),
    0xc4: ("SetMaterial", "material"), 0xcc: ("SetLight", "state"),
    0xd4: ("LightEnable", "lightenable"), 0xdc: ("SetClipPlane", "state"),
    0xe4: ("SetRenderState", "renderstate"), 0x104: ("SetTexture", "state"),
    0x10c: ("SetTextureStageState", "tss"), 0x114: ("SetSamplerState", "state"),
    0x144: ("DrawPrimitive", "draw"), 0x148: ("DrawIndexedPrimitive", "draw"),
    0x14c: ("DrawPrimitiveUP", "draw"), 0x150: ("DrawIndexedPrimitiveUP", "draw"),
    0x158: ("CreateVertexDeclaration", "state"),
    0x15c: ("SetVertexDeclaration", "state"),
    0x164: ("SetFVF", "state"), 0x170: ("SetVertexShader", "vshader"),
    0x178: ("SetVertexShaderConstantF", "state"),
    0x190: ("SetStreamSource", "state"), 0x198: ("SetStreamSourceFreq", "state"),
    0x1a0: ("SetIndices", "state"), 0x1ac: ("SetPixelShader", "state"),
    0x1b4: ("SetPixelShaderConstantF", "state"),
}

# Enum decoders. Born from the color0x1c0 incident: `SetTextureStageState(0,1,7)` was
# misremembered as MODULATE (4) when 7 = D3DTOP_ADD, and SRCBLEND=1/DESTBLEND=3 were never
# decoded to ZERO/SRCCOLOR, hiding that the pass is a framebuffer MULTIPLY. Decode the values
# so the reader never has to (mis)recall a D3D enum again.
D3DBLEND_NAMES = {
    1: "ZERO", 2: "ONE", 3: "SRCCOLOR", 4: "INVSRCCOLOR", 5: "SRCALPHA",
    6: "INVSRCALPHA", 7: "DESTALPHA", 8: "INVDESTALPHA", 9: "DESTCOLOR",
    10: "INVDESTCOLOR", 11: "SRCALPHASAT", 14: "BLENDFACTOR", 15: "INVBLENDFACTOR",
}
D3DTOP_NAMES = {
    1: "DISABLE", 2: "SELECTARG1", 3: "SELECTARG2", 4: "MODULATE", 5: "MODULATE2X",
    6: "MODULATE4X", 7: "ADD", 8: "ADDSIGNED", 9: "ADDSIGNED2X", 10: "SUBTRACT",
    11: "ADDSMOOTH", 12: "BLENDDIFFUSEALPHA", 13: "BLENDTEXTUREALPHA",
    14: "BLENDFACTORALPHA", 15: "BLENDTEXTUREALPHAPM", 16: "BLENDCURRENTALPHA",
    18: "PREMODULATE", 19: "MODULATEALPHA_ADDCOLOR", 20: "MODULATECOLOR_ADDALPHA",
    24: "DOTPRODUCT3", 25: "MULTIPLYADD", 26: "LERP",
}
D3DTA_NAMES = {0: "DIFFUSE", 1: "CURRENT", 2: "TEXTURE", 3: "TFACTOR", 4: "SPECULAR",
               5: "TEMP", 6: "CONSTANT"}
D3DCMP_NAMES = {1: "NEVER", 2: "LESS", 3: "EQUAL", 4: "LESSEQUAL", 5: "GREATER",
                6: "NOTEQUAL", 7: "GREATEREQUAL", 8: "ALWAYS"}
D3DCULL_NAMES = {1: "NONE", 2: "CW", 3: "CCW"}
D3DFOGMODE_NAMES = {0: "NONE", 1: "EXP", 2: "EXP2", 3: "LINEAR"}
D3DFILL_NAMES = {1: "POINT", 2: "WIREFRAME", 3: "SOLID"}
D3DSHADE_NAMES = {1: "FLAT", 2: "GOURAUD", 3: "PHONG"}
D3DBLENDOP_NAMES = {1: "ADD", 2: "SUBTRACT", 3: "REVSUBTRACT", 4: "MIN", 5: "MAX"}
D3DPT_NAMES = {1: "POINTLIST", 2: "LINELIST", 3: "LINESTRIP", 4: "TRIANGLELIST",
               5: "TRIANGLESTRIP", 6: "TRIANGLEFAN"}
D3DTS_NAMES = {2: "VIEW", 3: "PROJECTION", 256: "WORLD"}
D3DTS_NAMES.update({16 + i: f"TEXTURE{i}" for i in range(8)})

D3DTSS_NAMES = {
    1: "COLOROP", 2: "COLORARG1", 3: "COLORARG2", 4: "ALPHAOP", 5: "ALPHAARG1",
    6: "ALPHAARG2", 7: "BUMPENVMAT00", 8: "BUMPENVMAT01", 9: "BUMPENVMAT10",
    10: "BUMPENVMAT11", 11: "TEXCOORDINDEX", 22: "BUMPENVLSCALE", 23: "BUMPENVLOFFSET",
    24: "TEXTURETRANSFORMFLAGS", 26: "COLORARG0", 27: "ALPHAARG0", 28: "RESULTARG",
    32: "CONSTANT",
}
# TSS types whose value is a D3DTOP op vs a D3DTA argument selector.
_D3DTSS_OP_TYPES = {1, 4}
_D3DTSS_ARG_TYPES = {2, 3, 5, 6, 26, 27, 28}

# Render states whose VALUE is an enum worth decoding.
_D3DRS_VALUE_DECODER = {
    8: D3DFILL_NAMES, 9: D3DSHADE_NAMES, 19: D3DBLEND_NAMES, 20: D3DBLEND_NAMES,
    22: D3DCULL_NAMES, 23: D3DCMP_NAMES, 25: D3DCMP_NAMES, 35: D3DFOGMODE_NAMES,
    140: D3DFOGMODE_NAMES, 171: D3DBLENDOP_NAMES,
}


def _dstate_rs_value(sid, val_tok):
    """Decode an enum-valued render state's value, e.g. SRCBLEND 1 -> '1 (ZERO)'."""
    v = _dstate_int(val_tok)
    dec = _D3DRS_VALUE_DECODER.get(sid)
    if v is not None and dec and v in dec:
        return f"{val_tok} ({dec[v]})"
    return val_tok


def _dstate_ta(value):
    """Decode a D3DTA argument selector incl. modifier bits."""
    base = D3DTA_NAMES.get(value & 0xf, f"0x{value & 0xf:x}")
    mods = ("|COMPLEMENT" if value & 0x10 else "") + ("|ALPHAREPLICATE" if value & 0x20 else "")
    return base + mods


def _dstate_tss(argl):
    """Decode (stage, type, value) of a SetTextureStageState arg list (last 3 args).
    Returns (text, meta) where meta = (stage:int|None, type:int|None, value:int|None)."""
    if len(argl) < 3:
        return ", ".join(argl), (None, None, None)
    stage_tok, typ_tok, val_tok = argl[-3], argl[-2], argl[-1]
    stage, typ, val = _dstate_int(stage_tok), _dstate_int(typ_tok), _dstate_int(val_tok)
    tname = D3DTSS_NAMES.get(typ, typ_tok)
    if val is not None and typ in _D3DTSS_OP_TYPES:
        vtext = f"{val_tok} ({D3DTOP_NAMES.get(val, '?')})"
    elif val is not None and typ in _D3DTSS_ARG_TYPES:
        vtext = f"{val_tok} ({_dstate_ta(val)})"
    else:
        vtext = val_tok
    return f"stage{stage_tok}, {tname} = {vtext}", (stage, typ, val)


def _dstate_blend_summary(rs_vals, tss_vals):
    """Human conclusion lines derived from the accumulated blend/stage state."""
    out = []
    src, dst = rs_vals.get(19), rs_vals.get(20)
    if rs_vals.get(27) == 1 and src is not None and dst is not None:
        s, d = D3DBLEND_NAMES.get(src, f"0x{src:x}"), D3DBLEND_NAMES.get(dst, f"0x{dst:x}")
        eq = f"framebuffer = src*{s} + dest*{d}"
        if (src, dst) == (1, 3):
            eq += "   == framebuffer *= srcColor  (MULTIPLICATIVE darken/tint - NOT alpha blending)"
        elif (src, dst) == (5, 6):
            eq += "   (standard SRCALPHA/INVSRCALPHA alpha blend)"
        elif (src, dst) == (2, 2):
            eq += "   (ADDITIVE)"
        elif (src, dst) == (9, 1):
            eq += "   == framebuffer *= destColor swap form (multiplicative)"
        out.append(eq)
    cop = tss_vals.get((0, 1))
    if cop is not None:
        a1 = tss_vals.get((0, 2)); a2 = tss_vals.get((0, 3))
        opn = D3DTOP_NAMES.get(cop, f"0x{cop:x}")
        a1s = _dstate_ta(a1) if a1 is not None else "CURRENT(default arg1=TEXTURE)"
        a2s = _dstate_ta(a2) if a2 is not None else "CURRENT(default)"
        if cop == 2:
            out.append(f"stage0 colour = {a1s}  (SELECTARG1: arg2 ignored)")
        elif cop == 3:
            out.append(f"stage0 colour = {a2s}  (SELECTARG2: arg1 ignored)")
        elif cop == 1:
            out.append("stage0 colour op DISABLED")
        else:
            out.append(f"stage0 colour = {a1s} {opn} {a2s}")
    aop = tss_vals.get((0, 4))
    if aop is not None:
        b1 = tss_vals.get((0, 5)); b2 = tss_vals.get((0, 6))
        opn = D3DTOP_NAMES.get(aop, f"0x{aop:x}")
        b1s = _dstate_ta(b1) if b1 is not None else "(default)"
        if aop == 2:
            out.append(f"stage0 alpha  = {b1s}")
        elif aop == 1:
            out.append("stage0 alpha op DISABLED")
        else:
            b2s = _dstate_ta(b2) if b2 is not None else "(default)"
            out.append(f"stage0 alpha  = {b1s} {opn} {b2s}")
    if 60 in rs_vals:
        out.append("TEXTUREFACTOR written in this function (TFACTOR args read it)")
    return out

_DSTATE_LINE_ADDR = re.compile(r"^([0-9a-f]{8})\s")
_DSTATE_VTBL = re.compile(r"\+ 0x([0-9a-f]+)\)\)\(")
_DSTATE_INT = re.compile(r"^(0x[0-9a-f]+|\d+)$")
_DSTATE_CALLNAME = re.compile(r"\b([A-Za-z_][A-Za-z0-9_]*)\(")


def _dstate_int(tok):
    tok = tok.strip().rstrip(";")
    m = _DSTATE_INT.match(tok)
    if not m:
        return None
    return int(tok, 16) if tok.lower().startswith("0x") else int(tok)


def _dstate_args(code):
    """Split the argument list of the LAST call on the line (best effort)."""
    end = code.rfind(")")
    if end < 0:
        return []
    depth, start = 0, -1
    for i in range(end, -1, -1):
        c = code[i]
        if c == ")":
            depth += 1
        elif c == "(":
            depth -= 1
            if depth == 0:
                start = i
                break
    if start < 0:
        return []
    inner, args, d, cur = code[start + 1:end], [], 0, ""
    for c in inner:
        if c in "([":
            d += 1
        elif c in ")]":
            d -= 1
        if c == "," and d == 0:
            args.append(cur.strip()); cur = ""
        else:
            cur += c
    if cur.strip():
        args.append(cur.strip())
    return args


def cmd_d3dstate(args, con):
    """Fixed-function D3D9 state timeline of one function, with lighting tracking.

    Walks the function's pseudocode in order, extracts every device state event
    (SetRenderState* helpers + raw device vtable calls), tracks D3DRS_LIGHTING,
    and WARNS when a material is bound while lighting is off/unknown - the
    proximity-inference trap. `--at 0xADDR` reports the accumulated state there.
    """
    dump = get_dump(con, args)
    row = resolve_func(con, args.target)
    if not row:
        print(f"error: function not found: {args.target}", file=sys.stderr)
        return 1
    addr, name, sig, sb, eb, sl, el = row
    shown = load_globals(con).get(name)
    disp = f"{name}" + (f" [{shown[0]}]" if shown else "")
    at = norm_addr(args.at) if args.at else None
    at_n = int(at, 16) if at else None

    rename = build_rename_map(load_globals(con), load_locals(con, addr))
    lines = read_func_lines(dump, sb, eb)

    # Group continuation lines (same leading address) into one logical statement:
    # the decompiler wraps long calls across lines, which used to hide e.g. the
    # multi-line TEXTUREFACTOR write in sub_8a4d40 from the parser entirely.
    stmts = []   # [addr_str, joined_code]
    cur_addr = None
    for raw in lines:
        m = _DSTATE_LINE_ADDR.match(raw)
        if m:
            cur_addr = m.group(1)
        if cur_addr is None:
            continue
        body = (raw[8:] if m else raw).split("//", 1)[0].strip()
        if not body:
            continue
        if stmts and stmts[-1][0] == cur_addr:
            stmts[-1][1] += " " + body
        else:
            stmts.append([cur_addr, body])

    events = []   # (addr_int, addr_str, kind, text, meta)
    for cur_addr, joined in stmts:
        code = apply_renames(joined, rename)
        a_int = int(cur_addr, 16)
        if cur_addr == addr:
            continue  # the function's own signature/prologue line is not a call event

        # raw device vtable call: (*(...)(*(...)obj + 0xNN))(obj, ...)
        vm = _DSTATE_VTBL.search(code)
        if vm:
            off = int(vm.group(1), 16)
            ent = D3D9_VTBL.get(off)
            if ent:
                fname, kind = ent
                argl = _dstate_args(code)
                if kind == "renderstate" and len(argl) >= 3:
                    sid = _dstate_int(argl[-2])
                    sname = D3DRS_NAMES.get(sid, f"0x{sid:x}" if sid is not None else argl[-2])
                    events.append((a_int, cur_addr, "rs",
                                   f"SetRenderState({sname}={_dstate_rs_value(sid, argl[-1])})",
                                   (sid, _dstate_int(argl[-1]))))
                elif kind == "tss":
                    text, meta = _dstate_tss(argl)
                    events.append((a_int, cur_addr, "tss", f"{fname}({text})", meta))
                elif kind == "lightenable" and len(argl) >= 3:
                    events.append((a_int, cur_addr, "lighten",
                                   f"LightEnable(index={argl[-2]}, enable={argl[-1]})", None))
                elif kind == "draw" and len(argl) >= 2:
                    pt = _dstate_int(argl[1])
                    ptname = D3DPT_NAMES.get(pt)
                    rest = ", ".join(argl[1:])
                    events.append((a_int, cur_addr, "draw",
                                   f"{fname}({rest})" + (f"   [{ptname}]" if ptname else ""), None))
                elif kind == "transform" and len(argl) >= 2:
                    ts = _dstate_int(argl[-2]) if len(argl) >= 3 else _dstate_int(argl[-1])
                    tsname = D3DTS_NAMES.get(ts)
                    events.append((a_int, cur_addr, "state",
                                   f"{fname}({', '.join(argl[1:])})"
                                   + (f"   [D3DTS_{tsname}]" if tsname else ""), None))
                else:
                    events.append((a_int, cur_addr, kind,
                                   f"{fname}({', '.join(argl[1:]) if len(argl) > 1 else ''})", None))
                continue

        # named helper calls (post-rename), e.g. SetRenderStateCached(cache, 0x89, 1)
        for cm in _DSTATE_CALLNAME.finditer(code):
            fn = cm.group(1)
            if re.search(r"SetRenderState", fn):
                argl = _dstate_args(code)
                if len(argl) >= 2:
                    sid = _dstate_int(argl[-2])
                    sname = D3DRS_NAMES.get(sid, f"0x{sid:x}" if sid is not None else argl[-2])
                    events.append((a_int, cur_addr, "rs",
                                   f"{fn}({sname}={_dstate_rs_value(sid, argl[-1])})",
                                   (sid, _dstate_int(argl[-1]))))
                break
            if re.search(r"SetMaterial", fn):
                events.append((a_int, cur_addr, "material", f"{fn}(...)", None))
                break
            if re.search(r"SetTextureStageState|SetTSS", fn):
                text, meta = _dstate_tss(_dstate_args(code))
                events.append((a_int, cur_addr, "tss", f"{fn}({text})", meta))
                break
            if re.search(r"LightEnable", fn):
                argl = _dstate_args(code)
                events.append((a_int, cur_addr, "lighten", f"{fn}({', '.join(argl)})", None))
                break
            if re.search(r"Draw|Render", fn) and not re.match(r"sub_[0-9a-f]+$", fn):
                events.append((a_int, cur_addr, "call-draw?", f"call {fn}(...) [callee may draw/set state]", None))
                break
            if re.match(r"sub_[0-9a-f]{6,8}$", fn):
                # opaque callee: it may draw or change state; surface it instead of hiding it
                events.append((a_int, cur_addr, "call?", f"call {fn}(...) [unlabeled callee - may draw/set state]", None))
                break

    if not events:
        print(f"{disp}: no D3D state events recognised (not a render-path function?)")
        return 0

    # walk + lighting/blend/stage tracking
    lighting = None        # None = inherited/unknown
    last_material = None   # (addr_str, lighting_at_bind)
    rs_vals = {}           # D3DRS id -> last int value (None values not stored)
    tss_vals = {}          # (stage, type) -> last int value
    warns = []
    print(f"d3dstate {disp}  ({len(events)} events"
          + (f", state evaluated at 0x{at}" if at else "") + ")")
    print(f"  LIGHTING starts as INHERITED/unknown (set by caller or previous frame)")
    for a_int, a_str, kind, text, meta in events:
        marker = "  "
        note = ""
        if kind == "rs":
            if meta and meta[0] is not None:
                rs_vals[meta[0]] = meta[1]   # value may be None (non-literal expr) - presence still counts
            if "LIGHTING=" in text:
                val = text.rsplit("=", 1)[-1].rstrip(")")
                lighting = _dstate_int(val) if _dstate_int(val) is not None else val
                note = "   <<< FF lighting " + ("ON" if lighting == 1 else "OFF" if lighting == 0 else f"= {lighting}")
        elif kind == "tss":
            if meta and meta[0] is not None and meta[1] is not None and meta[2] is not None:
                tss_vals[(meta[0], meta[1])] = meta[2]
        elif kind == "material":
            last_material = (a_str, lighting)
            if lighting == 0:
                note = "   !! bound while LIGHTING=0 - FF IGNORES the material (inert unless lighting turns on AND no rebind)"
                warns.append(a_str)
            elif lighting is None:
                note = "   ?? lighting INHERITED here - verify caller state before attributing this material to a draw"
                warns.append(a_str)
        elif kind == "draw":
            mat = f"material from 0x{last_material[0]}" if last_material else "material INHERITED"
            lit = "ON" if lighting == 1 else "OFF" if lighting == 0 else "INHERITED"
            note = f"   <<< DRAW with LIGHTING={lit}, {mat}" + \
                   ("" if lighting == 1 else " (material NOT consumed)" if lighting == 0 else "")
        elif kind == "vshader":
            note = "   <<< programmable VS path - FF lighting/material do not apply to these draws"
        if at_n is not None and a_int > at_n:
            marker = " >"   # past the --at point
        print(f"{marker}{a_str}  {text}{note}")

    print()
    lit = "ON" if lighting == 1 else "OFF" if lighting == 0 else "INHERITED/unknown"
    if at_n is not None:
        upto = [e for e in events if e[0] <= at_n]
        lit_at, mat_at = None, None
        rs_at, tss_at = {}, {}
        for a_int, a_str, kind, text, meta in upto:
            if kind == "rs":
                if meta and meta[0] is not None:
                    rs_at[meta[0]] = meta[1]
                if "LIGHTING=" in text:
                    v = _dstate_int(text.rsplit("=", 1)[-1].rstrip(")"))
                    lit_at = v if v is not None else lit_at
            elif kind == "tss":
                if meta and meta[0] is not None and meta[1] is not None and meta[2] is not None:
                    tss_at[(meta[0], meta[1])] = meta[2]
            elif kind == "material":
                mat_at = a_str
        lit_s = "ON" if lit_at == 1 else "OFF" if lit_at == 0 else "INHERITED/unknown"
        print(f"state at 0x{at}: LIGHTING={lit_s}; active material = "
              + (f"bound @0x{mat_at}" if mat_at else "inherited"))
        for line in _dstate_blend_summary(rs_at, tss_at):
            print(f"  {line}")
        if lit_at == 0 and mat_at:
            print("  -> any draw here CANNOT consume the material (FF ignores D3DMATERIAL9 while lighting is off).")
        elif lit_at is None:
            print("  -> lighting is inherited: check the CALLER's state before concluding anything about materials.")
    else:
        print(f"end-of-function: LIGHTING={lit}")
        summary = _dstate_blend_summary(rs_vals, tss_vals)
        if summary:
            print("blend/stage conclusion (accumulated over this function; caller state may differ):")
            for line in summary:
                print(f"  {line}")
    if warns:
        print(f"  {len(warns)} material bind(s) under lighting OFF/unknown: "
              + ", ".join(f"0x{w}" for w in warns))
        print("  Rule: a SetMaterial is only consumed by a draw if LIGHTING=1 at that draw and no"
              " rebind happened in between. Confirm with `break exec` on the device SetMaterial impl.")
    return 0


# --------------------------------------------------------------------------
# Live-process reader - resolve runtime-populated (.bss) values by actually
# running the client. SRO_Client.exe has no ASLR (DllCharacteristics=0,
# ImageBase=0x400000) so a static VA == its runtime address; we still rebase
# off the real module base for safety. Read-only ReadProcessMemory from
# OUTSIDE the process - no DLL injection, no patching, no on-disk changes.
# --------------------------------------------------------------------------

IMAGE_BASE_DEFAULT = 0x400000


class LiveProcess:
    """ctypes wrapper around OpenProcess / ReadProcessMemory / module enumeration,
    with an optional launch+terminate lifecycle. Windows-only, read-only."""
    PROCESS_ACCESS = 0x0010 | 0x0400 | 0x0001 | 0x00100000  # VM_READ|QUERY_INFO|TERMINATE|SYNC
    TH32CS_SNAPMODULE = 0x8
    TH32CS_SNAPMODULE32 = 0x10

    def __init__(self, pid, owned=False, popen=None):
        import ctypes
        from ctypes import wintypes
        self.pid = pid
        self.owned = owned
        self._popen = popen
        self._ct = ctypes
        self._k = ctypes.WinDLL("kernel32", use_last_error=True)
        self._k.OpenProcess.restype = wintypes.HANDLE
        self._k.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        self._k.ReadProcessMemory.restype = wintypes.BOOL
        self._k.ReadProcessMemory.argtypes = [wintypes.HANDLE, wintypes.LPCVOID,
                                              wintypes.LPVOID, ctypes.c_size_t,
                                              ctypes.POINTER(ctypes.c_size_t)]
        self._k.CreateToolhelp32Snapshot.restype = wintypes.HANDLE
        self._k.CreateToolhelp32Snapshot.argtypes = [wintypes.DWORD, wintypes.DWORD]
        self._k.CloseHandle.argtypes = [wintypes.HANDLE]
        self._k.TerminateProcess.restype = wintypes.BOOL
        self._k.TerminateProcess.argtypes = [wintypes.HANDLE, wintypes.UINT]
        self.h = self._k.OpenProcess(self.PROCESS_ACCESS, False, pid)
        if not self.h:
            raise OSError(f"OpenProcess(pid={pid}) failed (err {ctypes.get_last_error()}); "
                          f"is the process still alive / do you need admin?")

    @classmethod
    def attach(cls, pid):
        return cls(pid, owned=False)

    @classmethod
    def launch(cls, exe, extra_args=None, cwd=None):
        import subprocess
        if not os.path.exists(exe):
            raise OSError(f"exe not found: {exe}")
        si = subprocess.STARTUPINFO()
        si.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        si.wShowWindow = 4  # SW_SHOWNOACTIVATE - render without stealing focus; the caller
        #                     then shoves the window off-screen (off-screen keeps rendering,
        #                     minimise can pause D3D). The game re-shows its own window anyway.
        cmd = [exe] + (list(extra_args) if extra_args else [])
        p = subprocess.Popen(cmd, cwd=cwd or os.path.dirname(exe) or None,
                             startupinfo=si, close_fds=True)
        return cls(p.pid, owned=True, popen=p)

    def module_base(self, name_lower):
        """Runtime base of a loaded module (e.g. 'sro_client.exe'), or None if not yet
        mapped. Lets us rebase static VAs even if the loader ever relocates the image."""
        ct = self._ct
        from ctypes import wintypes

        class ME32(ct.Structure):
            _fields_ = [("dwSize", wintypes.DWORD), ("th32ModuleID", wintypes.DWORD),
                        ("th32ProcessID", wintypes.DWORD), ("GlblcntUsage", wintypes.DWORD),
                        ("ProccntUsage", wintypes.DWORD), ("modBaseAddr", ct.POINTER(ct.c_byte)),
                        ("modBaseSize", wintypes.DWORD), ("hModule", wintypes.HMODULE),
                        ("szModule", ct.c_char * 256), ("szExePath", ct.c_char * 260)]

        self._k.Module32First.argtypes = [wintypes.HANDLE, ct.POINTER(ME32)]
        self._k.Module32Next.argtypes = [wintypes.HANDLE, ct.POINTER(ME32)]
        invalid = ct.c_void_p(-1).value
        snap = self._k.CreateToolhelp32Snapshot(
            self.TH32CS_SNAPMODULE | self.TH32CS_SNAPMODULE32, self.pid)
        if not snap or snap == invalid:
            return None
        try:
            me = ME32()
            me.dwSize = ct.sizeof(ME32)
            if not self._k.Module32First(snap, ct.byref(me)):
                return None
            while True:
                if me.szModule.decode("ascii", "ignore").lower() == name_lower:
                    return ct.cast(me.modBaseAddr, ct.c_void_p).value
                if not self._k.Module32Next(snap, ct.byref(me)):
                    return None
        finally:
            self._k.CloseHandle(snap)

    def read(self, addr, n):
        ct = self._ct
        buf = ct.create_string_buffer(n)
        got = ct.c_size_t(0)
        ok = self._k.ReadProcessMemory(self.h, ct.c_void_p(addr), buf, n, ct.byref(got))
        if not ok or got.value < n:
            return None
        return buf.raw[:got.value]

    def alive(self):
        if self._popen is not None:
            return self._popen.poll() is None
        return True

    def move_offscreen(self):
        """Shove every top-level window this process owns far off-screen. Off-screen keeps
        the app RENDERING (unlike minimise, which makes many D3D apps pause the render loop,
        so per-draw values never update); the window just never appears on a monitor.
        The game calls ShowWindow on its own window after creation, overriding the launch
        STARTUPINFO hint, so we re-place it here (idempotent, call each poll). Returns #moved."""
        import ctypes
        from ctypes import wintypes
        if not hasattr(self, "_u"):
            self._u = ctypes.WinDLL("user32", use_last_error=True)
            self._u.GetWindowThreadProcessId.argtypes = [wintypes.HWND,
                                                         ctypes.POINTER(wintypes.DWORD)]
            self._u.IsWindowVisible.argtypes = [wintypes.HWND]
            self._u.SetWindowPos.argtypes = [wintypes.HWND, wintypes.HWND, ctypes.c_int,
                                             ctypes.c_int, ctypes.c_int, ctypes.c_int,
                                             wintypes.UINT]
            self._u.EnumWindows.argtypes = [ctypes.c_void_p, wintypes.LPARAM]
        u = self._u
        moved = [0]
        SWP = 0x1 | 0x4 | 0x10  # NOSIZE | NOZORDER | NOACTIVATE
        WNDENUMPROC = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)

        def cb(hwnd, _lparam):
            pid = wintypes.DWORD(0)
            u.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
            if pid.value == self.pid and u.IsWindowVisible(hwnd):
                u.SetWindowPos(hwnd, 0, -32000, -32000, 0, 0, SWP)
                moved[0] += 1
            return True

        u.EnumWindows(WNDENUMPROC(cb), 0)
        return moved[0]

    def terminate(self):
        try:
            self._k.TerminateProcess(self.h, 0)
        except OSError:
            pass
        if self._popen is not None:
            try:
                self._popen.wait(timeout=5)
            except Exception:
                pass

    def close(self):
        if self.h:
            self._k.CloseHandle(self.h)
            self.h = None


# --------------------------------------------------------------------------
# Hardware-breakpoint debugger (Windows, 32-bit x86 target).
#
# Uses the Win32 debug API + CPU debug registers DR0-DR7 - NO code patching, no
# INT3 (0xCC) bytes written into the target. DebugSetProcessKillOnExit(FALSE) is
# set right after attach, so detaching (or revtool exiting) NEVER kills the
# client you attached to. This answers questions static pseudocode + read-only
# RPM can't: "what instruction WRITES this field?" (data breakpoint) and "what
# are the registers when EIP reaches here?" (execute breakpoint).
#
# x86 CONTEXT layout is identical to WOW64_CONTEXT, so the same struct works
# whether revtool's Python is 32-bit (GetThreadContext) or 64-bit debugging a
# 32-bit target (Wow64GetThreadContext).
# --------------------------------------------------------------------------

CONTEXT_i386 = 0x00010000
_CONTEXT_CONTROL = CONTEXT_i386 | 0x1
_CONTEXT_INTEGER = CONTEXT_i386 | 0x2
_CONTEXT_SEGMENTS = CONTEXT_i386 | 0x4
_CONTEXT_FLOATING_POINT = CONTEXT_i386 | 0x8
_CONTEXT_DEBUG_REGISTERS = CONTEXT_i386 | 0x10
_CONTEXT_EXTENDED_REGISTERS = CONTEXT_i386 | 0x20
CONTEXT_ALL_X86 = (_CONTEXT_CONTROL | _CONTEXT_INTEGER | _CONTEXT_SEGMENTS |
                   _CONTEXT_FLOATING_POINT | _CONTEXT_DEBUG_REGISTERS |
                   _CONTEXT_EXTENDED_REGISTERS)


def _dbg_structs():
    """Build (and cache) the ctypes structs for the debug API. Done lazily so
    importing revtool on a non-Windows box never touches Windows-only types."""
    import ctypes
    from ctypes import wintypes
    cache = getattr(_dbg_structs, "_cache", None)
    if cache is not None:
        return cache
    DWORD, LONG, WORD = wintypes.DWORD, wintypes.LONG, wintypes.WORD
    HANDLE, PVOID = wintypes.HANDLE, ctypes.c_void_p

    class FLOATING_SAVE_AREA(ctypes.Structure):
        _fields_ = [("ControlWord", DWORD), ("StatusWord", DWORD), ("TagWord", DWORD),
                    ("ErrorOffset", DWORD), ("ErrorSelector", DWORD), ("DataOffset", DWORD),
                    ("DataSelector", DWORD), ("RegisterArea", ctypes.c_byte * 80),
                    ("Cr0NpxState", DWORD)]

    class CONTEXT86(ctypes.Structure):
        _fields_ = [("ContextFlags", DWORD),
                    ("Dr0", DWORD), ("Dr1", DWORD), ("Dr2", DWORD), ("Dr3", DWORD),
                    ("Dr6", DWORD), ("Dr7", DWORD),
                    ("FloatSave", FLOATING_SAVE_AREA),
                    ("SegGs", DWORD), ("SegFs", DWORD), ("SegEs", DWORD), ("SegDs", DWORD),
                    ("Edi", DWORD), ("Esi", DWORD), ("Ebx", DWORD), ("Edx", DWORD),
                    ("Ecx", DWORD), ("Eax", DWORD),
                    ("Ebp", DWORD), ("Eip", DWORD), ("SegCs", DWORD), ("EFlags", DWORD),
                    ("Esp", DWORD), ("SegSs", DWORD),
                    ("ExtendedRegisters", ctypes.c_byte * 512)]

    class EXCEPTION_RECORD(ctypes.Structure):
        _fields_ = [("ExceptionCode", DWORD), ("ExceptionFlags", DWORD),
                    ("ExceptionRecord", PVOID), ("ExceptionAddress", PVOID),
                    ("NumberParameters", DWORD), ("ExceptionInformation", PVOID * 15)]

    class EXCEPTION_DEBUG_INFO(ctypes.Structure):
        _fields_ = [("ExceptionRecord", EXCEPTION_RECORD), ("dwFirstChance", DWORD)]

    class CREATE_THREAD_DEBUG_INFO(ctypes.Structure):
        _fields_ = [("hThread", HANDLE), ("lpThreadLocalBase", PVOID), ("lpStartAddress", PVOID)]

    class CREATE_PROCESS_DEBUG_INFO(ctypes.Structure):
        _fields_ = [("hFile", HANDLE), ("hProcess", HANDLE), ("hThread", HANDLE),
                    ("lpBaseOfImage", PVOID), ("dwDebugInfoFileOffset", DWORD),
                    ("nDebugInfoSize", DWORD), ("lpThreadLocalBase", PVOID),
                    ("lpStartAddress", PVOID), ("lpImageName", PVOID), ("fUnicode", WORD)]

    class LOAD_DLL_DEBUG_INFO(ctypes.Structure):
        _fields_ = [("hFile", HANDLE), ("lpBaseOfDll", PVOID), ("dwDebugInfoFileOffset", DWORD),
                    ("nDebugInfoSize", DWORD), ("lpImageName", PVOID), ("fUnicode", WORD)]

    class DEBUG_EVENT_U(ctypes.Union):
        _fields_ = [("Exception", EXCEPTION_DEBUG_INFO),
                    ("CreateThread", CREATE_THREAD_DEBUG_INFO),
                    ("CreateProcessInfo", CREATE_PROCESS_DEBUG_INFO),
                    ("LoadDll", LOAD_DLL_DEBUG_INFO),
                    ("raw", ctypes.c_byte * 184)]

    class DEBUG_EVENT(ctypes.Structure):
        _fields_ = [("dwDebugEventCode", DWORD), ("dwProcessId", DWORD),
                    ("dwThreadId", DWORD), ("u", DEBUG_EVENT_U)]

    class THREADENTRY32(ctypes.Structure):
        _fields_ = [("dwSize", DWORD), ("cntUsage", DWORD), ("th32ThreadID", DWORD),
                    ("th32OwnerProcessID", DWORD), ("tpBasePri", LONG),
                    ("tpDeltaPri", LONG), ("dwFlags", DWORD)]

    ns = type("DbgTypes", (), dict(
        CONTEXT86=CONTEXT86, DEBUG_EVENT=DEBUG_EVENT, THREADENTRY32=THREADENTRY32))
    _dbg_structs._cache = ns
    return ns


class Debugger:
    """Win32 hardware-breakpoint debugger for the 32-bit client. Attaches to a
    RUNNING pid, arms a CPU debug register on every thread, runs the debug loop
    until the breakpoint fires (or timeout), captures the trapped thread's
    register context, then disarms every thread and detaches WITHOUT killing the
    target (DebugSetProcessKillOnExit FALSE)."""

    DBG_CONTINUE = 0x00010002
    DBG_EXCEPTION_NOT_HANDLED = 0x80010001
    # Native codes AND their WOW64 (32-bit-on-64-bit) equivalents. A 64-bit
    # debugger attached to a 32-bit target receives STATUS_WX86_* for the debug
    # traps, not the native STATUS_* - miss this and every HW-BP hit looks like a
    # foreign exception, gets passed through, and crashes the client.
    EXCEPTION_SINGLE_STEP = 0x80000004
    EXCEPTION_BREAKPOINT = 0x80000003
    STATUS_WX86_SINGLE_STEP = 0x4000001E
    STATUS_WX86_BREAKPOINT = 0x4000001F
    SINGLE_STEP_CODES = (0x80000004, 0x4000001E)
    BREAKPOINT_CODES = (0x80000003, 0x4000001F)
    EX_EXCEPTION = 1
    EX_CREATE_THREAD = 2
    EX_CREATE_PROCESS = 3
    EX_EXIT_PROCESS = 5
    EX_LOAD_DLL = 6
    TH32CS_SNAPTHREAD = 0x4
    THREAD_ACCESS = 0x1FFFFF  # THREAD_ALL_ACCESS

    def __init__(self, pid, verbose=False):
        import ctypes
        from ctypes import wintypes
        self.pid = pid
        self.verbose = verbose
        self.ct = ctypes
        self.t = _dbg_structs()
        self.is64 = sys.maxsize > 2 ** 32
        k = ctypes.WinDLL("kernel32", use_last_error=True)
        self.k = k
        k.DebugActiveProcess.argtypes = [wintypes.DWORD]
        k.DebugActiveProcess.restype = wintypes.BOOL
        k.DebugActiveProcessStop.argtypes = [wintypes.DWORD]
        k.DebugActiveProcessStop.restype = wintypes.BOOL
        k.DebugSetProcessKillOnExit.argtypes = [wintypes.BOOL]
        k.DebugSetProcessKillOnExit.restype = wintypes.BOOL
        k.WaitForDebugEvent.argtypes = [ctypes.POINTER(self.t.DEBUG_EVENT), wintypes.DWORD]
        k.WaitForDebugEvent.restype = wintypes.BOOL
        k.ContinueDebugEvent.argtypes = [wintypes.DWORD, wintypes.DWORD, wintypes.DWORD]
        k.ContinueDebugEvent.restype = wintypes.BOOL
        k.OpenThread.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        k.OpenThread.restype = wintypes.HANDLE
        k.SuspendThread.argtypes = [wintypes.HANDLE]
        k.SuspendThread.restype = wintypes.DWORD
        k.ResumeThread.argtypes = [wintypes.HANDLE]
        k.ResumeThread.restype = wintypes.DWORD
        k.CloseHandle.argtypes = [wintypes.HANDLE]
        k.CreateToolhelp32Snapshot.argtypes = [wintypes.DWORD, wintypes.DWORD]
        k.CreateToolhelp32Snapshot.restype = wintypes.HANDLE
        k.Thread32First.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.THREADENTRY32)]
        k.Thread32Next.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.THREADENTRY32)]
        k.ReadProcessMemory.argtypes = [wintypes.HANDLE, wintypes.LPCVOID, wintypes.LPVOID,
                                        ctypes.c_size_t, ctypes.POINTER(ctypes.c_size_t)]
        k.ReadProcessMemory.restype = wintypes.BOOL
        getname = "Wow64GetThreadContext" if self.is64 else "GetThreadContext"
        setname = "Wow64SetThreadContext" if self.is64 else "SetThreadContext"
        self._get = getattr(k, getname)
        self._set = getattr(k, setname)
        self._get.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.CONTEXT86)]
        self._get.restype = wintypes.BOOL
        self._set.argtypes = [wintypes.HANDLE, ctypes.POINTER(self.t.CONTEXT86)]
        self._set.restype = wintypes.BOOL
        self.modbase = None
        self._attached = False

    def _thread_ids(self):
        ct = self.ct
        invalid = ct.c_void_p(-1).value
        snap = self.k.CreateToolhelp32Snapshot(self.TH32CS_SNAPTHREAD, 0)
        if not snap or snap == invalid:
            return []
        ids = []
        try:
            te = self.t.THREADENTRY32()
            te.dwSize = ct.sizeof(self.t.THREADENTRY32)
            ok = self.k.Thread32First(snap, ct.byref(te))
            while ok:
                if te.th32OwnerProcessID == self.pid:
                    ids.append(te.th32ThreadID)
                ok = self.k.Thread32Next(snap, ct.byref(te))
        finally:
            self.k.CloseHandle(snap)
        return ids

    @staticmethod
    def _dr7_for(kind):
        """DR7 bits for DR0 + the RW/LEN encoding. exec: RW=00 LEN=00. write:
        RW=01 LEN=11 (4 bytes). rw: RW=11 LEN=11."""
        rw_len = {"exec": (0b00, 0b00), "write": (0b01, 0b11), "rw": (0b11, 0b11)}[kind]
        rw, ln = rw_len
        return (1 << 0) | (rw << 16) | (ln << 18)  # L0 + RW0 + LEN0

    def _set_dr_handle(self, hthread, addr, kind):
        ct = self.ct
        ctx = self.t.CONTEXT86()
        ctx.ContextFlags = _CONTEXT_DEBUG_REGISTERS
        if not self._get(hthread, ct.byref(ctx)):
            return False
        if addr is None:
            ctx.Dr0 = 0
            ctx.Dr7 &= ~((1 << 0) | (0b1111 << 16))  # clear L0 + RW0/LEN0
        else:
            ctx.Dr0 = addr & 0xffffffff
            ctx.Dr6 = 0
            ctx.Dr7 = (ctx.Dr7 & ~(0b1111 << 16)) | self._dr7_for(kind)
        ctx.ContextFlags = _CONTEXT_DEBUG_REGISTERS
        return bool(self._set(hthread, ct.byref(ctx)))

    def _set_all(self, addr, kind):
        """Arm (addr!=None) or disarm (addr=None) DR0 on every thread of the
        target. Each thread is explicitly SuspendThread'd around the context
        write so it is reliable BOTH inside a debug event (threads already frozen
        - suspend just nests) AND in the detach path after the loop (threads
        running - we must freeze them or the DR write silently no-ops, leaving a
        stale breakpoint that crashes the client once it fires). Best-effort."""
        n = 0
        for tid in self._thread_ids():
            ht = self.k.OpenThread(self.THREAD_ACCESS, False, tid)
            if not ht:
                continue
            try:
                suspended = self.k.SuspendThread(ht) != 0xffffffff
                if self._set_dr_handle(ht, addr, kind):
                    n += 1
                if suspended:
                    self.k.ResumeThread(ht)
            finally:
                self.k.CloseHandle(ht)
        return n

    def _get_ctx(self, tid):
        ct = self.ct
        ht = self.k.OpenThread(self.THREAD_ACCESS, False, tid)
        if not ht:
            return None, None
        ctx = self.t.CONTEXT86()
        ctx.ContextFlags = CONTEXT_ALL_X86
        ok = self._get(ht, ct.byref(ctx))
        return (ctx if ok else None), ht

    def run(self, addr, kind, timeout_s, on_hit, hide_window=None):
        """Attach, arm DR0=addr (kind), pump the debug loop until on_hit signals
        stop or timeout. on_hit(self, ctx, addr, kind) -> None to stop, or
        (new_addr, new_kind) to re-arm (used to chain exec->write). Returns the
        number of breakpoint hits delivered to on_hit."""
        import time
        ct = self.ct
        if not self.k.DebugActiveProcess(self.pid):
            raise OSError(f"DebugActiveProcess(pid={self.pid}) failed "
                          f"(err {ct.get_last_error()}); another debugger attached, "
                          f"or insufficient rights?")
        self._attached = True
        koe = self.k.DebugSetProcessKillOnExit(False)  # detaching must NOT kill the client
        if self.verbose:
            print(f"    [dbg] attached; DebugSetProcessKillOnExit(FALSE)={bool(koe)} "
                  f"err={ct.get_last_error()}", file=sys.stderr)
        armed_addr, armed_kind = addr, kind
        evt = self.t.DEBUG_EVENT()
        end = time.time() + timeout_s
        hits = 0
        stop = False
        try:
            while not stop and time.time() < end:
                if not self.k.WaitForDebugEvent(ct.byref(evt), 1000):
                    continue  # 1s slice timeout; outer while re-checks deadline
                code = evt.dwDebugEventCode
                tid = evt.dwThreadId
                status = self.DBG_CONTINUE
                if code == self.EX_CREATE_PROCESS:
                    self.modbase = evt.u.CreateProcessInfo.lpBaseOfImage or 0
                    if evt.u.CreateProcessInfo.hFile:
                        self.k.CloseHandle(evt.u.CreateProcessInfo.hFile)
                    armed = self._set_all(armed_addr, armed_kind)
                    if self.verbose:
                        print(f"    [dbg] CREATE_PROCESS base=0x{self.modbase or 0:08x}; "
                              f"armed DR0=0x{armed_addr:08x} ({armed_kind}) on {armed} thread(s)",
                              file=sys.stderr)
                    if hide_window:
                        try:
                            hide_window()
                        except Exception:
                            pass
                elif code == self.EX_CREATE_THREAD:
                    ok = self._set_dr_handle(evt.u.CreateThread.hThread, armed_addr, armed_kind)
                    if self.verbose:
                        print(f"    [dbg] CREATE_THREAD tid={tid} arm={bool(ok)}", file=sys.stderr)
                elif code == self.EX_LOAD_DLL:
                    if evt.u.LoadDll.hFile:
                        self.k.CloseHandle(evt.u.LoadDll.hFile)
                elif code == self.EX_EXIT_PROCESS:
                    if self.verbose:
                        print("    [dbg] EXIT_PROCESS (debuggee gone)", file=sys.stderr)
                    stop = True
                elif code == self.EX_EXCEPTION:
                    exc = evt.u.Exception.ExceptionRecord.ExceptionCode & 0xffffffff
                    if self.verbose and exc not in self.BREAKPOINT_CODES:
                        print(f"    [dbg] EXCEPTION 0x{exc:08x} tid={tid} "
                              f"firstchance={evt.u.Exception.dwFirstChance}", file=sys.stderr)
                    if exc in self.BREAKPOINT_CODES:
                        status = self.DBG_CONTINUE  # initial attach breakpoint - swallow
                    elif exc in self.SINGLE_STEP_CODES:
                        # A single-step exception in a process that never sets the
                        # trap flag itself is ALWAYS one of our HW breakpoints. We
                        # must consume it (DBG_CONTINUE) - handing it back to the app
                        # would crash it. Identify the hit via DR6 (data BP) or, as a
                        # fallback, EIP==armed exec address.
                        status = self.DBG_CONTINUE
                        ctx, ht = self._get_ctx(tid)
                        eip = ctx.Eip & 0xffffffff if ctx else 0
                        is_ours = ctx is not None and (
                            (ctx.Dr6 & 0xF) or (armed_kind == "exec" and eip == (armed_addr & 0xffffffff)))
                        if self.verbose:
                            print(f"    [dbg] SINGLE_STEP tid={tid} "
                                  f"Dr6=0x{(ctx.Dr6 if ctx else 0):08x} eip=0x{eip:08x} "
                                  f"ours={is_ours}", file=sys.stderr)
                        if is_ours:
                            hits += 1
                            prev_kind = armed_kind
                            result = on_hit(self, ctx, armed_addr, armed_kind)
                            if result is None:
                                self._set_all(None, None)
                                stop = True
                            else:
                                armed_addr, armed_kind = result
                                self._set_all(None, None)
                                self._set_all(armed_addr, armed_kind)
                                # Re-firing an EXEC bp at the SAME EIP would trap on
                                # the very same instruction again (it triggers BEFORE
                                # execution). Set the Resume Flag so the CPU runs one
                                # instruction before the instruction-bp re-checks.
                                if prev_kind == "exec" and ht is not None:
                                    rf = self.t.CONTEXT86()
                                    rf.ContextFlags = _CONTEXT_CONTROL
                                    if self._get(ht, ct.byref(rf)):
                                        rf.EFlags |= 0x10000
                                        rf.ContextFlags = _CONTEXT_CONTROL
                                        self._set(ht, ct.byref(rf))
                        if ht:
                            self.k.CloseHandle(ht)
                    else:
                        status = self.DBG_EXCEPTION_NOT_HANDLED
                self.k.ContinueDebugEvent(evt.dwProcessId, tid, status)
                evt = self.t.DEBUG_EVENT()
        finally:
            try:
                self._set_all(None, None)
            except Exception:
                pass
            if self._attached:
                self.k.DebugActiveProcessStop(self.pid)
                self._attached = False
        return hits

    def read(self, addr, n):
        ct = self.ct
        # use a fresh OpenProcess-style read via the debuggee handle isn't kept;
        # callers pass a LiveProcess for reads. This helper exists for symmetry.
        raise NotImplementedError


def _ctx_regs(ctx):
    """Pull the general-purpose + control registers out of a CONTEXT86 into a
    plain dict (all masked to 32-bit)."""
    return {
        "eax": ctx.Eax & 0xffffffff, "ebx": ctx.Ebx & 0xffffffff,
        "ecx": ctx.Ecx & 0xffffffff, "edx": ctx.Edx & 0xffffffff,
        "esi": ctx.Esi & 0xffffffff, "edi": ctx.Edi & 0xffffffff,
        "ebp": ctx.Ebp & 0xffffffff, "esp": ctx.Esp & 0xffffffff,
        "eip": ctx.Eip & 0xffffffff, "eflags": ctx.EFlags & 0xffffffff,
    }


def _print_runtime(con, base_va, words, modbase, elapsed, observe_subj, author_args, conn_commit):
    delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
    rebase = f"module base 0x{modbase:08x} (delta {delta:+#x})" if modbase \
        else "module base not found - assuming no relocation (delta 0)"
    import struct as _struct
    print(f"runtime 0x{base_va:08x}  read after {elapsed:.1f}s  [{rebase}]")
    first_obs = None
    for i, w in enumerate(words):
        va = base_va + i * 4
        line = f"  +0x{i*4:<3x} 0x{va:08x} = 0x{w:08x}"
        if IMAGE_BASE_DEFAULT <= w < IMAGE_BASE_DEFAULT + 0x4000000:
            name, tag = resolve_addr(con, w)
            line += f"  ->  {name}  {tag}"
            obs = f"0x{w:08x}"
        else:
            fval = _struct.unpack("<f", _struct.pack("<I", w))[0]
            si = w - 0x100000000 if w >= 0x80000000 else w
            sane = (fval == 0.0) or (1e-4 < abs(fval) < 1e9)
            fstr = f"{fval:.6g}" if sane else "denorm/NaN"
            line += f"   int={si}  float={fstr}"
            obs = fstr if sane else str(si)
        if i == 0:
            first_obs = obs
        print(line)
    if observe_subj:
        author = get_author(author_args)
        val = first_obs if first_obs is not None else f"0x{words[0]:08x}"
        con.execute("INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
                    (observe_subj, val, "revtool runtime", author, now_iso()))
        results = reconcile_assertions(con, observe_subj, val, "revtool runtime", author)
        con.commit()
        print(f"  observed {observe_subj} = {val}")
        for r in results:
            print(f"    assert #{r['id']}: {r['verdict']}  ({r['detail']})")
    return 0


def _watch_float(w):
    import struct as _struct
    f = _struct.unpack("<f", _struct.pack("<I", w))[0]
    return f if (f == 0.0 or 1e-4 < abs(f) < 1e9) else None


def _print_watch(con, base_va, count, samples, modbase, args):
    """Summarise what each offset DID across the whole sampling window - so per-frame /
    per-draw values that are 0 most of the time (and nonzero only mid-draw) are still seen,
    without having to snapshot the exact instant."""
    delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
    rebase = f"module base 0x{modbase:08x} (delta {delta:+#x})" if modbase \
        else "module base not found (delta 0)"
    n = len(samples)
    print(f"watch 0x{base_va:08x}  {n} samples / {args.timeout:.0f}s @ {args.interval:.0f}ms  [{rebase}]")
    if n == 0:
        print("  (no successful reads - process gone or address unmapped)")
        return 1

    def fmt(w):
        if IMAGE_BASE_DEFAULT <= w < IMAGE_BASE_DEFAULT + 0x4000000:
            name, _tag = resolve_addr(con, w)
            return name
        fv = _watch_float(w)
        return f"{fv:.6g}" if fv is not None else f"0x{w:08x}"

    first_obs = None
    for i in range(count):
        vals = [s[1][i] for s in samples]
        nz = sum(1 for v in vals if v != 0)
        distinct = []
        for v in vals:
            if v not in distinct:
                distinct.append(v)
        first_nz = next((s[0] for s in samples if s[1][i] != 0), None)
        last = vals[-1]
        shown = ", ".join(fmt(v) for v in distinct[:6])
        more = "" if len(distinct) <= 6 else f" +{len(distinct) - 6} more"
        fnz = f"first@{first_nz:.1f}s" if first_nz is not None else "NEVER nonzero"
        print(f"  +0x{i * 4:<3x} nonzero {nz}/{n}  distinct {len(distinct)} [{shown}{more}]  "
              f"{fnz}  last={fmt(last)}")
        if i == 0:
            first_obs = fmt(last)
    if args.observe and first_obs is not None:
        author = get_author(args)
        con.execute("INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
                    (args.observe, first_obs, "revtool runtime --watch", author, now_iso()))
        results = reconcile_assertions(con, args.observe, first_obs, "revtool runtime --watch", author)
        con.commit()
        print(f"  observed {args.observe} = {first_obs} (last sample)")
        for r in results:
            print(f"    assert #{r['id']}: {r['verdict']}  ({r['detail']})")
    return 0


def _running_pids_by_name(name):
    """Return the PIDs of every running process whose image name == `name`
    (case-insensitive, e.g. "sro_client.exe"). Windows-only; uses the Toolhelp
    process snapshot so it never spawns a child or kills anything. Empty list on
    any failure (best-effort guard, never fatal)."""
    if sys.platform != "win32":
        return []
    import ctypes
    from ctypes import wintypes

    TH32CS_SNAPPROCESS = 0x00000002
    INVALID_HANDLE_VALUE = ctypes.c_void_p(-1).value

    class PROCESSENTRY32(ctypes.Structure):
        _fields_ = [
            ("dwSize", wintypes.DWORD),
            ("cntUsage", wintypes.DWORD),
            ("th32ProcessID", wintypes.DWORD),
            ("th32DefaultHeapID", ctypes.POINTER(ctypes.c_ulong)),
            ("th32ModuleID", wintypes.DWORD),
            ("cntThreads", wintypes.DWORD),
            ("th32ParentProcessID", wintypes.DWORD),
            ("pcPriClassBase", ctypes.c_long),
            ("dwFlags", wintypes.DWORD),
            ("szExeFile", ctypes.c_char * 260),
        ]

    k32 = ctypes.windll.kernel32
    snap = k32.CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)
    if snap == INVALID_HANDLE_VALUE or snap == -1:
        return []
    pids = []
    try:
        entry = PROCESSENTRY32()
        entry.dwSize = ctypes.sizeof(PROCESSENTRY32)
        ok = k32.Process32First(snap, ctypes.byref(entry))
        target = name.lower()
        while ok:
            exe = entry.szExeFile.decode("latin-1", "ignore").lower()
            if exe == target:
                pids.append(int(entry.th32ProcessID))
            ok = k32.Process32Next(snap, ctypes.byref(entry))
    finally:
        k32.CloseHandle(snap)
    return pids


def cmd_runtime(args, con):
    """Resolve a runtime-populated value by running the client and reading its memory.

    The .bss/runtime case `deref` can't see (vtable slot, current-process pointer, a
    function pointer like SWorld+0x14): launch SRO_Client.exe minimised in the
    background, poll the address until it is set & stable (that wait IS the lifecycle:
    splash -> title), resolve the value to a (possibly renamed) function, then kill it.
    """
    if sys.platform != "win32":
        print("error: `runtime` is Windows-only (uses ReadProcessMemory)", file=sys.stderr)
        return 1
    import struct
    import time
    exe = args.exe or get_binary(con, args)
    modname = os.path.basename(exe).lower()

    # `--list-clients`: pure discovery. Snapshot running processes by image name and print
    # their PIDs so the caller can pick a `--pid` to attach to. Never launches/attaches/kills,
    # so it can never hang on warmup or crash a live client.
    if getattr(args, "list_clients", False):
        pids = _running_pids_by_name(modname)
        if not pids:
            print(f"no running {modname} (nothing to attach to).")
        else:
            print(f"running {modname}: {len(pids)} instance(s)")
            for p in pids:
                print(f"  pid {p}   ->  read in place:  py -3 scripts/re/revtool/revtool.py runtime <addr> --pid {p}")
        return 0

    if args.addr is None:
        print("error: addr is required (or pass --list-clients to just list running clients)",
              file=sys.stderr)
        return 1
    try:
        base_va = int(args.addr, 16)
    except ValueError:
        print(f"error: bad address {args.addr!r}", file=sys.stderr)
        return 1
    count = max(1, args.count)

    # `--expect code` waits for the first dword to be a real pointer INTO the module
    # (a function pointer), not an early-init transient / heap value. Needs the PE to
    # know the module's mapped range.
    in_image = None
    if args.expect == "code":
        pe = open_pe(con, args)
        if pe is None:
            return 1
        in_image = pe.in_image

    def settled(words):
        first = words[0]
        if args.expect == "code":
            return in_image(first)
        if args.expect == "nonzero":
            return first != 0
        return True  # any

    if args.pid:
        try:
            lp = LiveProcess.attach(args.pid)
        except OSError as e:
            print(f"error: {e}", file=sys.stderr)
            return 1
        owned = False
        print(f"attached to pid {args.pid} ({modname})")
    else:
        if not os.path.exists(exe):
            print(f"error: exe not found: {exe}\n"
                  f"  set it once: revtool config binary <path>, or pass --exe", file=sys.stderr)
            return 1
        # A second instance of the client crashes at splash (the first holds a
        # single-instance lock / shared resource), which previously surfaced as the
        # confusing "exited during warmup". Detect an already-running client up front
        # and refuse to launch a doomed second one. We do NOT kill it - the user may be
        # using it - we just tell them how to proceed.
        existing = _running_pids_by_name(modname)
        if existing:
            pid_list = ", ".join(str(p) for p in existing)
            print(f"error: {modname} is ALREADY running (pid {pid_list}).\n"
                  f"  Launching a second instance crashes at splash, so revtool will not do it.\n"
                  f"  -> either close that client, then re-run; or read from it in place without\n"
                  f"     launching/killing:  py -3 scripts/re/revtool/revtool.py runtime {args.addr} --pid {existing[0]}",
                  file=sys.stderr)
            return 1
        try:
            lp = LiveProcess.launch(exe, args.args.split() if args.args else None)
        except OSError as e:
            print(f"error: launch failed: {e}", file=sys.stderr)
            return 1
        owned = True
        hidden = "hidden off-screen" if args.hide else "on-screen (--no-hide)"
        print(f"launched {os.path.basename(exe)} (pid {lp.pid}, {hidden}) - "
              f"{'watching' if args.watch else 'polling'} 0x{base_va:08x} up to {args.timeout}s ...")

    try:
        hide_on = owned and args.hide

        def tame():
            if hide_on:
                try:
                    lp.move_offscreen()
                except OSError:
                    pass

        if hide_on:
            # catch the window the instant it appears and shove it off-screen, so it barely
            # flashes (tight loop), then keep enforcing in the warmup/poll loops below.
            t_end = time.time() + 4.0
            while time.time() < t_end:
                if lp.alive() and lp.move_offscreen() > 0:
                    break
                time.sleep(0.03)
        if args.warmup > 0:
            # let the client get past splash/init before the first read, so we don't
            # latch onto constructor-time junk. Bail early if it dies during warmup.
            print(f"warming up {args.warmup:.0f}s before first read ...")
            warm_end = time.time() + args.warmup
            while time.time() < warm_end:
                if owned and not lp.alive():
                    print("error: the client exited during warmup (needs launch args, or "
                          "crashed at splash). Try --pid against a client you started.",
                          file=sys.stderr)
                    return 1
                tame()
                # warm_end - now can go negative between the loop test and here
                time.sleep(max(0.0, min(0.5, warm_end - time.time())))

        nbytes = count * 4

        if args.watch:
            # sample over the WHOLE window and report what each offset does - removes the
            # "guess the exact instant" fragility for per-frame / per-draw values.
            modbase = None
            samples = []   # (elapsed, words)
            wstart = time.time()
            wdeadline = wstart + args.timeout
            while time.time() < wdeadline:
                if owned and not lp.alive():
                    print("error: the client exited during watch.", file=sys.stderr)
                    break
                if modbase is None:
                    modbase = lp.module_base(modname)
                delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
                data = lp.read(base_va + delta, nbytes)
                if data and len(data) == nbytes:
                    samples.append((time.time() - wstart, struct.unpack(f"<{count}I", data)))
                tame()
                time.sleep(args.interval / 1000.0)
            return _print_watch(con, base_va, count, samples, modbase, args)

        start = time.time()
        deadline = start + args.timeout
        modbase = None
        prev = None
        last_seen = None       # last successfully-read value, even if not 'settled'
        stable_hits = 0
        need_stable = max(1, args.samples)
        while time.time() < deadline:
            tame()
            if owned and not lp.alive():
                print("error: the client exited before the value was set "
                      "(needs launch args, or crashed at splash). Try --pid against a "
                      "client you started, or pass --args.", file=sys.stderr)
                return 1
            if modbase is None:
                modbase = lp.module_base(modname)
            delta = (modbase - IMAGE_BASE_DEFAULT) if modbase else 0
            data = lp.read(base_va + delta, nbytes)
            if data and len(data) == nbytes:
                words = struct.unpack(f"<{count}I", data)
                last_seen = words
                if settled(words):
                    # require the value to hold steady across N samples so we don't
                    # latch onto an early-init transient
                    stable_hits = stable_hits + 1 if words == prev else 1
                    prev = words
                    if stable_hits >= need_stable:
                        return _print_runtime(con, base_va, words, modbase,
                                              time.time() - start, args.observe, args, True)
                else:
                    prev = None
                    stable_hits = 0
            time.sleep(args.interval / 1000.0)
        if prev:
            print(f"note: value seen but not stable for {need_stable} samples within "
                  f"{args.timeout}s; last read:")
            return _print_runtime(con, base_va, prev, modbase, time.time() - start,
                                  args.observe, args, True)
        if last_seen is not None and last_seen[0] != 0:
            print(f"note: 0x{base_va:08x} was non-zero ({last_seen[0]:#010x}) but never matched "
                  f"--expect {args.expect} within {args.timeout}s.\n"
                  f"  (e.g. --expect code wants a pointer INTO the module; this value is outside "
                  f"it - a heap/object pointer, not a .text function.) Last live read:")
            return _print_runtime(con, base_va, last_seen, modbase, time.time() - start,
                                  args.observe, args, True)
        print(f"timed out after {args.timeout}s: 0x{base_va:08x} still 0.\n"
              f"  the process likely hasn't reached the lifecycle stage that sets it "
              f"(or needs proper launch args). Increase --timeout, or attach with --pid\n"
              f"  once the client is at the scene where the value is live.", file=sys.stderr)
        return 1
    finally:
        if owned and not args.keep:
            lp.terminate()
            print(f"terminated pid {lp.pid}")
        elif owned and args.keep:
            print(f"left pid {lp.pid} running (--keep)")
        lp.close()


def _parse_read_expr(expr):
    """Parse a capture expression into (kind, reg, off, vtype).
      reg                 -> ('reg', reg, 0, None)            value of a register
      reg+0x90            -> ('addr', reg, 0x90, None)        computed address
      [reg+0x90]:f32      -> ('mem', reg, 0x90, 'f32')        4 bytes read at addr
    vtype in {f32,i32,u32,hex} (mem only; default hex)."""
    e = expr.strip().lower()
    vtype = None
    if ":" in e:
        e, vtype = e.rsplit(":", 1)
        e = e.strip()
        vtype = vtype.strip()
    deref = e.startswith("[") and e.endswith("]")
    if deref:
        e = e[1:-1].strip()
    if "+" in e:
        reg, offs = e.split("+", 1)
        reg = reg.strip()
        off = int(offs.strip(), 16) if offs.strip().lower().startswith("0x") else int(offs.strip())
    elif "-" in e[1:]:
        reg, offs = e[0] + e[1:].split("-", 1)[0], e[1:].split("-", 1)[1]
        reg = reg.strip()
        off = -(int(offs.strip(), 16) if offs.strip().lower().startswith("0x") else int(offs.strip()))
    else:
        reg, off = e, 0
    regs = ("eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp", "eip", "eflags")
    if reg not in regs:
        raise ValueError(f"unknown register {reg!r} in expr {expr!r}")
    if deref:
        return ("mem", reg, off, vtype or "hex")
    return ("addr" if off else "reg", reg, off, vtype)


def _fmt_mem(raw, vtype):
    import struct as _s
    if raw is None or len(raw) < 4:
        return "<unreadable>"
    w = _s.unpack("<I", raw[:4])[0]
    if vtype == "f32":
        f = _s.unpack("<f", raw[:4])[0]
        return f"{f:.6g}"
    if vtype == "i32":
        return str(w - 0x100000000 if w >= 0x80000000 else w)
    if vtype == "u32":
        return str(w)
    return f"0x{w:08x}"


def _eval_reads(exprs, regs, lp):
    """Render each capture expression against a register snapshot + live reads."""
    out = []
    for raw_expr in exprs:
        kind, reg, off, vtype = _parse_read_expr(raw_expr)
        rv = regs[reg]
        if kind == "reg":
            out.append(f"{reg}=0x{rv:08x}")
        elif kind == "addr":
            out.append(f"{raw_expr}=0x{(rv + off) & 0xffffffff:08x}")
        else:  # mem
            addr = (rv + off) & 0xffffffff
            data = lp.read(addr, 4)
            out.append(f"{raw_expr}=@0x{addr:08x} {_fmt_mem(data, vtype)}")
    return out


def cmd_break(args, con):
    """Hardware-breakpoint capture on a RUNNING client (never launches/kills it).

      exec  <VA>            trap when EIP reaches VA; dump registers (+ --read).
      write <ADDR>          trap when 4 bytes at ADDR are written; report the
                            writer instruction (EIP) + the new value.
      field <VA> <reg> <off> two-phase: exec-trap at VA, read <reg>+<off> as the
                            instance field address, then arm a WRITE breakpoint on
                            it and report the instruction that computes/stores it.
    Uses CPU debug registers (DR0-DR7) - no code patching - and detaches with
    KillOnExit=FALSE so the client keeps running afterward."""
    if sys.platform != "win32":
        print("error: `break` is Windows-only (uses the Win32 debug API)", file=sys.stderr)
        return 1
    if not args.pid:
        print("error: `break` requires --pid (it attaches to a client you already "
              "have at the right scene; it never launches one)", file=sys.stderr)
        return 1
    if args.mode == "field" and (not args.reg or args.off is None):
        print("error: field mode needs <reg> and <off>, e.g. "
              "`break field 0x8b1398 ecx 0x90 --pid PID`", file=sys.stderr)
        return 1
    if args.mode == "field":
        if args.reg.lower() not in ("eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp"):
            print(f"error: bad base register {args.reg!r}", file=sys.stderr)
            return 1
        args.reg = args.reg.lower()

    def parse_addr(s):
        return int(s, 16) if s.lower().startswith("0x") else int(s, 16)

    try:
        primary = parse_addr(args.addr)
    except ValueError:
        print(f"error: bad address {args.addr!r}", file=sys.stderr)
        return 1
    reads = [e for e in (args.read.split(",") if args.read else []) if e.strip()]
    # validate expressions up front (clear error before we attach a debugger)
    for e in reads:
        try:
            _parse_read_expr(e)
        except ValueError as ex:
            print(f"error: {ex}", file=sys.stderr)
            return 1

    lp = LiveProcess.attach(args.pid)
    modname = os.path.basename(args.exe or get_binary(con, args)).lower()
    modbase = lp.module_base(modname) or IMAGE_BASE_DEFAULT
    delta = modbase - IMAGE_BASE_DEFAULT

    def rebase_in(va):   # static VA -> live address
        return (va + delta) & 0xffffffff

    def rebase_out(live):  # live code address -> static VA (for symbol lookup)
        return (live - delta) & 0xffffffff

    def describe_eip(live_eip):
        sva = rebase_out(live_eip)
        name, tag = resolve_addr(con, sva)
        return f"0x{live_eip:08x} (static 0x{sva:08x}: {name} {tag})"

    state = {"phase": 1, "captured": [], "remaining": max(1, args.hits)}

    def _writer_line(regs, target):
        newv = lp.read(target, 4)
        print(f"  [write hit] writer EIP = {describe_eip(regs['eip'])}")
        print(f"    field @0x{target:08x} now = {_fmt_mem(newv, 'f32')} (f32) / "
              f"{_fmt_mem(newv, 'hex')}")
        print(f"    regs: eax=0x{regs['eax']:08x} ecx=0x{regs['ecx']:08x} "
              f"edx=0x{regs['edx']:08x} esi=0x{regs['esi']:08x} edi=0x{regs['edi']:08x} "
              f"ebp=0x{regs['ebp']:08x}")

    def on_hit(dbg, ctx, armed_addr, armed_kind):
        regs = _ctx_regs(ctx)
        if args.mode == "field" and state["phase"] == 1:
            base = regs[args.reg]
            off = int(args.off, 16) if args.off.lower().startswith("0x") else int(args.off)
            target = (base + off) & 0xffffffff
            cur = lp.read(target, 4)
            print(f"  [exec hit] {describe_eip(regs['eip'])}")
            print(f"    {args.reg}=0x{base:08x}  +0x{off:x} -> field @0x{target:08x}"
                  f"  current={_fmt_mem(cur, 'f32')} (f32) / {_fmt_mem(cur, 'hex')}")
            print(f"    arming WRITE breakpoint on 0x{target:08x} - waiting for the writer ...")
            state["phase"] = 2
            state["target"] = target
            return (target, "write")
        if args.mode == "field":  # phase 2 - the write(s) fired
            target = state["target"]
            _writer_line(regs, target)
            state["captured"].append(regs)
            state["remaining"] -= 1
            if state["remaining"] > 0:
                return (target, "write")  # keep watching for the next writer
            return None
        # write single-shot / multi-hit mode
        if args.mode == "write":
            _writer_line(regs, primary)
            state["captured"].append(regs)
            state["remaining"] -= 1
            if state["remaining"] > 0:
                return (primary, "write")
            return None
        # exec mode
        print(f"  [exec hit] {describe_eip(regs['eip'])}")
        print(f"    eax=0x{regs['eax']:08x} ebx=0x{regs['ebx']:08x} "
              f"ecx=0x{regs['ecx']:08x} edx=0x{regs['edx']:08x}")
        print(f"    esi=0x{regs['esi']:08x} edi=0x{regs['edi']:08x} "
              f"ebp=0x{regs['ebp']:08x} esp=0x{regs['esp']:08x}")
        if reads:
            for line in _eval_reads(reads, regs, lp):
                print(f"    {line}")
        state["captured"].append(regs)
        state["remaining"] -= 1
        if state["remaining"] > 0:
            return (armed_addr, "exec")
        return None

    # Arm address: code VAs are rebased; write targets in `write` mode are taken
    # as absolute (heap) unless they look like a module VA.
    if args.mode == "write":
        is_code = IMAGE_BASE_DEFAULT <= primary < IMAGE_BASE_DEFAULT + 0x4000000
        arm_addr, arm_kind = (rebase_in(primary) if is_code else primary), "write"
    else:  # exec / field both start with an EXEC bp on a code VA
        arm_addr, arm_kind = rebase_in(primary), "exec"

    existing = _running_pids_by_name(modname)
    print(f"attaching debugger to pid {args.pid} ({modname}); "
          f"module base 0x{modbase:08x} (delta {delta:+#x})")
    print(f"  arming {arm_kind.upper()} breakpoint @0x{arm_addr:08x}"
          f"{'  (rebased from ' + args.addr + ')' if delta and arm_kind != 'write' else ''}; "
          f"timeout {args.timeout:.0f}s ... (Ctrl-C-safe: detaches without killing)")
    dbg = Debugger(args.pid, verbose=args.verbose)
    try:
        hits = dbg.run(arm_addr, arm_kind, args.timeout, on_hit)
    except OSError as e:
        print(f"error: {e}", file=sys.stderr)
        lp.close()
        return 1
    finally:
        lp.close()
    if not hits:
        print(f"  no breakpoint hit within {args.timeout:.0f}s "
              f"(is the client at the scene that executes this code? "
              f"is the address right?)")
        return 1
    print(f"  detached cleanly ({hits} hit(s)); client still running.")
    return 0


def _human_bytes(n):
    """`115553624` -> `110.2 MiB (115,553,624 bytes)` for status readouts. The raw
    byte count is kept because it is the exact validation target for a regenerated
    dump (see revtool_README.md Provisioning)."""
    units = ["bytes", "KiB", "MiB", "GiB"]
    size = float(n)
    unit = 0
    while size >= 1024 and unit < len(units) - 1:
        size /= 1024
        unit += 1
    if unit == 0:
        return f"{n:,} bytes"
    return f"{size:.1f} {units[unit]} ({n:,} bytes)"


def evidence_artifact_paths(con, args):
    # Derived analyzer exports live with their target in the research tree
    # (targets/<sha256>/derived), never in a temp directory a cleanup can take.
    current = os.path.join(RECONSTRUCT_DIR, "targets", TARGET_SHA256, "derived")
    progress = current
    return {
        "target": get_binary(con, args),
        "binary_ninja_hlil": get_dump(con, args),
        "binary_ninja_functions": os.path.join(current, "binary-ninja-function-spans.csv"),
        "binary_ninja_calls": os.path.join(current, "binary-ninja-direct-calls.csv"),
        "binary_ninja_coverage": os.path.join(current, "binary-ninja-text-coverage.json"),
        "rizin_function_index": os.path.join(current, "function-index.json"),
        "rizin_project": os.path.join(current, "sro_client.rzdb"),
        "reconciled_function_index": os.path.join(current, "function-index-reconciled.json"),
        "progress_data": os.path.join(progress, "progress.json"),
        "ui_family_plan": os.path.join(progress, "plan.json"),
        "exact_provider_manifest": os.path.join(current, "provider-manifest.json"),
    }


# Evidence a contributor can lack without the graph being wrong. The HLIL dump
# is Binary Ninja output nobody can redistribute, and the byte treemap and UI
# family plan came from the retired exact-rebuild project (its C++ replacement
# proofs and pinned MSVC toolchains). Each is still checked when present; when
# absent, doctor reports it as absent instead of failing the whole graph.
OPTIONAL_EVIDENCE = ("binary_ninja_hlil", "progress_data", "ui_family_plan")


def artifact_lock_record(name, path):
    record = {
        "path": os.path.relpath(os.path.abspath(path), WORKSPACE_ROOT).replace("\\", "/"),
        "sha256": sha256_file(path),
        "size": os.path.getsize(path),
    }
    if name == "binary_ninja_hlil":
        record.update({
            "producer": "Binary Ninja HLIL linear-view export",
            "producer_version": "legacy-unrecorded",
            # This 110 MiB legacy linear-view dump predates the structured
            # exporter below.  Claiming that exporter could reproduce it would
            # turn a hash pin into false provenance.
            "recipe": None,
            "recipe_status": "legacy-unrecorded-no-exact-regeneration-recipe",
            "limitations": "semantic hypothesis snapshot; not byte/function ground truth",
        })
    elif name.startswith("binary_ninja"):
        record.update({
            "producer": "Binary Ninja structured snapshot",
            "recipe": "scripts/re/revtool/export_binary_ninja_text_coverage.py",
        })
    elif name.startswith("rizin"):
        record["producer"] = "Rizin analysis snapshot"
    elif name == "reconciled_function_index":
        record.update({
            "producer": "cross-analyzer entry reconciler",
            "recipe": "scripts/re/revtool/reconcile_function_index.py",
        })
    elif name == "progress_data":
        record.update({
            "producer": "proof-backed byte treemap generator",
            "recipe": "research/tools/re/cpp-replacement-proof/exact_progress_treemap.py",
        })
    elif name == "ui_family_plan":
        record.update({
            "producer": "RTTI UI family planner",
            "recipe": "research/tools/re/cpp-replacement-proof/ui_family_plan.py",
        })
    elif name == "exact_provider_manifest":
        record["producer"] = "C++ replacement proof gate"
    else:
        record["producer"] = "retail PE"
    return record


def validate_evidence_graph(con, args):
    """Return stale or cross-target evidence relationships.

    Hashing every file independently does not prove the files belong together.
    The derived artifacts therefore carry their input hashes, and this gate
    verifies the complete graph before ``--refresh-lock`` is allowed to bless a
    new snapshot.
    """
    paths = evidence_artifact_paths(con, args)
    issues = []

    def document(name):
        path = paths[name]
        try:
            with open(path, encoding="utf-8") as f:
                return json.load(f)
        except (OSError, json.JSONDecodeError) as exc:
            issues.append(f"{name}: {exc}")
            return {}

    def expect(label, actual, expected):
        if str(actual or "").lower() != str(expected or "").lower():
            issues.append(f"{label}: {actual!r} != {expected!r}")

    present = {name for name, path in paths.items() if path and os.path.isfile(path)}
    for name, path in paths.items():
        if name not in present and name not in OPTIONAL_EVIDENCE:
            issues.append(f"{name}: missing {path}")
    if issues:
        return issues

    target_hash = sha256_file(paths["target"])
    expect("target", target_hash, TARGET_SHA256)

    coverage = document("binary_ninja_coverage")
    expect("binary_ninja_coverage.schema", coverage.get("schema"),
           "sro-binary-ninja-text-coverage-v3")
    expect("binary_ninja_coverage.target", coverage.get("binary_sha256"), target_hash)
    expect("binary_ninja_coverage.spans",
           coverage.get("function_spans_sha256"),
           sha256_file(paths["binary_ninja_functions"]))
    expect("binary_ninja_coverage.calls",
           coverage.get("direct_calls_sha256"),
           sha256_file(paths["binary_ninja_calls"]))

    rizin_index = document("rizin_function_index")
    expect("rizin_function_index.target", rizin_index.get("target_sha256"), target_hash)
    expect("rizin_function_index.project", rizin_index.get("rizin_project_sha256"),
           sha256_file(paths["rizin_project"]))

    reconciled = document("reconciled_function_index")
    expect("reconciled_function_index.target", reconciled.get("target_sha256"), target_hash)
    reconciled_sources = reconciled.get("sources") or {}
    for key, source_name in (
        ("rizin_index_sha256", "rizin_function_index"),
        ("binary_ninja_functions_sha256", "binary_ninja_functions"),
        ("binary_ninja_coverage_sha256", "binary_ninja_coverage"),
        ("provider_manifest_sha256", "exact_provider_manifest"),
        ("rizin_project_sha256", "rizin_project"),
    ):
        expect(f"reconciled_function_index.{key}", reconciled_sources.get(key),
               sha256_file(paths[source_name]))

    ledger_path = os.path.abspath(args.ledger or default_sync_path(args.db))
    if "progress_data" not in present:
        progress = None
    else:
        progress = document("progress_data")
    if progress is not None:
        _validate_progress(progress, paths, ledger_path, target_hash, expect)
    if "ui_family_plan" in present:
        _validate_plan(document("ui_family_plan"), paths, ledger_path, target_hash, expect)
    return issues


def _validate_progress(progress, paths, ledger_path, target_hash, expect):
    expect("progress_data.target", progress.get("targetSha256"), target_hash)
    progress_sources = progress.get("sources") or {}
    for key, source_name in (
        ("functionIndexSha256", "reconciled_function_index"),
        ("providerManifestSha256", "exact_provider_manifest"),
        ("binaryNinjaCoverageSha256", "binary_ninja_coverage"),
        ("rizinProjectSha256", "rizin_project"),
    ):
        expect(f"progress_data.{key}", progress_sources.get(key),
               sha256_file(paths[source_name]))
    if os.path.isfile(ledger_path):
        expect("progress_data.revtoolLedgerSha256",
               progress_sources.get("revtoolLedgerSha256"), sha256_file(ledger_path))


def _validate_plan(plan, paths, ledger_path, target_hash, expect):
    expect("ui_family_plan.schema", plan.get("schema"), "sro-ui-family-plan-v7")
    expect("ui_family_plan.target", plan.get("targetSha256"), target_hash)
    plan_sources = plan.get("sources") or {}
    for key, source_name in (
        ("functionIndexSha256", "reconciled_function_index"),
        ("providerManifestSha256", "exact_provider_manifest"),
        ("rizinProjectSha256", "rizin_project"),
        ("binaryNinjaCallsSha256", "binary_ninja_calls"),
    ):
        expect(f"ui_family_plan.{key}", plan_sources.get(key),
               sha256_file(paths[source_name]))
    if os.path.isfile(ledger_path):
        expect("ui_family_plan.revtoolLedgerSha256",
               plan_sources.get("revtoolLedgerSha256"), sha256_file(ledger_path))


def build_evidence_lock(con, args):
    issues = validate_evidence_graph(con, args)
    if issues:
        raise ValueError("evidence graph is stale or cross-target:\n  " + "\n  ".join(issues))
    artifacts = {}
    for name, path in evidence_artifact_paths(con, args).items():
        if (not path or not os.path.isfile(path)) and name in OPTIONAL_EVIDENCE:
            continue
        if not path or not os.path.isfile(path):
            raise FileNotFoundError(f"required evidence artifact is missing: {name}: {path}")
        artifacts[name] = artifact_lock_record(name, path)
    if artifacts["target"]["sha256"] != TARGET_SHA256:
        raise ValueError("configured target is not the pinned SRO_Client.exe v1.150")
    return {
        "schema": EVIDENCE_LOCK_SCHEMA,
        "target_sha256": TARGET_SHA256,
        "artifacts": artifacts,
        "policy": {
            "bytes": "original PE and exact providers",
            "structure": "PE/RTTI/direct calls, then corroborated analyzers",
            "semantic_names": "provenance-tiered Revtool claims",
            "single_analyzer": "hypothesis only",
        },
    }


def cmd_doctor(args, con):
    lock_path = os.path.abspath(args.lock or DEFAULT_EVIDENCE_LOCK)
    if args.refresh_lock:
        lock = build_evidence_lock(con, args)
        with open(lock_path, "w", encoding="utf-8") as f:
            json.dump(lock, f, indent=2, ensure_ascii=False)
            f.write("\n")
        print(f"refreshed evidence lock: {lock_path}")

    checks = []
    def check(name, ok, detail):
        checks.append({"name": name, "ok": bool(ok), "detail": detail})

    check("schema", meta_get(con, "schema_version") == str(REVTOOL_SCHEMA_VERSION),
          f"db={meta_get(con, 'schema_version')} expected={REVTOOL_SCHEMA_VERSION}")
    if not os.path.isfile(lock_path):
        check("evidence-lock", False, f"missing: {lock_path}; run doctor --refresh-lock")
        lock = None
    else:
        try:
            lock = json.load(open(lock_path, encoding="utf-8"))
            check("evidence-lock-schema", lock.get("schema") == EVIDENCE_LOCK_SCHEMA,
                  str(lock.get("schema")))
        except (OSError, json.JSONDecodeError) as exc:
            check("evidence-lock", False, str(exc))
            lock = None
    if lock:
        for name, record in lock.get("artifacts", {}).items():
            path = os.path.join(WORKSPACE_ROOT, record.get("path", ""))
            if not os.path.isfile(path):
                check(f"artifact:{name}", False, f"missing: {path}")
                continue
            actual = sha256_file(path)
            check(f"artifact:{name}", actual == record.get("sha256"),
                  f"sha256={actual} locked={record.get('sha256')}")
    graph_issues = validate_evidence_graph(con, args)
    check("evidence-graph", not graph_issues,
          "all derived inputs match" if not graph_issues else "; ".join(graph_issues))
    binary = get_binary(con, args)
    if os.path.isfile(binary):
        actual_target = sha256_file(binary)
        check("target-hash", actual_target == TARGET_SHA256,
              f"sha256={actual_target}")
        check("db-target-binding", meta_get(con, "target_sha256") == actual_target,
              f"db={meta_get(con, 'target_sha256')} actual={actual_target}")
    else:
        check("target", False, f"missing: {binary}")

    # A lock over two unrelated files is not enough: validate that the
    # structured Binary Ninja coverage and span CSV are a self-consistent pair
    # exported from this exact PE.  This catches a stale CSV paired with a fresh
    # JSON file, a snapshot from another Binary Ninja database, and newline or
    # serialization drift that invalidates the embedded hash.
    evidence_paths = evidence_artifact_paths(con, args)
    coverage_path = evidence_paths["binary_ninja_coverage"]
    spans_path = evidence_paths["binary_ninja_functions"]
    calls_path = evidence_paths["binary_ninja_calls"]
    if (os.path.isfile(coverage_path) and os.path.isfile(spans_path)
            and os.path.isfile(calls_path) and os.path.isfile(binary)):
        try:
            with open(coverage_path, encoding="utf-8") as f:
                coverage = json.load(f)
            check("binary-ninja-schema",
                  coverage.get("schema") == "sro-binary-ninja-text-coverage-v3"
                  and coverage.get("exporter_version") == 3,
                  f"schema={coverage.get('schema')} exporter={coverage.get('exporter_version')}")
            check("binary-ninja-target-binding",
                  coverage.get("binary_sha256") == actual_target
                  and coverage.get("binary_size") == os.path.getsize(binary),
                  f"sha256={coverage.get('binary_sha256')} size={coverage.get('binary_size')}")
            spans_hash = sha256_file(spans_path)
            check("binary-ninja-span-binding",
                  coverage.get("function_spans_sha256") == spans_hash,
                  f"csv={spans_hash} embedded={coverage.get('function_spans_sha256')}")
            calls_hash = sha256_file(calls_path)
            check("binary-ninja-call-binding",
                  coverage.get("direct_calls_sha256") == calls_hash,
                  f"csv={calls_hash} embedded={coverage.get('direct_calls_sha256')}")
            with open(spans_path, newline="", encoding="utf-8") as f:
                span_count = sum(1 for _ in csv.DictReader(f))
            check("binary-ninja-function-count",
                  coverage.get("function_count") == span_count,
                  f"csv={span_count} metadata={coverage.get('function_count')}")
            with open(calls_path, newline="", encoding="utf-8") as f:
                call_rows = list(csv.DictReader(f))
            direct_edges = sum(1 for row in call_rows if row.get("target_address"))
            unresolved_sites = sum(1 for row in call_rows if not row.get("target_address"))
            check("binary-ninja-call-count",
                  coverage.get("direct_call_edge_count") == direct_edges
                  and coverage.get("unresolved_call_site_count") == unresolved_sites,
                  f"direct={direct_edges}/{coverage.get('direct_call_edge_count')} "
                  f"unresolved={unresolved_sites}/{coverage.get('unresolved_call_site_count')}")

            pe_image = PEImage(binary)
            text_section = next(
                (row for row in pe_image.sections if row[0] == ".text"), None)
            if text_section is None:
                raise ValueError("target PE has no .text section")
            _name, text_rva, text_virtual_size, text_raw_offset, text_raw_size = text_section
            text_bytes = pe_image.data[text_raw_offset:text_raw_offset + text_raw_size]
            expected_text_hash = hashlib.sha256(text_bytes).hexdigest()
            text_metadata_ok = (
                coverage.get("text_va") == pe_image.image_base + text_rva
                and coverage.get("text_virtual_size") == text_virtual_size
                and coverage.get("text_raw_offset") == text_raw_offset
                and coverage.get("text_size") == text_raw_size
                and coverage.get("text_sha256") == expected_text_hash
            )
            check("binary-ninja-text-binding", text_metadata_ok,
                  f"sha256={coverage.get('text_sha256')} expected={expected_text_hash}")
        except (OSError, ValueError, KeyError, json.JSONDecodeError) as exc:
            check("binary-ninja-structured-evidence", False, str(exc))
    else:
        check("binary-ninja-structured-evidence", False,
              f"missing coverage={coverage_path} spans={spans_path} calls={calls_path} "
              f"or target={binary}")
    dump = get_dump(con, args)
    if dump and os.path.isfile(dump):
        actual_dump = sha256_file(dump)
        check("db-dump-binding", meta_get(con, "dump_sha256") == actual_dump,
              f"db={meta_get(con, 'dump_sha256')} actual={actual_dump}")
        expected_functions = meta_get(con, "dump_function_count")
        actual_functions = con.execute("SELECT COUNT(*) FROM functions").fetchone()[0]
        check("db-function-index", expected_functions == str(actual_functions),
              f"db={actual_functions} indexed={expected_functions}")
    else:
        check("dump", False, f"missing: {dump}")

    ledger_path = os.path.abspath(args.ledger or default_sync_path(args.db))
    if os.path.isfile(ledger_path):
        try:
            diff = sync_diff(con, ledger_path)
            validate_ledger_meta(con, diff["meta"], ledger_path)
            check("ledger-claims", not diff["local_only"] and not diff["remote_only"]
                  and not diff["differing"],
                  f"local-only={len(diff['local_only'])} "
                  f"remote-only={len(diff['remote_only'])} differing={len(diff['differing'])}")
            check("ledger-events", not diff["local_event_only"] and not diff["remote_event_only"],
                  f"local-only={len(diff['local_event_only'])} "
                  f"remote-only={len(diff['remote_event_only'])}")
        except (OSError, ValueError) as exc:
            check("ledger", False, str(exc))
    else:
        check("ledger", False, f"missing: {ledger_path}")
    conflicts = con.execute(
        "SELECT COUNT(*) FROM sync_conflicts WHERE resolved=0").fetchone()[0]
    check("sync-conflicts", conflicts == 0, f"unresolved={conflicts}")

    result = {"schema": "sro-revtool-doctor-v1", "ok": all(c["ok"] for c in checks),
              "checks": checks}
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        for row in checks:
            print(f"{'PASS' if row['ok'] else 'FAIL'}  {row['name']}: {row['detail']}")
        print(f"doctor: {'PASS' if result['ok'] else 'FAIL'}")
    return 0 if result["ok"] else 2


def cmd_dossier(args, con):
    script = os.path.join(HERE, "evidence_dossier.py")
    command = [sys.executable, script, args.target, "--db", os.path.abspath(args.db),
               "--dump", os.path.abspath(get_dump(con, args)),
               "--binary", os.path.abspath(get_binary(con, args))]
    if args.out:
        command.extend(["--out", os.path.abspath(args.out)])
    if args.json:
        command.append("--json")
    if args.allow_stale:
        command.append("--allow-stale")
    return subprocess.run(command, cwd=str(sro_paths.REPO_ROOT)).returncode


def cmd_status(args, con):
    nf = con.execute("SELECT COUNT(*) FROM functions").fetchone()[0]
    ng = con.execute("SELECT COUNT(*) FROM globals").fetchone()[0]
    nl = con.execute("SELECT COUNT(*) FROM locals").fetchone()[0]
    nt = con.execute("SELECT COUNT(*) FROM types").fetchone()[0]
    nfld = con.execute("SELECT COUNT(*) FROM fields").fetchone()[0]
    nb = con.execute("SELECT COUNT(*) FROM bindings").fetchone()[0]
    nn = con.execute("SELECT COUNT(*) FROM notes").fetchone()[0]
    dump = get_dump(con, args)
    # DB state: revtool.db is per-machine (gitignored, binary); the function index
    # inside it is rebuilt with `index`, the overlay work syncs via the JSONL. Report
    # whether it exists and its size so a fresh clone can see it must be (re)built.
    db_present = os.path.exists(args.db)
    db_size = os.path.getsize(args.db) if db_present else 0
    print(f"db:    {args.db}  ({'present, ' + _human_bytes(db_size) if db_present else 'MISSING - run `py -3 scripts/re/revtool/revtool.py index`'})")
    # Dump state: size is a validation target for a regenerated export (see
    # revtool_README.md "Provisioning"); a present-but-truncated dump is a broken
    # export, not "no callers", so surface the byte size next to present/absent.
    if os.path.exists(dump):
        dump_size = os.path.getsize(dump)
        dump_state = "present, " + _human_bytes(dump_size) if dump_size else "present but EMPTY (0 bytes) - broken export"
    else:
        dump_state = "MISSING - see revtool_README.md Provisioning"
    print(f"dump:  {dump}  ({dump_state})")
    print(f"functions indexed: {nf}")
    print(f"global renames:    {ng}")
    print(f"local renames:     {nl}")
    print(f"types / fields:    {nt} / {nfld}")
    print(f"bindings:          {nb}")
    print(f"notes:             {nn}")
    # confidence breakdown across renameable claims
    print("confidence (global+local+field):")
    counts = {t: 0 for t in TIERS}
    for table in ("globals", "locals", "fields"):
        for tier, c in con.execute(f"SELECT tier,COUNT(*) FROM {table} GROUP BY tier"):
            counts[tier if tier in counts else "guess"] = counts.get(tier, 0) + c
    for t in TIERS:
        print(f"  {t:<10} {counts.get(t, 0)}")
    ndisp = sum(con.execute(f"SELECT COUNT(*) FROM {t} WHERE disputed=1").fetchone()[0]
                for t in ("globals", "locals", "fields"))
    print(f"  DISPUTED   {ndisp}")
    # assertions + dynamic reconciliation
    na = con.execute("SELECT COUNT(*) FROM assertions").fetchone()[0]
    nobs = con.execute("SELECT COUNT(*) FROM observations").fetchone()[0]
    print(f"assertions:        {na}  (observations: {nobs})")
    if na:
        print("  status:")
        for st in ASSERT_STATUS:
            c = con.execute("SELECT COUNT(*) FROM assertions WHERE status=?", (st,)).fetchone()[0]
            print(f"    {st:<10} {c}")
        nweak = con.execute(
            "SELECT COUNT(*) FROM assertions WHERE basis IN ('absence','inferred') "
            "AND status!='confirmed'").fetchone()[0]
        print(f"  unconfirmed weak-basis (run `review --needs-dynamic`): {nweak}")
    # investigation ledger / enforcement
    ninv = con.execute(
        "SELECT COUNT(*) FROM access_log WHERE kind IN ('viewed','xref')").fetchone()[0]
    author = get_author(args)
    my_debt = compute_debt(con, author=author)
    all_debt = compute_debt(con)
    limit = debt_limit(con)
    print(f"investigated (view/xref): {ninv}")
    print(f"  YOUR debt ({author}): {len(my_debt)}/{limit}   (this is what gates you)")
    print(f"  ALL authors' debt: {len(all_debt)}   (revtool review --debt)")
    print(f"strict enforcement: {'on' if strict_enabled(args, con) else 'off'}  "
          f"(exploration budget {limit} PER author; config debt_limit N)")
    return 0


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
# =========================================================================== #
# Codebase-mapping formats (static analyzers + dynamic-trace ingest + C export)
#
# Seven capabilities that turn the immutable dump + the PE + injected runtime
# traces into structured overlay facts, then re-render typed C. Each is a thin
# producer of the SAME overlay tables the manual commands write, so provenance,
# tiers, and `sync` apply uniformly. See revtool_README.md "Codebase mapping".
#   1. asserts        static: scrape ASSERT(file,"expr") -> member/method names
#   2. classes        static: RTTI/vftable -> class hierarchy + vtable slots
#   3. ingest-calls   dynamic: trace call edges + observed `this` vtable -> bind
#   4. ingest-allocs  dynamic: alloc size + ctor vtable -> sizeof(class)
#   5. ingest-layout  dynamic: guard-page field accesses -> struct fields
#   6. infer-types    dynamic: value samples -> field/global type + assertion
#   7. reconstruct    revtool-native typed C (no Ghidra): struct decls + body
#                     with `*(T*)((char*)v+0xNN)` rewritten to `v->field`
# =========================================================================== #

# An ASSERT macro compiles to `helper(cond, line, "file.cpp", "stringified-cond")`.
# We do not need the helper name: the adjacency of a source-file string literal and
# a following string literal is the reliable tell, and \s* spans the newline MSVC
# inserts when the condition string wraps. Group1=source file, group2=the expression.
ASSERT_PAIR_RE = re.compile(
    rb'"([^"\n]{1,80}\.(?:cpp|cxx|cc|hpp|h))"\s*,\s*"([^"\n]{1,400})"')
# Pointer-form assert: the stringified condition is passed as `&data_xxxx` instead of an
# inline literal, e.g. `sub_467c50(arg3, 0x17f, ".\maploader.cpp", &data_c43d30)`. The file
# is still an inline literal. group1=file, group2=the data symbol to resolve in pass two.
ASSERT_PTR_RE = re.compile(
    rb'\bsub_[0-9a-fA-F]+\(\s*[^,()\n]{0,48}?,\s*(?:0x[0-9a-fA-F]+|\d+)\s*,\s*'
    rb'"([^"\n]{1,80}\.(?:cpp|cxx|cc|hpp|h))"\s*,\s*&(data_[0-9a-fA-F]+)\s*\)')
# Data string definition: `char const data_xxxx[0x..] = "literal", 0`. Resolves the pointer.
DATA_STR_DEF_RE = re.compile(
    rb'\bchar const (data_[0-9a-fA-F]+)\[0x[0-9a-fA-F]+\]\s*=\s*"((?:[^"\\\n]|\\.)*)"')
# Short strings the decompiler leaves as a raw hex dump instead of a `char const` literal:
#   `00c422d4  data_c422d4:`  /  `00c422d4   30 00 00 00   0...`  ->  "0"
# Capture the label then the hex-byte run on the following dump line; decode until the NUL.
DATA_LABEL_RE = re.compile(
    rb'(data_[0-9a-fA-F]+):[ \t]*\r?\n[0-9a-f]{8}\s+((?:[0-9a-f]{2} ){1,32})')


def _decode_hex_run(hexrun):
    """`b'30 00 00 00 '` -> 'aaa' up to (not including) the first NUL byte."""
    out = bytearray()
    for tok in hexrun.split():
        b = int(tok, 16)
        if b == 0:
            break
        out.append(b)
    return out.decode("latin-1", "replace")
# Member extraction is deliberately limited to Hungarian `m_*` fields: high signal, ~zero
# false positives. Method calls in asserts are mostly STL noise (.size()/.end()) so we do
# NOT mine them - the raw expression is still printed for the human to read.
_MEMBER_RE = re.compile(r"\bm_[A-Za-z]\w*")


def _assert_expr_useful(expr):
    """True if an assert expression carries RE signal (an identifier/comparison/call), as
    opposed to a bare format/sentinel string like "%s", "0", or "" that names nothing."""
    if not expr:
        return False
    if "%" in expr and not (_MEMBER_RE.search(expr) or "::" in expr
                            or "==" in expr or "!=" in expr or "(" in expr):
        return False
    return bool(re.search(r"[A-Za-z_]", expr))


def _dedup_exprs(exprs):
    """Collapse identical (src, expr) pairs to (pair, count), preserving first-seen order."""
    counts = {}
    order = []
    for pair in exprs:
        if pair not in counts:
            order.append(pair)
        counts[pair] = counts.get(pair, 0) + 1
    return [(pair, counts[pair]) for pair in order]


def _func_byte_index(con):
    """(rows, starts) for bisecting a dump byte offset to its containing function.

    rows: list of (start_byte, end_byte, addr, name) sorted by start_byte.
    """
    rows = con.execute(
        "SELECT start_byte,end_byte,addr,name FROM functions ORDER BY start_byte").fetchall()
    starts = [r[0] for r in rows]
    return rows, starts


def _containing_func(rows, starts, byte_off):
    i = bisect.bisect_right(starts, byte_off) - 1
    if i >= 0:
        sb, eb, addr, name = rows[i]
        if sb <= byte_off < eb:
            return (addr, name)
    return (None, None)


def cmd_asserts(args, con):
    """FORMAT 1 - static assert-string scraper.

    The client is an assert-heavy build: every ASSERT carries its `__FILE__` and the
    stringified condition, which routinely names real members (`m_sDungeonInfo`),
    methods (`REGIONMGR.IsEnableRegist()`) and invariants (`iRid <= 0x7FFF`). Scraping
    them maps each function to its ORIGINAL source module and yields rename/field
    candidates - zero runtime needed.
    """
    dump = get_dump(con, args)
    if not os.path.exists(dump):
        print(f"error: dump not found: {dump}", file=sys.stderr)
        return 1
    rows, starts = _func_byte_index(con)
    by_file = {}                  # file -> count
    by_func = {}                  # (addr,name) -> {"file":set,"members":set,"exprs":[...]}
    # records: (src_file, expr_text_or_None, byte_offset). expr=None => pointer-form whose
    # data symbol resolved to nothing useful (still attributes the function to its source).
    records = []
    n_inline = n_ptr = n_ptr_resolved = 0
    needed = {}                   # data_sym -> list of (src, byte_offset)  (pass-one refs)
    with open(dump, "rb") as f:
        mm = mmap.mmap(f.fileno(), 0, access=mmap.ACCESS_READ)
        try:
            # PASS 1a - inline asserts: `"file.cpp", "expr"`
            for m in ASSERT_PAIR_RE.finditer(mm):
                src = m.group(1).decode("ascii", "replace").lstrip(".\\/")
                expr = m.group(2).decode("ascii", "replace")
                records.append((src, expr, m.start()))
                n_inline += 1
            # PASS 1b - pointer asserts: `"file.cpp", &data_xxxx`  (collect the symbols)
            for m in ASSERT_PTR_RE.finditer(mm):
                src = m.group(1).decode("ascii", "replace").lstrip(".\\/")
                sym = m.group(2).decode("ascii", "replace")
                needed.setdefault(sym, []).append((src, m.start()))
                n_ptr += 1
            # PASS 2 - resolve the needed data symbols to their string literals: first the
            # `char const ... = "..."` form, then the raw hex-dump form for short strings.
            data_map = {}
            if needed:
                want = set(needed)
                for m in DATA_STR_DEF_RE.finditer(mm):
                    nm = m.group(1).decode("ascii", "replace")
                    if nm in want:
                        data_map[nm] = m.group(2).decode("ascii", "replace")
                        if len(data_map) == len(want):
                            break
                missing = want - set(data_map)
                if missing:
                    for m in DATA_LABEL_RE.finditer(mm):
                        nm = m.group(1).decode("ascii", "replace")
                        if nm in missing:
                            data_map[nm] = _decode_hex_run(m.group(2))
                            missing.discard(nm)
                            if not missing:
                                break
            for sym, occ in needed.items():
                txt = data_map.get(sym)
                for src, off in occ:
                    if txt is not None:
                        n_ptr_resolved += 1
                    records.append((src, txt, off))
        finally:
            mm.close()

    # Aggregate. Every assert attributes its function to a source module; member names are
    # mined only from expressions that actually carry signal (not "%s"/"0"/sentinels).
    total = 0
    for src, expr, off in records:
        total += 1
        by_file[src] = by_file.get(src, 0) + 1
        addr, name = _containing_func(rows, starts, off)
        rec = by_func.setdefault((addr, name),
                                 {"file": set(), "members": set(), "exprs": []})
        rec["file"].add(src)
        if _assert_expr_useful(expr):
            for mm2 in _MEMBER_RE.finditer(expr):
                rec["members"].add(mm2.group(0))
            rec["exprs"].append((src, expr))
        elif expr is not None:
            rec["exprs"].append((src, expr))           # e.g. "%s" - shown, not mined
        else:
            rec["exprs"].append((src, "(ptr; unresolved)"))

    if total == 0:
        print("(no assert strings found)")
        return 0

    # --- targeted views ---
    if args.func:
        fr = resolve_func(con, args.func)
        if not fr:
            print(f"error: function not found: {args.func}", file=sys.stderr)
            return 1
        key = (fr[0], fr[1])
        rec = by_func.get(key)
        if not rec:
            print(f"{fr[1]}: no asserts found in this function")
            return 0
        print(f"{fr[1]} @ 0x{fr[0]}")
        print(f"  source: {', '.join(sorted(rec['file']))}")
        if rec["members"]:
            print(f"  members: {', '.join(sorted(rec['members']))}")
        for (src, expr), n in _dedup_exprs(rec["exprs"]):
            mult = f"  (x{n})" if n > 1 else ""
            print(f"    [{src}] {expr}{mult}")
        return 0

    if args.file:
        sel = {k: v for k, v in by_func.items()
               if any(args.file.lower() in s.lower() for s in v["file"])}
        print(f"asserts in *{args.file}*: {sum(len(v['exprs']) for v in sel.values())} "
              f"in {len(sel)} functions")
        for (addr, name), rec in sorted(sel.items(), key=lambda kv: (kv[0][1] or "")):
            label = name or "(no function)"
            applied = con.execute("SELECT new_name FROM globals WHERE orig_name=?",
                                  (name,)).fetchone() if name else None
            disp = (applied[0] if applied and applied[0] else label)
            mem = f"  members: {', '.join(sorted(rec['members']))}" if rec["members"] else ""
            print(f"\n  {disp} @ 0x{addr}{mem}")
            for (src, expr), n in _dedup_exprs(rec["exprs"])[:args.limit]:
                mult = f"  (x{n})" if n > 1 else ""
                print(f"    {expr}{mult}")
        return 0

    if args.suggest:
        cand = [(k, v) for k, v in by_func.items() if k[1] and v["members"]]
        cand.sort(key=lambda kv: len(kv[1]["members"]), reverse=True)
        print(f"rename/field candidates (functions with named members in asserts): {len(cand)}")
        for (addr, name), rec in cand[:args.limit]:
            applied = con.execute("SELECT new_name FROM globals WHERE orig_name=?",
                                  (name,)).fetchone()
            tag = "" if (applied and applied[0]) else "  [unnamed]"
            print(f"  {name} @ 0x{addr}{tag}")
            print(f"    src={', '.join(sorted(rec['file']))}  "
                  f"members={', '.join(sorted(rec['members']))}")
        return 0

    if args.apply_notes:
        author = get_author(args)
        n = 0
        for (addr, name), rec in by_func.items():
            if not name:
                continue
            srcs = ", ".join(sorted(rec["file"]))
            mems = ", ".join(sorted(rec["members"]))
            body = f"src={srcs}" + (f"; asserts name: {mems}" if mems else "")
            # idempotent: skip if a src= note already exists on this function
            exists = con.execute(
                "SELECT 1 FROM notes WHERE target=? AND body LIKE 'src=%'", (name,)).fetchone()
            if exists:
                continue
            con.execute("INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
                        (name, body, author, "mechanism", now_iso()))
            n += 1
        con.commit()
        print(f"asserts: attached source/member notes to {n} functions "
              f"(basis: observed assert strings).")
        return 0

    # --- default: summary ---
    print(f"asserts: {total} assert strings ({n_inline} inline, {n_ptr} pointer-form, "
          f"{n_ptr_resolved} of those resolved to a literal) across {len(by_file)} source "
          f"files, {len([k for k in by_func if k[1]])} functions.")
    print("\ntop source modules by assert count:")
    for src, cnt in sorted(by_file.items(), key=lambda kv: kv[1], reverse=True)[:args.limit]:
        print(f"  {cnt:5d}  {src}")
    print("\nnext: `asserts --file maploader.cpp` | `asserts --suggest` | "
          "`asserts --apply-notes`")
    return 0


# --------------------------------------------------------------------------- #
# RTTI / vtable index (shared by `classes` and the dynamic ingest commands)
# --------------------------------------------------------------------------- #
_VFTABLE_RE = re.compile(
    r"^([0-9a-f]{8})\s+struct\s+[\w:<>]+::VTable\s+([\w:<>]+)::`vftable'"
    r"(?:\{for `([\w:<>]+)'\})?")
_COL_RE = re.compile(r"_RTTICompleteObjectLocator\s+([\w:<>]+)::`RTTI Complete Object Locator'")
# A class's Base Class Array lists, nearest-first, the class itself ([0]) then all of its
# ancestors ([1:]). MSVC enumerates the FULL ancestor set here, so one array == the whole
# inheritance chain for that class (no recursion needed). Two text shapes:
#   multi:  `struct CObjChild::`RTTI Base Class Array' = { ...[0x0]=A::...; [0x1]=B::... }`
#   single: `void* CObj::`RTTI Base Class Array' = CObj::`RTTI Base Class Descriptor...`  (root)
_ARR_START_RE = re.compile(r"struct ([\w:<>]+)::`RTTI Base Class Array' =")
_ARR_SINGLE_RE = re.compile(
    r"void\* ([\w:<>]+)::`RTTI Base Class Array' = ([\w:<>]+)::`RTTI Base Class Descriptor")
_ARR_ENTRY_RE = re.compile(r"\[0x[0-9a-fA-F]+\]\s*=\s*([\w:<>]+)::`RTTI Base Class Descriptor")


def _rtti_index(dump):
    """Parse the dump's RTTI/vftable labels into a class model.

    Returns dict:
      classes        set of class names
      vtable_addr    class -> int VA of its primary vftable
      class_at_vtbl  int VA -> class for primary vftables
      class_vtables  class -> list((int VA, for_base_or_None)) for all vftables
      direct_base    class -> immediate base class (entries[1] of its array)
      anc            class -> set(all ancestor classes)        (from the Base Class Array)
      desc           class -> set(all descendant classes)      (inverse of anc)
    """
    classes = set()
    vtable_addr = {}
    class_at_vtbl = {}
    class_vtables = {}
    direct_base = {}
    anc = {}
    in_owner = None
    entries = []

    def _finalize(owner, ents):
        if not owner or not ents:
            return
        # ents[0] is the class itself; ents[1:] are its ancestors, nearest-first.
        rest = [e for e in ents[1:]]
        if rest:
            direct_base[owner] = rest[0]
            anc.setdefault(owner, set()).update(rest)
            classes.update(rest)

    with open(dump, encoding="utf-8", errors="replace") as f:
        for line in f:
            mv = _VFTABLE_RE.match(line)
            if mv:
                addr = int(mv.group(1), 16)
                cls = mv.group(2)
                for_base = mv.group(3)
                classes.add(cls)
                class_vtables.setdefault(cls, []).append((addr, for_base))
                if for_base is None and cls not in vtable_addr:
                    vtable_addr[cls] = addr
                    class_at_vtbl[addr] = cls
                continue
            if in_owner is not None:
                me = _ARR_ENTRY_RE.search(line)
                if me:
                    entries.append(me.group(1))
                    continue
                # Structural lines (`arrayOfBaseClassDescriptors[..] =`, `{`) precede the
                # entries, so only CLOSE once at least one entry has been collected.
                if entries:
                    _finalize(in_owner, entries)
                    in_owner, entries = None, []
                # else: keep waiting for the entries (fall through to re-check starts)
            ma = _ARR_START_RE.search(line)
            if ma:
                in_owner, entries = ma.group(1), []
                classes.add(in_owner)
                continue
            ms = _ARR_SINGLE_RE.search(line)
            if ms:
                classes.add(ms.group(1))  # single-entry array => root class, no ancestors
                continue
            mc = _COL_RE.search(line)
            if mc:
                classes.add(mc.group(1))
    _finalize(in_owner, entries)

    desc = {}
    for cls, ancestors in anc.items():
        for a in ancestors:
            desc.setdefault(a, set()).add(cls)
    return {
        "classes": classes, "vtable_addr": vtable_addr, "class_at_vtbl": class_at_vtbl,
        "class_vtables": class_vtables, "direct_base": direct_base, "anc": anc, "desc": desc,
        "source": "legacy Binary Ninja dump labels",
    }


def _structural_rtti_index(args, con, pe=None):
    """Build the shared PE/RTTI class index used by the family planner.

    The reconciled entry inventory supplies candidate code starts, but the
    locator, hierarchy, complete-object offset, and table bounds all come from
    the retail PE.  This function deliberately fails rather than falling back
    silently to dump labels: two class commands giving different structural
    answers is more dangerous than an unavailable command.
    """
    if scan_vtables is None:
        raise ValueError(f"shared RTTI reader (msvc_rtti.py) is unavailable from {HERE}")
    pe = pe or open_pe(con, args)
    if pe is None:
        raise ValueError("retail PE is unavailable")
    reconciled_path = evidence_artifact_paths(con, args)["reconciled_function_index"]
    with open(reconciled_path, encoding="utf-8") as stream:
        reconciled = json.load(stream)
    target_hash = sha256_file(pe.path)
    if str(reconciled.get("target_sha256") or "").lower() != target_hash.lower():
        raise ValueError("reconciled function index belongs to another executable")
    starts = {
        int(row["va"])
        for row in reconciled.get("functions") or []
        if row.get("admitted", True)
    }
    sections = [
        (name, pe.image_base + rva, raw_ptr, raw_size)
        for name, rva, _virtual_size, raw_ptr, raw_size in pe.sections
    ]
    text = next((row for row in sections if row[0] == ".text"), None)
    if text is None:
        raise ValueError("retail PE has no .text section")
    _name, text_va, _raw, text_size = text
    records = scan_vtables(pe.data, sections, text_va, text_va + text_size, starts)
    hierarchy = rtti_hierarchies(pe.data, sections)
    direct_sets = rtti_direct_bases(pe.data, sections)
    classes = set(hierarchy)
    for values in hierarchy.values():
        classes.update(values)
    vtable_addr = {}
    class_at_vtbl = {}
    class_vtables = {}
    record_by_vtable = {}
    for record in records:
        class_at_vtbl[record.table_va] = record.class_name
        record_by_vtable[record.table_va] = record
        class_vtables.setdefault(record.class_name, []).append((
            record.table_va,
            None if record.complete_object_offset == 0
            else f"this+0x{record.complete_object_offset:x}",
        ))
        if record.complete_object_offset == 0:
            vtable_addr.setdefault(record.class_name, record.table_va)
    anc = {
        owner: set(values) - {owner}
        for owner, values in hierarchy.items()
    }
    desc = {}
    for owner, ancestors in anc.items():
        for base in ancestors:
            desc.setdefault(base, set()).add(owner)
    direct_base = {
        owner: next(iter(values))
        for owner, values in direct_sets.items()
        if len(values) == 1
    }
    return {
        "classes": classes,
        "vtable_addr": vtable_addr,
        "class_at_vtbl": class_at_vtbl,
        "class_vtables": class_vtables,
        "vtable_records": record_by_vtable,
        "direct_base": direct_base,
        "direct_bases": {owner: set(values) for owner, values in direct_sets.items()},
        "anc": anc,
        "desc": desc,
        "source": "shared structural PE/MSVC RTTI parser",
    }


def _best_rtti_index(args, con, pe=None):
    """One structural RTTI answer for commands and dynamic ingesters."""
    return _structural_rtti_index(args, con, pe)


def cmd_classes(args, con):
    """FORMAT 2 - structurally bounded PE RTTI/vftable class hierarchy."""
    pe = open_pe(con, args)
    if pe is None:
        return 1
    try:
        idx = _best_rtti_index(args, con, pe)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"error: structural RTTI inventory unavailable: {exc}", file=sys.stderr)
        return 1
    classes = idx["classes"]

    if args.name:
        cls = args.name
        if cls not in classes:
            # tolerate an applied/dump alias by case-insensitive match
            hit = [c for c in classes if c.lower() == cls.lower()]
            if not hit:
                print(f"error: class not found in RTTI: {cls}", file=sys.stderr)
                return 1
            cls = hit[0]
        anc = idx["anc"].get(cls, set())
        des = idx["desc"].get(cls, set())
        direct_sub = sorted(
            c for c in des if cls in idx.get("direct_bases", {}).get(c, ())
        )
        print(f"class {cls}")
        direct = sorted(idx.get("direct_bases", {}).get(cls, ()))
        print(f"  direct base(s): {', '.join(direct) or '(none - root)'}")
        if anc:
            print(f"  all ancestors: {', '.join(sorted(anc))}")
        print(f"  direct subclasses: {', '.join(direct_sub) or '(none)'}")
        if des:
            print(f"  all descendants ({len(des)}): {', '.join(sorted(des))}")
        vaddr = idx["vtable_addr"].get(cls)
        if vaddr is None:
            print("  vtable: (no primary vftable found)")
            return 0
        print(f"  vtable @ 0x{vaddr:08x}")
        record = idx.get("vtable_records", {}).get(vaddr)
        count = len(record.targets) if record is not None else 0
        print(f"  source: {idx['source']}")
        print(f"  bounded slots: {count}")
        printed = 0
        for i in range(min(args.count, count)):
            target = pe.read_u32(vaddr + i * 4)
            if target is None or not pe.in_image(target):
                break
            name, tag = resolve_addr(con, target)
            print(f"    +0x{i*4:<4x} 0x{target:08x}  {name}  {tag}")
            printed += 1
        if not printed:
            print("    (no readable slots)")
        return 0

    if args.base:
        base = args.base
        subs = idx["desc"].get(base, set())
        if not subs and base not in classes:
            print(f"error: base class not found in RTTI: {base}", file=sys.stderr)
            return 1
        print(f"subclasses of {base}: {len(subs)}")
        for c in sorted(subs):
            direct = (
                "  (direct)"
                if base in idx.get("direct_bases", {}).get(c, ())
                else ""
            )
            va = idx["vtable_addr"].get(c)
            vs = f"  vtable 0x{va:08x}" if va else ""
            print(f"  {c}{vs}{direct}")
        return 0

    roots = sorted(c for c in classes if not idx.get("direct_bases", {}).get(c))
    print(f"classes: {len(classes)} RTTI classes, {len(idx['vtable_addr'])} with primary vtables, "
          f"{len(idx['direct_bases'])} with one or more known direct bases.")
    print(f"source: {idx['source']}")
    if args.list:
        for c in sorted(classes):
            va = idx["vtable_addr"].get(c)
            b = idx["direct_base"].get(c)
            extra = []
            if b:
                extra.append("base=" + b)
            if va:
                extra.append(f"vtable=0x{va:08x}")
            print(f"  {c}" + ("  [" + "; ".join(extra) + "]" if extra else ""))
    else:
        print(f"\nroot classes (no base): {len(roots)}")
        for c in roots[:args.limit]:
            nd = len(idx["desc"].get(c, set()))
            print(f"  {c}" + (f"  (+{nd} descendants)" if nd else ""))
        print("\nnext: `classes <Name>` | `classes --base CObj` | `classes --list`")
    return 0


# --------------------------------------------------------------------------- #
# Dynamic-trace ingest (formats 3-6). All read JSONL emitted by the native-trace
# DLL (one JSON object per line, `kind` field selects the record).
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


def cmd_ingest_calls(args, con):
    """FORMAT 3 - dynamic call graph + observed-`this` type binding.

    JSONL records: {"kind":"call","caller":"0x..","callee":"0x..","this_vtable":"0x.."?}
    Resolves caller/callee to functions; counts edges; and (the payload static cannot give)
    when a record carries the live `this` vtable, maps it to a class and BINDS the callee's
    arg1 to that class - resolving indirect/virtual dispatch from ground truth.
    """
    records = _read_jsonl(args.log)
    idx = _best_rtti_index(args, con) if any(
        r.get("this_vtable") for r in records
    ) else {"class_at_vtbl": {}}
    edges = {}
    callee_class = {}  # callee_addr_int -> {class: count}
    for r in records:
        if r.get("kind") != "call":
            continue
        caller = _as_int(r.get("caller"))
        callee = _as_int(r.get("callee"))
        if callee is None:
            continue
        edges[(caller, callee)] = edges.get((caller, callee), 0) + 1
        vt = _as_int(r.get("this_vtable"))
        if vt is not None:
            cls = idx["class_at_vtbl"].get(vt)
            if cls:
                callee_class.setdefault(callee, {})
                callee_class[callee][cls] = callee_class[callee].get(cls, 0) + 1
    if not edges:
        print("ingest-calls: no `call` records found.")
        return 0
    print(f"ingest-calls: {len(edges)} distinct edges, "
          f"{len(callee_class)} callees with an observed `this` class.")
    for (caller, callee), cnt in sorted(edges.items(), key=lambda kv: kv[1], reverse=True)[:args.limit]:
        cn, ct = resolve_addr(con, caller) if caller is not None else ("(unknown)", "")
        en, et = resolve_addr(con, callee)
        cls = callee_class.get(callee)
        clsf = ""
        if cls:
            top = max(cls.items(), key=lambda kv: kv[1])[0]
            clsf = f"   this={top}"
        print(f"  {cn} -> {en}  (x{cnt}){clsf}")
    if args.apply:
        author = get_author(args)
        n = 0
        for callee, classmap in callee_class.items():
            top = max(classmap.items(), key=lambda kv: kv[1])[0]
            fr = resolve_func(con, "%08x" % callee)
            if not fr:
                continue
            faddr = fr[0]
            con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (top,))
            con.execute(
                "INSERT INTO bindings(func_addr,var_name,type_name,tier,author,updated_at) "
                "VALUES(?,?,?,?,?,?) ON CONFLICT(func_addr,var_name) DO UPDATE SET "
                "type_name=excluded.type_name, tier=excluded.tier, author=excluded.author, "
                "updated_at=excluded.updated_at",
                (faddr, "arg1", top, "mechanism", author, now_iso()))
            record_history(con, "bind", f"bind:{faddr}:arg1", "set", None, top, author,
                           "dynamic: observed this vtable")
            n += 1
        con.commit()
        print(f"  applied {n} arg1 type bindings (basis: dynamic observed `this`).")
    return 0


def cmd_ingest_allocs(args, con):
    """FORMAT 4 - sizeof(class) from allocation size + constructor vtable.

    JSONL records:
      {"kind":"alloc","ret":"0x..","size":N,"site":"0x.."}
      {"kind":"ctor","obj":"0x..","vtable":"0x.."}   (the vtable write identifies the class)
      {"kind":"alloc","class":"Name","size":N}        (pre-correlated shortcut)
    Correlates an allocation's returned pointer with the ctor that stamps a vtable into it,
    yielding `sizeof(class)` - which sizes the struct and labels the alloc sites.
    """
    records = _read_jsonl(args.log)
    need_idx = any(r.get("kind") == "ctor" for r in records)
    idx = _best_rtti_index(args, con) if need_idx else {"class_at_vtbl": {}}
    alloc_by_ret = {}
    class_size = {}   # class -> (size, site)
    for r in records:
        if r.get("kind") == "alloc":
            cls = r.get("class")
            size = _as_int(r.get("size"))
            if cls and size is not None:
                if cls not in class_size or size > class_size[cls][0]:
                    class_size[cls] = (size, _as_int(r.get("site")))
            ret = _as_int(r.get("ret"))
            if ret is not None and size is not None:
                alloc_by_ret[ret] = (size, _as_int(r.get("site")))
        elif r.get("kind") == "ctor":
            obj = _as_int(r.get("obj"))
            vt = _as_int(r.get("vtable"))
            cls = idx["class_at_vtbl"].get(vt) if vt is not None else None
            if cls and obj in alloc_by_ret:
                size, site = alloc_by_ret[obj]
                if cls not in class_size or size > class_size[cls][0]:
                    class_size[cls] = (size, site)
    if not class_size:
        print("ingest-allocs: no class sizes recovered "
              "(need alloc{class,size} or alloc{ret,size}+ctor{obj,vtable}).")
        return 0
    print(f"ingest-allocs: recovered sizeof for {len(class_size)} classes.")
    author = get_author(args)
    for cls, (size, site) in sorted(class_size.items()):
        sitef = f" (alloc site 0x{site:08x})" if site else ""
        print(f"  sizeof({cls}) = {size} (0x{size:x}) bytes{sitef}")
        if args.apply:
            con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (cls,))
            body = f"sizeof={size} (0x{size:x}) bytes (dynamic){sitef}"
            exists = con.execute(
                "SELECT 1 FROM notes WHERE target=? AND body LIKE 'sizeof=%'", (cls,)).fetchone()
            if not exists:
                con.execute(
                    "INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
                    (cls, body, author, "mechanism", now_iso()))
            con.execute(
                "INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
                (f"sizeof:{cls}", str(size), os.path.basename(args.log), author, now_iso()))
    if args.apply:
        con.commit()
        print(f"  applied sizeof notes (basis: dynamic).")
    return 0


# C type widths used by layout/reconstruct.
_CTYPE_SIZE = {
    "uint8": 1, "int8": 1, "char": 1, "bool": 1,
    "uint16": 2, "int16": 2, "short": 2,
    "uint32": 4, "int32": 4, "int": 4, "float": 4, "ptr": 4, "void*": 4,
    "uint64": 8, "int64": 8, "double": 8,
}


def _ftype_from_size(size):
    return {1: "uint8", 2: "uint16", 4: "uint32", 8: "double"}.get(size, "uint32")


def cmd_ingest_layout(args, con):
    """FORMAT 5 - struct field layout from guard-page access traces.

    JSONL records: {"kind":"field","type":"Name"|"vtable":"0x..","offset":"0x..",
                    "size":N,"access":"r"|"w"}
    Each distinct (type, offset) becomes a field with a size-derived C type and an
    observed read/write note. Existing NAMED fields are never overwritten - this only
    fills unnamed offsets, so it complements hand RE instead of clobbering it.
    """
    records = _read_jsonl(args.log)
    need_idx = any(r.get("vtable") for r in records if r.get("kind") == "field")
    idx = _best_rtti_index(args, con) if need_idx else {"class_at_vtbl": {}}
    layout = {}   # type -> {offset: {"size":max,"access":set}}
    for r in records:
        if r.get("kind") != "field":
            continue
        typ = r.get("type")
        if not typ:
            vt = _as_int(r.get("vtable"))
            typ = idx["class_at_vtbl"].get(vt) if vt is not None else None
        off = _as_int(r.get("offset"))
        if not typ or off is None:
            continue
        size = _as_int(r.get("size")) or 4
        acc = r.get("access", "?")
        slot = layout.setdefault(typ, {}).setdefault(off, {"size": size, "access": set()})
        slot["size"] = max(slot["size"], size)
        slot["access"].add(acc)
    if not layout:
        print("ingest-layout: no `field` records found.")
        return 0
    author = get_author(args)
    applied = 0
    for typ, offs in sorted(layout.items()):
        print(f"{typ}: {len(offs)} distinct offsets")
        for off, slot in sorted(offs.items()):
            ft = _ftype_from_size(slot["size"])
            acc = "".join(sorted(slot["access"]))
            print(f"  +0x{off:<4x} {ft:<8} [{acc}]")
            if args.apply:
                exists = con.execute(
                    "SELECT new_name FROM fields WHERE type_name=? AND offset=?",
                    (typ, off)).fetchone()
                if exists:
                    continue  # never clobber a hand-named field
                con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (typ,))
                con.execute(
                    "INSERT INTO fields(type_name,offset,new_name,ftype,note,tier,author,"
                    "evidence,updated_at,basis) VALUES(?,?,?,?,?,?,?,?,?,?) "
                    "ON CONFLICT(type_name,offset) DO NOTHING",
                    (typ, off, f"field_0x{off:x}", ft, f"observed {acc} width {slot['size']}",
                     "guess", author, "guard-page trace", now_iso(), "dynamic"))
                applied += 1
    if args.apply:
        con.commit()
        print(f"\napplied {applied} new fields (basis: dynamic; unnamed offsets only).")
    return 0


def _classify_samples(values):
    """Infer a field/global type from observed integer samples.

    Returns (ftype, predicate, claim). Heuristic, recorded with basis=dynamic so a human
    still reviews; the point is to turn a column of hex into a typed hypothesis.
    """
    uniq = sorted(set(values))
    if not uniq:
        return ("uint32", "any", "no samples")
    if len(uniq) == 1:
        v = uniq[0]
        return ("uint32", f"eq:{v}", f"constant 0x{v:x}")
    # float reinterpret: plausible if every nonzero sample decodes to a sane magnitude
    floatish = True
    for v in uniq:
        if v == 0:
            continue
        try:
            fv = struct.unpack("<f", struct.pack("<I", v & 0xffffffff))[0]
        except struct.error:
            floatish = False
            break
        if fv != fv or abs(fv) == float("inf") or not (1e-6 <= abs(fv) <= 1e9):
            floatish = False
            break
    if floatish and any(v not in (0, 1) for v in uniq):
        return ("float", "any", "values decode to sane IEEE-754 floats")
    # pointer-ish: all in the user-address range and 4-aligned-ish
    if all(v == 0 or 0x00010000 <= v < 0x80000000 for v in uniq) and \
            any(v >= 0x00400000 for v in uniq):
        return ("ptr", "any", "values fall in the user/code address range")
    # bitmask: small flag-like values
    if all(bin(v).count("1") <= 4 for v in uniq) and max(uniq) < 0x10000 and len(uniq) > 1:
        union = 0
        for v in uniq:
            union |= v
        return ("uint32", "any", f"flag bits, union=0x{union:x}")
    return ("uint32", f"range:{min(uniq)}:{max(uniq)}", f"int range [{min(uniq)}..{max(uniq)}]")


_SUBJ_FIELD_RE = re.compile(r"^([A-Za-z_]\w*)\+0x([0-9a-fA-F]+)$")


def cmd_infer_types(args, con):
    """FORMAT 6 - value/type inference from field/global samples.

    JSONL records: {"kind":"sample","subject":"Class+0x78"|"data_f0673f","value":"0x..","as":"f32"?}
    Aggregates samples per subject, classifies the type, and (with --apply) sets the field
    type for "Class+0xNN" subjects and records a dynamic assertion for any subject.
    """
    records = _read_jsonl(args.log)
    samples = {}
    forced = {}
    for r in records:
        if r.get("kind") != "sample":
            continue
        subj = r.get("subject")
        val = _as_int(r.get("value"))
        if not subj or val is None:
            continue
        samples.setdefault(subj, []).append(val)
        if r.get("as"):
            forced[subj] = r["as"]
    if not samples:
        print("infer-types: no `sample` records found.")
        return 0
    author = get_author(args)
    applied_fields = applied_asserts = 0
    for subj, vals in sorted(samples.items()):
        ft, pred, claim = _classify_samples(vals)
        if subj in forced:
            ft = {"f32": "float", "f": "float", "u32": "uint32",
                  "ptr": "ptr"}.get(forced[subj], ft)
        print(f"{subj}: {ft}  ({pred})  n={len(vals)} uniq={len(set(vals))}  -- {claim}")
        if not args.apply:
            continue
        m = _SUBJ_FIELD_RE.match(subj)
        if m:
            typ, off = m.group(1), int(m.group(2), 16)
            exists = con.execute(
                "SELECT new_name FROM fields WHERE type_name=? AND offset=?",
                (typ, off)).fetchone()
            con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (typ,))
            con.execute(
                "INSERT INTO fields(type_name,offset,new_name,ftype,note,tier,author,evidence,"
                "updated_at,basis) VALUES(?,?,?,?,?,?,?,?,?,?) "
                "ON CONFLICT(type_name,offset) DO UPDATE SET ftype=excluded.ftype, "
                "updated_at=excluded.updated_at, basis=excluded.basis",
                (typ, off, exists[0] if exists else f"field_0x{off:x}", ft, f"inferred: {claim}",
                 "guess", author, "value samples", now_iso(), "dynamic"))
            applied_fields += 1
        # HONESTY: the sampled VALUES are real (basis=dynamic), but the type CLASSIFICATION
        # is a heuristic over a handful of samples - it is NOT a confirmed fact. Record it
        # OPEN at tier=guess so it shows up as "needs confirmation", never as ground truth.
        con.execute(
            "INSERT INTO assertions(subject,claim,predicate,basis,method,tier,status,author,"
            "evidence,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
            (subj, f"[heuristic] {claim}", pred, "dynamic",
             "infer-types value-sample classification (heuristic)", "guess", "open",
             author, f"{len(vals)} samples from {os.path.basename(args.log)}", now_iso()))
        applied_asserts += 1
    if args.apply:
        con.commit()
        print(f"\napplied {applied_fields} field types, {applied_asserts} dynamic assertions.")
    return 0


# --------------------------------------------------------------------------- #
# FORMAT 7 - revtool-native typed C reconstruction (no Ghidra)
# --------------------------------------------------------------------------- #
# A raw offset-deref the rewrite targets: `*(T*)((char*)v + 0xNN)` / `*(T*)(v + 0xNN)`.
# Counting these before/after the rewrite gives an honest coverage number for the output.
_RAW_DEREF_RE = re.compile(r"\*\(\s*\w[\w ]*\*\s*\)\(\s*(?:\(\s*char\s*\*\s*\))?\s*\w+\s*\+\s*0x[0-9a-fA-F]+")


def _ctype_size(ft):
    if not ft:
        return 4
    return _CTYPE_SIZE.get(ft.replace("*", "").strip(), 4)


def _emit_struct(name, fields):
    """fields: list of (offset, fieldname, ftype, note). Emit a C struct with padding."""
    out = [f"struct {name} {{"]
    cur = 0
    for off, fname, ft, note in sorted(fields, key=lambda x: x[0]):
        if off > cur:
            out.append(f"    char _pad_{cur:x}[0x{off - cur:x}];")
        ctype = (ft or "uint32_t")
        ctype = {"uint8": "uint8_t", "uint16": "uint16_t", "uint32": "uint32_t",
                 "int8": "int8_t", "int16": "int16_t", "int32": "int32_t",
                 "int64": "int64_t", "uint64": "uint64_t", "ptr": "void*"}.get(ctype, ctype)
        cmt = f"  // {note}" if note else ""
        out.append(f"    /* +0x{off:x} */ {ctype} {fname};{cmt}")
        cur = off + _ctype_size(ft)
    out.append("};")
    return "\n".join(out)


def _rewrite_fields(code, var, fields):
    """Rewrite `*(T*)((char*)var + 0xNN)` and `*(T*)var` (offset 0) to `var->field`."""
    for off, fname in fields:
        if off == 0:
            pat = re.compile(
                r"\*\(\s*\w[\w ]*\*\s*\)\s*" + re.escape(var) + r"\b")
            code = pat.sub(f"{var}->{fname}", code)
        pat2 = re.compile(
            r"\*\(\s*\w[\w ]*\*\s*\)\(\s*\(\s*char\s*\*\s*\)\s*" + re.escape(var)
            + r"\s*\+\s*0x%x\s*\)" % off)
        code = pat2.sub(f"{var}->{fname}", code)
        # also the no-cast form: *(uint32_t*)(var + 0xNN)
        pat3 = re.compile(
            r"\*\(\s*\w[\w ]*\*\s*\)\(\s*" + re.escape(var) + r"\s*\+\s*0x%x\s*\)" % off)
        code = pat3.sub(f"{var}->{fname}", code)
    return code


def cmd_reconstruct(args, con):
    """FORMAT 7 - emit typed C for a function from the overlay (no Ghidra).

    Pulls the function body, applies global renames, then rewrites offset-arithmetic field
    accesses on every bound var into `var->field` and prints the struct decls those fields
    imply. The output reads like source instead of `*(uint32_t*)((char*)a1 + 0x78)`.
    """
    dump = get_dump(con, args)
    fr = resolve_func(con, args.target)
    if not fr:
        print(f"error: function not found: {args.target}", file=sys.stderr)
        return 1
    addr, name, sig, sb, eb, sl, el = fr
    code = read_func_lines(dump, sb, eb)

    globals_map = load_globals(con)
    locals_map = load_locals(con, addr)
    bindings = load_bindings(con, addr)

    # strip the dump's leading "ADDR   " gutter so the C reads cleanly
    stripped = []
    for line in code:  # read_func_lines returns a list of lines
        mm = re.match(r"^[0-9a-f]{8}\s{2}(.*)$", line)
        stripped.append(mm.group(1) if mm else line)
    body = "\n".join(stripped)

    # 1) rewrite field accesses per bound var (before global rename so var tokens are intact)
    #    Count raw offset-derefs before/after so the output never pretends to be complete:
    #    only a few `*(T*)((char*)v+0xNN)` shapes are rewritten; nested/indexed/odd ones stay.
    raw_before = len(_RAW_DEREF_RE.findall(body))
    used_types = {}
    for var, typ in bindings.items():
        flds = load_fields(con, typ)  # {offset: (new_name, ftype, note, ...)}
        used_types[typ] = flds
        pairs = [(off, vals[0]) for off, vals in flds.items()]
        body = _rewrite_fields(body, var, pairs)
    raw_after = len(_RAW_DEREF_RE.findall(body))
    rewrote = raw_before - raw_after

    # 2) apply global (sub_/data_) renames only; keep var names as bound
    rename = {k: v[0] for k, v in globals_map.items()}
    body = apply_renames(body, rename)
    # local var renames too (so arg3 -> sectorId reads nicely), but only whole-word
    for orig, vals in locals_map.items():
        body = re.sub(r"\b" + re.escape(orig) + r"\b", vals[0], body)

    out = []
    self_name = globals_map.get(name, (name,))[0]
    out.append(f"// reconstructed by revtool from {name}"
               + (f" ({self_name})" if self_name != name else "") + f" @ 0x{addr}")
    out.append(f"// dump lines {sl}-{el}; NOT a compiler-faithful decompile - field/name")
    out.append("// overlay applied to the pseudocode body. A readability LENS, not source.")
    out.append(f"// field-rewrite coverage: {rewrote} offset-deref(s) -> v->field, "
               f"{raw_after} left raw (nested/indexed/unbound).")
    out.append("")
    for typ, flds in sorted(used_types.items()):
        if not flds:
            out.append(f"struct {typ} {{ /* no fields named yet */ }};")
            continue
        fields = [(off, vals[0], vals[1], vals[2]) for off, vals in flds.items()]
        out.append(_emit_struct(typ, fields))
        out.append("")
    if bindings:
        out.append("// bindings: " + ", ".join(f"{v}:{t}*" for v, t in sorted(bindings.items())))
        out.append("")
    out.append(body)
    text = "\n".join(out)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as f:
            f.write(text + "\n")
        print(f"reconstruct: wrote {args.out} ({len(text.splitlines())} lines, "
              f"{len(used_types)} structs)")
    else:
        print(text)
    return 0


def build_parser():
    p = argparse.ArgumentParser(
        prog="revtool",
        description="Non-destructive symbol overlay for the SRO pseudocode dump.")
    p.add_argument("--db", default=DEFAULT_DB, help="overlay database path")
    p.add_argument("--dump", default=None, help="pseudocode dump path (default: indexed/auto)")
    p.add_argument("--binary", default=None,
                   help="PE image path for vtable/deref (default: config 'binary' or SRO_Client.exe)")
    p.add_argument("--author", default=None,
                   help="attribute claims to this author (default: $REVTOOL_AUTHOR or OS user)")
    p.add_argument("--strict", action="store_true",
                   help="force strict enforcement for this call (investigate => must label)")
    p.add_argument("--no-strict", dest="no_strict", action="store_true",
                   help="disable strict enforcement for this call")
    sub = p.add_subparsers(dest="cmd", required=True)

    sp = sub.add_parser("index", help="scan the dump and build the function index")
    sp.set_defaults(handler=cmd_index, dump_required=True)

    sp = sub.add_parser("status", help="show overlay/index stats")
    sp.set_defaults(handler=cmd_status)

    sp = sub.add_parser("doctor",
                        help="fail-closed provenance, analyzer snapshot, DB, and ledger audit")
    sp.add_argument("--lock", default=DEFAULT_EVIDENCE_LOCK,
                    help="evidence lock path")
    sp.add_argument("--ledger", default=None,
                    help="claim/event ledger path (default: selected DB's ledger)")
    sp.add_argument("--refresh-lock", action="store_true",
                    help="explicitly rewrite the analyzer evidence lock before checking")
    sp.add_argument("--json", action="store_true", help="emit machine-readable result")
    sp.set_defaults(handler=cmd_doctor)

    sp = sub.add_parser("dossier",
                        help="hash-bound bytes/proof/BN/Rizin/RTTI/Revtool packet for one function")
    sp.add_argument("target", help="VA, original dump symbol, or applied Revtool name")
    sp.add_argument("--out", default=None, help="write canonical JSON dossier")
    sp.add_argument("--json", action="store_true", help="print JSON instead of summary")
    sp.add_argument("--allow-stale", action="store_true",
                    help="diagnostic only; do not use a stale dossier for promotion")
    sp.set_defaults(handler=cmd_dossier)

    sp = sub.add_parser("find", help="find functions by name/signature substring")
    sp.add_argument("pattern")
    sp.add_argument("--limit", type=int, default=40)
    sp.set_defaults(handler=cmd_find)

    sp = sub.add_parser("view", help="render a function with the overlay applied")
    sp.add_argument("target", help="sub_xxxx | applied name | address")
    sp.add_argument("--raw", action="store_true", help="pristine original, no overlay")
    sp.add_argument("--no-apply", action="store_true", help="keep original tokens, only annotate")
    sp.add_argument("--no-legend", action="store_true", help="hide the rename legend")
    sp.add_argument("--lines", help="restrict to dump line range A:B (e.g. 1381700:1381760)")
    sp.add_argument("--force", action="store_true",
                    help="bypass the strict label-debt gate once (audited in history)")
    sp.set_defaults(handler=cmd_view)

    sp = sub.add_parser("xref", help="every line that mentions a symbol (whole dump)")
    sp.add_argument("symbol")
    sp.add_argument("--limit", type=int, default=200)
    sp.add_argument("--force", action="store_true",
                    help="bypass the strict label-debt gate once (audited in history)")
    sp.set_defaults(handler=cmd_xref)

    sp = sub.add_parser("rename", help="rename a symbol (global or local scope)")
    rsub = sp.add_subparsers(dest="scope", required=True)
    rg = rsub.add_parser("global", help="rename sub_/data_ everywhere")
    rg.add_argument("a", metavar="orig")
    rg.add_argument("b", metavar="new")
    rg.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    rg.add_argument("--evidence", default=None, help="why you believe this (RVA, rizin, etc.)")
    rg.add_argument("--basis", default=None, help=f"evidence basis: {'|'.join(BASES)}")
    rg.add_argument("--method", default=None, help="how derived (e.g. 'rizin /x absolute-addr')")
    rg.add_argument("--blindspots", default=None, help="what `method` cannot see")
    rg.add_argument("--shallow", action="store_true",
                    help="ack a confident claim has no locals/fields worth naming (bypasses depth gate, audited)")
    rg.set_defaults(handler=cmd_rename)
    rl = rsub.add_parser("local", help="rename a local within ONE function")
    rl.add_argument("func")
    rl.add_argument("a", metavar="orig")
    rl.add_argument("b", metavar="new")
    rl.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    rl.add_argument("--evidence", default=None)
    rl.set_defaults(handler=cmd_rename)

    sp = sub.add_parser("type", help="manage struct types")
    sp.add_argument("action", choices=["new", "list", "show", "rename"])
    sp.add_argument("name", nargs="?")
    sp.add_argument("newname", nargs="?",
                    help="new type name (for `type rename <old> <new>`)")
    sp.add_argument("--note", "--evidence", dest="note", default=None,
                    help="description for `type new`")
    sp.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    sp.add_argument("--basis", default=None, help=f"evidence basis: {'|'.join(BASES)}")
    sp.set_defaults(handler=cmd_type)

    sp = sub.add_parser("field", help="name a struct field at an offset")
    sp.add_argument("type")
    sp.add_argument("offset", help="hex, e.g. 0x67e6")
    sp.add_argument("name")
    sp.add_argument("--type", dest="ftype", default=None, help="field C type")
    sp.add_argument("--note", default=None)
    sp.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    sp.add_argument("--evidence", default=None)
    sp.add_argument("--basis", default=None, help=f"evidence basis: {'|'.join(BASES)}")
    sp.add_argument("--method", default=None, help="how derived")
    sp.add_argument("--blindspots", default=None, help="what `method` cannot see")
    sp.set_defaults(handler=cmd_field)

    sp = sub.add_parser("bind", help="bind a function-local var to a struct type")
    sp.add_argument("func")
    sp.add_argument("var")
    sp.add_argument("type")
    sp.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    sp.set_defaults(handler=cmd_bind)

    sp = sub.add_parser("note", help="attach/list/remove notes")
    sp.add_argument("action", choices=["add", "list", "rm"])
    sp.add_argument("target", nargs="?", help="addr | symbol | symbol@addr | (list filter) | (rm id)")
    sp.add_argument("body", nargs="?")
    sp.add_argument("--tier", default="mechanism", help=f"confidence: {'|'.join(TIERS)}")
    sp.add_argument("--supersedes", default=None,
                    help="comma-separated note ids this note RETIRES (they get a visible "
                         "[SUPERSEDED by #N] prefix + history entry). Use when correcting "
                         "an earlier claim instead of letting both coexist.")
    sp.set_defaults(handler=cmd_note)

    sp = sub.add_parser("seen", help="one-shot triage: label an investigated symbol with a hypothesis")
    sp.add_argument("symbol", help="sub_/data_ token (or applied name) you investigated")
    sp.add_argument("body", help="what it does / your best guess (required, >=8 chars)")
    sp.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    sp.set_defaults(handler=cmd_seen)

    sp = sub.add_parser("audit", help="scan a file for sub_/data_ symbols that have no label")
    sp.add_argument("path", help="file to scan (doc, source, RE writeup, ...)")
    sp.add_argument("--quiet", action="store_true", help="print only the unlabeled symbols")
    sp.set_defaults(handler=cmd_audit)

    # argparse applies old-style mapping interpolation to help strings. Escape
    # the literal percent sign or `--help` prints its internal formatting dict.
    sp = sub.add_parser("coverage", help="how thoroughly one function is RE'd (%% refs labeled + depth)")
    sp.add_argument("target", help="function name / applied name / address")
    sp.set_defaults(handler=cmd_coverage)

    sp = sub.add_parser("config", help="persist tool settings (strict, debt_limit, binary)")
    sp.add_argument("setting", choices=["strict", "debt_limit", "binary"])
    sp.add_argument("value", nargs="?", help="on|off | N | <path> (omit to show current)")
    sp.set_defaults(handler=cmd_config)

    sp = sub.add_parser("vtable",
                        help="dump a vtable from the PE, resolving each slot to a function")
    sp.add_argument("addr", nargs="*",
                    help="vtable VA(s), hex (e.g. 0xbcc27c). Multiple VAs require --slot and classify mono/poly.")
    sp.add_argument("--count", type=int, default=32, help="slots to read (default 32)")
    sp.add_argument("--slot", default=None,
                    help="read ONE slot at this byte offset (e.g. 0x2c) and resolve it")
    sp.add_argument("--base", default=None,
                    help="class name: compare --slot across this RTTI class and descendants when vtables are known")
    sp.add_argument("--all-subobjects", dest="all_subobjects", action="store_true",
                    help="with --base, include secondary multiple-inheritance subobject vtables too")
    sp.set_defaults(handler=cmd_vtable)

    sp = sub.add_parser("deref",
                        help="read N dwords from a VA in the PE (pxw equivalent, for .rdata/data)")
    sp.add_argument("addr", help="VA, hex (e.g. 0xce8a38)")
    sp.add_argument("--count", type=int, default=8, help="dwords to read (default 8)")
    sp.set_defaults(handler=cmd_deref)

    sp = sub.add_parser("d3dstate",
                        help="fixed-function D3D9 state timeline of a function (tracks "
                             "D3DRS_LIGHTING, materials, draws; warns on inert SetMaterial). "
                             "Use BEFORE attributing any per-frame value to a draw.")
    sp.add_argument("target", help="function name or address (e.g. sub_8b3180)")
    sp.add_argument("--at", default=None,
                    help="report the accumulated state at this VA (e.g. 0x8b359c, a draw site)")
    sp.set_defaults(handler=cmd_d3dstate)

    sp = sub.add_parser("runtime",
                        help="read a value LIVE from a running client (resolves .bss/runtime "
                             "pointers deref can't see); launches+kills the client by default")
    sp.add_argument("addr", nargs="?", default=None,
                    help="VA, hex (e.g. 0xeffea4 for SWorld+0x14). Omit with --list-clients.")
    sp.add_argument("--count", type=int, default=1, help="dwords to read (default 1)")
    sp.add_argument("--list-clients", dest="list_clients", action="store_true",
                    help="list running client PIDs (image == configured binary / --exe) and exit. "
                         "Never launches, attaches, or kills anything - use to pick a --pid.")
    sp.add_argument("--pid", type=int, default=None,
                    help="attach to an already-running client (does NOT launch or kill it)")
    sp.add_argument("--exe", default=None, help="client path (default: configured binary)")
    sp.add_argument("--args", default=None, help="command-line args to launch the client with")
    sp.add_argument("--warmup", type=float, default=0.0,
                    help="seconds to wait AFTER launch before the first read - lets the "
                         "client get past splash/init so you don't read ctor-time junk")
    sp.add_argument("--timeout", type=float, default=60.0,
                    help="seconds to poll for the value to be set (default 60)")
    sp.add_argument("--interval", type=float, default=400.0,
                    help="poll interval in ms (default 400)")
    sp.add_argument("--expect", choices=["any", "nonzero", "code"], default="nonzero",
                    help="what counts as 'settled': nonzero (default) | code (a pointer into "
                         "the module - use for function pointers, ignores early transients) | any")
    sp.add_argument("--samples", type=int, default=3,
                    help="consecutive equal reads required before accepting (default 3)")
    sp.add_argument("--watch", action="store_true",
                    help="sample the whole --timeout window and summarise what each offset DOES "
                         "(nonzero%%, distinct values, first-nonzero time) - use instead of "
                         "guessing the instant a per-frame/per-draw value is live")
    sp.add_argument("--no-hide", dest="hide", action="store_false",
                    help="don't move a launched client's window off-screen (default: hide it "
                         "off-screen so it still renders but never pops up)")
    sp.set_defaults(hide=True)
    sp.add_argument("--keep", action="store_true",
                    help="don't kill a client we launched (leave it running)")
    sp.add_argument("--observe", default=None,
                    help="record the read as an observation of this subject (reconciles asserts)")
    sp.set_defaults(handler=cmd_runtime)

    sp = sub.add_parser("break",
                        help="HW-breakpoint capture on a RUNNING client (--pid): exec/write/field. "
                             "Answers 'what writes this field?' / 'regs when EIP hits here?'. "
                             "Detaches without killing the client (KillOnExit=FALSE).")
    sp.add_argument("mode", choices=["exec", "write", "field"],
                    help="exec: trap on EIP==VA, dump regs. write: trap on 4-byte write to ADDR, "
                         "report writer EIP. field: exec-trap at VA, take <reg>+<off> as a field "
                         "address, then WRITE-trap it to find what computes it.")
    sp.add_argument("addr", help="VA (hex) for exec/field; ADDR (hex, code VA or absolute heap) for write")
    sp.add_argument("reg", nargs="?", default=None,
                    help="[field mode] base register holding the instance pointer (e.g. ecx)")
    sp.add_argument("off", nargs="?", default=None,
                    help="[field mode] byte offset of the field (e.g. 0x90)")
    sp.add_argument("--pid", type=int, required=True,
                    help="pid of the running client to attach to (REQUIRED; never launches)")
    sp.add_argument("--read", default=None,
                    help="[exec mode] comma-separated capture exprs: reg | reg+0xN | [reg+0xN]:f32"
                         " (types: f32|i32|u32|hex)")
    sp.add_argument("--hits", type=int, default=1,
                    help="capture this many breakpoint hits before detaching (default 1). For "
                         "write/field this shows the SEQUENCE of writers (e.g. zero-init then the "
                         "real compute); for exec it re-traps the same EIP across frames.")
    sp.add_argument("--timeout", type=float, default=30.0,
                    help="seconds to wait for the breakpoint(s) to fire (default 30)")
    sp.add_argument("--exe", default=None, help="client path (for module-name/base; default: configured binary)")
    sp.add_argument("--verbose", action="store_true",
                    help="trace the debug loop (events, arm counts, exceptions) to stderr")
    sp.set_defaults(handler=cmd_break)

    sp = sub.add_parser("mark", help="set the confidence tier of an existing claim")
    sp.add_argument("target",
                    help="global:NAME | local:FUNC:orig | field:TYPE:0xoff | bind:FUNC:var | note:ID | assert:ID")
    sp.add_argument("tier", help="|".join(TIERS))
    sp.add_argument("--evidence", default=None)
    sp.add_argument("--basis", default=None,
                    help=f"evidence basis ({'|'.join(BASES)}); blocks verified on absence/inferred")
    sp.add_argument("--shallow", action="store_true",
                    help="ack a confident function claim has no locals/fields worth naming")
    sp.set_defaults(handler=cmd_mark)

    sp = sub.add_parser("dispute", help="flag a claim as wrong/suspect (kept visible)")
    sp.add_argument("target", help="global:NAME | local:FUNC:orig | field:TYPE:0xoff | bind:FUNC:var")
    sp.add_argument("reason")
    sp.set_defaults(handler=cmd_dispute)

    sp = sub.add_parser("resolve", help="clear a dispute (optionally re-tier)")
    sp.add_argument("target")
    sp.add_argument("--tier", default=None, help=f"new confidence: {'|'.join(TIERS)}")
    sp.set_defaults(handler=cmd_resolve)

    sp = sub.add_parser("history", help="audit trail for a claim (or recent activity)")
    sp.add_argument("target", nargs="?", help="claim ref; omit for recent activity")
    sp.add_argument("--limit", type=int, default=30)
    sp.set_defaults(handler=cmd_history)

    sp = sub.add_parser("review", help="list claims needing attention (tier/disputed/author)")
    sp.add_argument("--tier", default=None, help=f"filter by tier (default guess): {'|'.join(TIERS)}")
    sp.add_argument("--disputed", action="store_true", help="only disputed claims")
    sp.add_argument("--needs-dynamic", dest="needs_dynamic", action="store_true",
                    help="high-tier claims resting on absence/inference, never confirmed at runtime")
    sp.add_argument("--debt", action="store_true",
                    help="symbols investigated via view/xref that were never labeled")
    sp.add_argument("--by", dest="by_author", default=None, help="filter by author")
    sp.set_defaults(handler=cmd_review)

    sp = sub.add_parser("assert", help="record a testable factual assertion about runtime state")
    asub = sp.add_subparsers(dest="action", required=True)
    aa = asub.add_parser("add", help="add an assertion")
    aa.add_argument("subject", help="data_f0673f | sub_8b3180@008b37b1 | MapRenderer+0x26e1")
    aa.add_argument("claim", help="human statement, e.g. 'statically 0; no writer'")
    aa.add_argument("--predicate", default=None,
                    help="machine check: zero|nonzero|eq:N|ne:N|lt:N|le:N|gt:N|ge:N|range:LO:HI|const|any")
    aa.add_argument("--basis", default="inferred", help=f"{'|'.join(BASES)}")
    aa.add_argument("--method", default=None, help="how derived (search/tool)")
    aa.add_argument("--blindspots", default=None, help="what `method` cannot see")
    aa.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(TIERS)}")
    aa.add_argument("--expected", default=None, help="expected value (text)")
    aa.add_argument("--evidence", default=None)
    aa.set_defaults(handler=cmd_assert)
    al = asub.add_parser("list", help="list assertions")
    al.add_argument("--subject", default=None, help="filter by subject substring")
    al.add_argument("--status", default=None, choices=ASSERT_STATUS)
    al.set_defaults(handler=cmd_assert)

    sp = sub.add_parser("observe", help="record a runtime observation; auto-reconciles assertions")
    sp.add_argument("subject")
    sp.add_argument("value")
    sp.add_argument("--source", default=None, help="where seen (e.g. native-trace.log:19)")
    sp.set_defaults(handler=cmd_observe)

    sp = sub.add_parser("ingest", help="bulk-observe from a native-trace log; reconcile assertions")
    sp.add_argument("log", help="path to sro-native-trace.log")
    sp.set_defaults(handler=cmd_ingest)

    sp = sub.add_parser("sync", help="git-mergeable JSONL export/import for collaboration")
    sp.add_argument("action", choices=["export", "import", "status", "conflicts", "resolve"])
    sp.add_argument(
        "path",
        nargs="?",
        help="default: <selected --db stem>_symbols.jsonl beside the database",
    )
    sp.add_argument("--take", choices=["local", "remote"], default=None,
                    help="with resolve: explicit side to preserve")
    sp.set_defaults(handler=cmd_sync)

    sp = sub.add_parser("export", help="emit a verified_*-style doc from the overlay")
    sp.add_argument("target")
    sp.set_defaults(handler=cmd_export)

    # --- codebase-mapping formats ------------------------------------------- #
    sp = sub.add_parser("asserts",
                        help="[fmt1] scrape ASSERT(file,\"expr\") strings -> source modules + member names")
    sp.add_argument("--file", default=None, help="only functions whose asserts cite this .cpp")
    sp.add_argument("--func", default=None, help="show asserts inside one function")
    sp.add_argument("--suggest", action="store_true", help="list rename/field candidates")
    sp.add_argument("--apply-notes", dest="apply_notes", action="store_true",
                    help="attach a src=<file>; members note to each function (idempotent)")
    sp.add_argument("--limit", type=int, default=40)
    sp.set_defaults(handler=cmd_asserts)

    sp = sub.add_parser("classes",
                        help="[fmt2] RTTI/vftable class hierarchy + per-class vtable slots")
    sp.add_argument("name", nargs="?", help="show one class (bases, subclasses, vtable slots)")
    sp.add_argument("--base", default=None, help="list all subclasses of this base")
    sp.add_argument("--list", action="store_true", help="list every class")
    sp.add_argument("--count", type=int, default=64, help="vtable slots to resolve for a class")
    sp.add_argument("--limit", type=int, default=40)
    sp.set_defaults(handler=cmd_classes)

    sp = sub.add_parser("ingest-calls",
                        help="[fmt3] dynamic call graph; observed `this` vtable -> arg1 type binding")
    sp.add_argument("log", help="JSONL trace ({\"kind\":\"call\",...})")
    sp.add_argument("--apply", action="store_true", help="bind callee arg1 to observed class")
    sp.add_argument("--limit", type=int, default=40)
    sp.set_defaults(handler=cmd_ingest_calls)

    sp = sub.add_parser("ingest-allocs",
                        help="[fmt4] alloc size + ctor vtable -> sizeof(class)")
    sp.add_argument("log", help="JSONL trace (alloc/ctor records)")
    sp.add_argument("--apply", action="store_true", help="record sizeof notes/observations")
    sp.set_defaults(handler=cmd_ingest_allocs)

    sp = sub.add_parser("ingest-layout",
                        help="[fmt5] guard-page field accesses -> struct fields")
    sp.add_argument("log", help="JSONL trace ({\"kind\":\"field\",...})")
    sp.add_argument("--apply", action="store_true", help="create fields for unnamed offsets")
    sp.set_defaults(handler=cmd_ingest_layout)

    sp = sub.add_parser("infer-types",
                        help="[fmt6] value samples -> field/global type + dynamic assertion")
    sp.add_argument("log", help="JSONL trace ({\"kind\":\"sample\",...})")
    sp.add_argument("--apply", action="store_true", help="set field types + record assertions")
    sp.set_defaults(handler=cmd_infer_types)

    sp = sub.add_parser("reconstruct",
                        help="[fmt7] emit typed C for a function (struct decls + field-rewritten body)")
    sp.add_argument("target", help="function name / applied name / address")
    sp.add_argument("--out", default=None, help="write to a file instead of stdout")
    sp.set_defaults(handler=cmd_reconstruct)

    return p


def main(argv):
    parser = build_parser()
    args = parser.parse_args(argv)
    # index needs an explicit/auto dump even before meta exists
    if args.cmd == "index" and not args.dump:
        args.dump = DEFAULT_DUMP
    con = open_db(args.db)
    try:
        return args.handler(args, con)
    finally:
        con.close()


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
