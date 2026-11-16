"""
===========================================================================
revtool_rizin.py - revtool's rizin backend

The dump backend reads a whole-program Binary Ninja HLIL export
(~110 MB of text that can never be committed). This backend reads the
same facts from the contributor's own retail binary via rizin
(rzpipe) instead: the function index from `aflj`, signatures from
`afsj`, a function's text from `pdfj` disassembly, cross references
from `axtj`. Commands that never touched the dump (they read the
overlay DB) delegate to revtool unchanged: `find`, `audit`, `rename`,
`doctor`, `sync`. `revtool.py --backend rizin` dispatches here; this
module is also runnable on its own, with the same argument shapes.

    py revtool_rizin.py --binary SRO_Client.exe index
    py revtool_rizin.py --binary SRO_Client.exe view sub_402a70
    py revtool_rizin.py --binary SRO_Client.exe xref data_f0c90c
    py revtool_rizin.py --binary SRO_Client.exe coverage sub_679b90
    py revtool_rizin.py --binary SRO_Client.exe rename global sub_402a70 WidgetDraw
    py revtool_rizin.py --binary SRO_Client.exe doctor

Everything revtool does with the data - the overlay, rename
application, notes, debt gates - is revtool's code, imported and
applied unmodified; only the source of the facts differs.

Semantics that differ from the dump backend, by construction:

  * Function set: rizin finds what its analysis reaches (~23k
    functions with `aac`, ~24k adding `aap`, on SRO_Client.exe) where
    the BN index holds ~95k. Indexing prints the count; nothing in
    this backend claims parity.
  * Names: rizin's `fcn.<8hex>` (and the `sub.` spelling it sometimes
    chooses) normalize to BN's `sub_<hex>` overlay spelling; other
    symbols (imports, entry points) keep rizin's name. Spelling is a
    display normalization, never a claim.
  * Signatures are rizin's (`afsj`): rizin types and argument names
    (`arg_8h`), not BN's reconstructed prototypes.
  * A function's "lines" are its disassembly instructions at their
    true addresses - there is no decompiler in rizin 0.8.2 (`pdg`/
    `pdc` do not exist), so `view` shows assembly, and the
    pseudo-C field-access shapes `annotate_fields` looks for mostly
    do not occur. Coverage therefore measures references exactly (from
    `aflj` call/data refs) while its field-depth number means "not
    measurable from disassembly" when it reports no accesses; it never
    invents offsets a disassembly line does not contain.
  * `xref` is analysis-derived (`axtj`): call and data references
    rizin resolved. Un-analyzed code is invisible to it, and each
    answer says so.
  * `rename` stores the same overlay claims as the dump backend. Its
    can-fire test consults this backend's function index (a `data_`
    key is absent from a function index by construction, so the test
    stays silent for one, exactly as it does when the dump is
    unavailable), and the strict depth gate measures the function's
    disassembly lines - which rarely carry the `var + 0xNN` field
    shape, so the gate normally passes unmeasured instead of refusing
    on a guess. `doctor` on a rizin DB audits this backend's
    provenance (target/index binding, the saved project, the ledger),
    not the BN evidence graph. `sync` is revtool's own, unchanged.
  * `start_byte`/`start_line` in the functions table are dump-backend
    geometry; rizin rows carry a stable ordinal in `start_byte` and
    zeros elsewhere. Database files are per-backend: run one
    backend's `index` per DB (the meta table records `backend`).

Run with the pinned venv from docs/native/REVERSE_ENGINEERING.md
(rzpipe is not a runtime dependency of the repo).
===========================================================================
"""

from __future__ import annotations

import argparse
import bisect
import json
import os
import re
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
try:
	import revtool
except ImportError:  # repo layout not merged yet: allow an explicit home
	_home = os.environ.get("REVTOOL_HOME")
	if _home and os.path.isfile(os.path.join(_home, "revtool.py")):
		sys.path.insert(0, _home)
		import revtool
	else:
		raise SystemExit(
			"error: revtool_rizin needs revtool.py beside it "
			"(tools/re/revtool/) or REVTOOL_HOME pointing at it.")

RIZIN_VERSION = "0.8.2"
ANALYSIS_CHOICES = ("aac", "aap", "aaa", "none")
# What `index` runs on a loaded project before it reads the function
# set: `aar` finds references without redrawing the seed's functions.
PROJECT_REFS_CHOICES = ("aar", "aac", "none")
# rizin 0.8 has no `?e` and parses `@@` as an iterator: a batch
# using either failed to parse whole and every signature came back
# empty (blind dogfood, 2026-11-09). `echo` of a plain word is safe.
SIGNATURE_SEPARATOR = "REVTOOL-AFSJ-SEP"
SIGNATURE_BATCH_BYTES = 4096


def signature_command(va):
	"""One batch entry: the function's afsj, then the echoed separator."""
	return f"afsj @ {va:#x}\necho {SIGNATURE_SEPARATOR}"


# Character widths of izj string types, for walking a string's
# character boundaries when its first one carries no reference.
STRING_REF_SCAN_MAX = 16
STRING_CHAR_BYTES = {"utf16le": 2, "utf16be": 2, "utf32le": 4, "utf32be": 4}


# =====================================================================
# rizin session
# =====================================================================

class RizinSession:
	"""One rzpipe connection to one retail binary."""

	def __init__(self, binary, rzdb=None, analyze="aac"):
		try:
			import rzpipe
		except ImportError as exc:
			raise SystemExit(
				"error: rzpipe is not importable; use the pinned venv "
				"(docs/native/REVERSE_ENGINEERING.md).") from exc
		self.binary = binary
		self.rzdb = rzdb
		self.from_project = bool(rzdb and os.path.exists(rzdb))
		self._functions = None
		self._strings = None
		# No CLI flags: rzpipe adds its own; an unknown flag makes rizin
		# print usage, exit, and rzpipe spin forever on a dead pipe.
		self._pipe = rzpipe.open(binary)
		self._pipe.cmd("e scr.color=false")
		if rzdb and os.path.exists(rzdb):
			self._pipe.cmd(f'Po "{rzdb}"')
		elif analyze and analyze != "none":
			self._pipe.cmd(analyze)

	def cmd(self, text):
		return self._pipe.cmd(text) or ""

	def cmdj(self, text):
		out = self.cmd(text)
		try:
			return json.loads(out) if out.strip() else None
		except json.JSONDecodeError:
			return None

	def functions(self):
		"""aflj rows for the analyzed binary (cached for this session)."""
		if self._functions is None:
			self._functions = self.cmdj("aflj") or []
		return self._functions

	def signature_parts(self, va):
		"""afsj for one function: {ret, cc, args:[{name, type}]}."""
		return self.cmdj(f"afsj @ {va:#x}") or {}

	def signature_parts_many(self, vas):
		"""afsj for many functions, {va: parts}, in few round trips.

		One afsj per rzpipe call cost 3.4 minutes on the seeded client
		(94,874 round trips, measured 2026-11-09). Each call here sends
		a batch with an echoed separator after every function, so an
		empty answer can never shift the next function's parts onto
		this one. Batches stay under the byte cap one rzpipe cmd()
		honours (the same cap revtool_seed's apply batches keep).
		"""
		parts = {}
		batch = []
		size = 0
		for va in vas:
			command = signature_command(va)
			if batch and size + len(command) + 1 > SIGNATURE_BATCH_BYTES:
				parts.update(self._signature_batch(batch))
				batch, size = [], 0
			batch.append(va)
			size += len(command) + 1
		if batch:
			parts.update(self._signature_batch(batch))
		return parts

	def _signature_batch(self, vas):
		text = "\n".join(signature_command(va) for va in vas)
		answers = self.cmd(text).split(SIGNATURE_SEPARATOR)
		parts = {}
		for va, answer in zip(vas, answers):
			answer = answer.strip()
			try:
				parts[va] = json.loads(answer) if answer else {}
			except json.JSONDecodeError:
				parts[va] = {}
		return parts

	def analyze_project(self, step):
		"""Run one analysis step on a loaded project; seconds taken.

		A seed-applied .rzdb holds hand-defined functions and nothing
		else: no references, so every xref answers 0 until rizin reads
		the code once. `aar` (references only) takes about 7 s on the
		seeded client (measured 2026-11-09) and keeps the seed's
		function set.
		"""
		started = time.monotonic()
		self.cmd(step)
		self._functions = None
		return time.monotonic() - started

	def strings(self):
		"""izj rows for the binary's data strings (cached)."""
		if self._strings is None:
			self._strings = self.cmdj("izj") or []
		return self._strings

	def func_ops(self, va):
		"""pdfj instruction dicts for one function, address order."""
		doc = self.cmdj(f"pdfj @ {va:#x}") or {}
		return doc.get("ops") or []

	def xrefs_to(self, va):
		"""axtj rows pointing at va: [{from, to, type}]."""
		self.cmd(f"s {va:#x}")
		return self.cmdj("axtj") or []

	def save(self):
		"""Save the project beside-then-replace, under an exclusive lock.

		`Ps` writes the .rzdb in place: two sessions saving at once (or
		a crash mid-write) leave a torn project behind. Write a sibling
		temp file and os.replace it into place instead, holding an
		O_EXCL lock so a second saver refuses rather than interleaving
		- the same rule the seed apply uses (revtool_seed.apply_seed).
		"""
		if not self.rzdb:
			return
		lock_path = self.rzdb + ".lock"
		try:
			lock_fd = os.open(lock_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY)
		except FileExistsError as exc:
			raise SystemExit(
				f"error: {lock_path} exists - another revtool session is "
				f"saving {self.rzdb}; remove the lock only if it is gone") from exc
		tmp_path = f"{self.rzdb}.tmp-{os.getpid()}"
		try:
			os.write(lock_fd, f"{os.getpid()}\n".encode())
			os.close(lock_fd)
			lock_fd = None
			self.cmd(f'Ps "{tmp_path}"')
			if not os.path.exists(tmp_path):
				raise SystemExit(
					f"error: rizin did not write {tmp_path}; project not saved")
			os.replace(tmp_path, self.rzdb)
		finally:
			if lock_fd is not None:
				os.close(lock_fd)
			if os.path.exists(tmp_path):
				os.remove(tmp_path)
			if os.path.exists(lock_path):
				os.remove(lock_path)

	def close(self):
		try:
			self._pipe.quit()
		except Exception:
			pass


# =====================================================================
# Spelling: rizin names <-> revtool tokens
# =====================================================================

_ANALYZED_NAME_RE = re.compile(r"^(?:fcn|sub)\.([0-9a-fA-F]{8})$")
_ANALYZED_NAME_PREFIX_RE = re.compile(r"^(?:fcn|sub)\.[0-9a-fA-F]{8}\b")
_CONV = {
	"fastcall": "__fastcall", "thiscall": "__thiscall",
	"stdcall": "__stdcall", "cdecl": "__cdecl",
}


def bn_token(name, va):
	"""BN overlay spelling for one analyzed function.

	`fcn.00402a70` -> `sub_402a70` (revtool's `sub_` + addr without
	leading zeros); any other name (imports, entry labels) passes
	through unchanged.
	"""
	if name and _ANALYZED_NAME_RE.match(name):
		return "sub_" + format(va, "x")
	return name or ("sub_" + format(va, "x"))


def bn_signature(entry, sig_parts, token):
	"""One `ret __conv sub_ADDR(type name, ...)` line from rizin facts."""
	if sig_parts:
		ret = sig_parts.get("ret") or "int32_t"
		conv = _CONV.get(sig_parts.get("cc") or "", "")
		args = []
		for a in sig_parts.get("args") or []:
			args.append(f"{a.get('type') or 'int32_t'} {a.get('name') or ''}".rstrip())
		mid = f" {conv}" if conv else ""
		return f"{ret}{mid} {token}({', '.join(args)})"
	raw = (entry.get("signature") or "").rstrip().rstrip(";")
	raw = _ANALYZED_NAME_PREFIX_RE.sub(lambda _m: token, raw, count=1)
	return raw if raw else token + "()"


def token_maps(con):
	"""VA -> overlay token for functions and named data (from the DB)."""
	func_by_va = {}
	for addr, name in con.execute("SELECT addr,name FROM functions"):
		va = int(addr, 16)
		func_by_va[va] = name
	data_by_va = {}
	for (orig,) in con.execute(
			"SELECT orig_name FROM globals WHERE orig_name LIKE 'data_%'"):
		try:
			data_by_va[int(orig[5:], 16)] = orig
		except ValueError:
			pass
	for va, name in func_by_va.items():
		data_by_va.setdefault(va, name)
	return func_by_va, data_by_va


_FCN_TEXT_RE = re.compile(r"\b(?:fcn|sub)\.([0-9a-fA-F]{8})\b")
_DATA_TEXT_RE = re.compile(r"\b0x([0-9a-fA-F]{4,8})\b")


def normalize_line(text, data_by_va):
	"""BN spelling inside one disassembly line.

	Call targets become `sub_...`; a bare address that the overlay
	knows as a named datum becomes its `data_...` token. Everything
	else (registers, immediates, comments) is rizin's text unchanged.
	"""
	text = _FCN_TEXT_RE.sub(lambda m: "sub_" + m.group(1).lower().lstrip("0"), text)

	def data_sub(m):
		va = int(m.group(1), 16)
		return data_by_va.get(va, m.group(0))

	return _DATA_TEXT_RE.sub(data_sub, text)


def function_lines(rz, va, signature, data_by_va, raw=False):
	"""Addressed text lines for one function, header first.

	Each line is `<8hex><2sp><code>` - the dump's wire shape - but the
	addresses are the instructions' real addresses and the code is
	rizin's disassembly, BN-spelled unless `raw`.
	"""
	header_name = signature
	ops = rz.func_ops(va)
	lines = [f"{va:08x}  {header_name}"]
	for op in ops:
		off = op.get("offset")
		if off is None:
			continue
		code = op.get("disasm") or op.get("opcode") or ""
		if not raw:
			code = normalize_line(code, data_by_va)
		lines.append(f"{off:08x}  {code}")
	return lines


_ANONYMOUS_FUNC_RE = re.compile(r"^(?:j_)*(?:sub|nullsub)_[0-9a-fA-F]+(?:_\d+)?$")


def is_labeled(con, name):
	"""True if a function token reads as RE'd: a real name or a claim.

	A seed-applied project carries BN's function names in the index
	itself, so a name that is not an anonymous spelling is a label
	already; only sub_/j_sub_/nullsub_ tokens need the overlay's
	vouching (revtool.symbol_is_annotated).
	"""
	if name and not _ANONYMOUS_FUNC_RE.match(name):
		return True
	return revtool.symbol_is_annotated(con, name)


def owner_of(func_rows, va):
	"""Owning function for an address: greatest function start <= va."""
	starts = [r[0] for r in func_rows]
	i = bisect.bisect_right(starts, va) - 1
	if 0 <= i < len(func_rows):
		return func_rows[i][1]
	return "?"


# =====================================================================
# index
# =====================================================================

def cmd_index(args, con, rz):
	binary = revtool.get_binary(con, args) or getattr(args, "binary", None)
	if not binary or not os.path.isfile(binary):
		print(f"error: binary not found: {binary}", file=sys.stderr)
		return 1

	from_project = getattr(rz, "from_project", False)
	refs_step = getattr(args, "refs", "aar") or "none"
	refs_seconds = None
	if from_project and refs_step != "none":
		# A seed-applied project has functions but no references
		# until rizin reads the code (README, the contributor seed).
		refs_seconds = rz.analyze_project(refs_step)

	entries = [e for e in rz.functions() if e.get("offset") is not None]
	entries.sort(key=lambda e: e["offset"])
	signatures = rz.signature_parts_many([e["offset"] for e in entries])
	rows = []
	for ordinal, entry in enumerate(entries):
		va = entry["offset"]
		token = bn_token(entry.get("name"), va)
		sig = bn_signature(entry, signatures.get(va, {}), token)
		# start_byte is a stable ordinal; line geometry is the dump
		# backend's and stays zero here (module docstring).
		rows.append((f"{va:08x}", token, sig, ordinal, ordinal + 1, 0, 0))

	con.execute("DELETE FROM functions")
	con.executemany(
		"INSERT OR REPLACE INTO functions "
		"(addr,name,signature,start_byte,end_byte,start_line,end_line) "
		"VALUES (?,?,?,?,?,?,?)", rows)
	# What actually produced the facts: a loaded project plus the
	# reference step run on it, or a fresh session's analysis.
	analysis = (f"project+{refs_step}" if from_project
	            else getattr(args, "analyze", "aac"))
	revtool.meta_set(con, "backend", "rizin")
	revtool.meta_set(con, "target_sha256", revtool.sha256_file(binary))
	revtool.meta_set(con, "rizin_analysis", analysis)
	revtool.record_artifact(
		con, "target", binary, record_count=len(rows), producer="PE file")
	revtool.record_artifact(
		con, "rizin-analysis", rz.rzdb if rz.rzdb and os.path.exists(rz.rzdb) else binary,
		record_count=len(rows), producer="rizin aflj analysis",
		version=RIZIN_VERSION, settings={"analysis": analysis})
	rz.save()
	con.commit()
	named = sum(1 for row in rows if not row[1].startswith("sub_"))
	if from_project:
		print(f"indexed {len(rows)} functions ({named} named) from the loaded "
		      f"project {rz.rzdb}.")
		if refs_seconds is None:
			print("note: --refs none: no reference analysis ran, so xref and "
			      "strings answer from whatever references the project holds "
			      "(none, for a fresh seed apply).")
		else:
			print(f"note: ran rizin `{refs_step}` on the project for references "
			      f"({refs_seconds:.1f}s) and saved it; the function set is the "
			      f"project's (a seed-applied .rzdb carries BN-grade boundaries "
			      f"and names), not fresh rizin analysis.")
	else:
		print(f"indexed {len(rows)} functions ({named} named) from rizin "
		      f"({getattr(args, 'analyze', 'aac')} analysis of {binary}).")
		print("note: rizin sees analysis-reachable code only; the BN dump "
		      "index holds roughly 4x this many functions. The two "
		      "backends are not interchangeable views of one set.")
	return 0


# =====================================================================
# view
# =====================================================================

def cmd_view(args, con, rz):
	row = revtool.resolve_func(con, args.target)
	if not row:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		print("  (run `index` first, or try `find`)", file=sys.stderr)
		return 1
	addr, name, sig, _sb, _eb, _sl, _el = row
	va = int(addr, 16)
	gate = revtool.investigation_gate(con, args, name)
	if gate is not None:
		return gate
	_func_by_va, data_by_va = token_maps(con)
	lines = function_lines(rz, va, sig, data_by_va, raw=args.raw)

	# --lines is a FUNCTION-LOCAL 1-based range under this backend
	# (the dump's are whole-dump line numbers; see module docstring).
	lo_line = hi_line = None
	if getattr(args, "lines", None):
		parts = args.lines.split(":")
		lo_line = int(parts[0]) if parts[0] else None
		hi_line = int(parts[1]) if len(parts) > 1 and parts[1] else None
		if (lo_line is not None and lo_line > len(lines)) or \
				(hi_line is not None and hi_line < 1):
			print(f"warning: --lines {args.lines} is outside {name} "
			      f"(local lines 1-{len(lines)}); nothing to show.",
			      file=sys.stderr)

	def in_range(i):
		n = i + 1
		if lo_line is not None and n < lo_line:
			return False
		if hi_line is not None and n > hi_line:
			return False
		return True

	any_shown = any(in_range(i) for i in range(len(lines)))
	if args.raw:
		if any_shown:
			revtool.warn_if_previously_investigated(con, name, stream=sys.stderr)
		print("\n".join(ln for i, ln in enumerate(lines) if in_range(i)))
		if any_shown:
			revtool.record_access(con, name, "viewed", revtool.get_author(args))
			con.commit()
			revtool.print_debt_footer(con, name, author=revtool.get_author(args))
		return 0

	globals_map = revtool.load_globals(con)
	g_self = globals_map.get(name)
	applied_name = g_self[0] if g_self else None
	locals_map = revtool.load_locals(con, addr)
	bindings = revtool.load_bindings(con, addr)
	fields_by_type = {t: revtool.load_fields(con, t)
	                  for t in set(bindings.values())}
	rename = revtool.build_rename_map(globals_map, locals_map)
	line_notes = revtool.load_line_notes(con, addr, name)

	body_text = "\n".join(ln for i, ln in enumerate(lines) if in_range(i))
	body_tokens = set(re.findall(r"[A-Za-z_]\w*", body_text))
	note_targets = {name, applied_name}
	note_targets |= set(bindings.values())
	field_note_items = []
	for tname, fmap in fields_by_type.items():
		for off, (fnew, ftype, fnote, ftier, fdisp, freason) in fmap.items():
			note_targets.add(fnew)
			if fnote:
				field_note_items.append((f"{tname}+0x{off:x} {fnew}", ftier, fnote))
	for orig, vals in {**globals_map, **locals_map}.items():
		if orig in body_tokens or (vals[0] and vals[0] in body_tokens):
			note_targets.add(orig)
			note_targets.add(vals[0])
	related_notes = revtool.collect_related_notes(con, note_targets)

	print("=" * 78)
	title = name + (f"  ->  {applied_name}" if applied_name else "")
	tier_tag = f"  [{g_self[3]}]" if g_self else ""
	print(f"{title}   @ 0x{addr}   ({len(lines)} lines, rizin disassembly){tier_tag}")
	if g_self and g_self[4]:
		print(f"  !! DISPUTED: {g_self[5] or 'see `revtool history`'}")
	if g_self and g_self[2]:
		print(f"  role: {g_self[2]}")
	if bindings:
		b = ", ".join(f"{v}:{t}" for v, t in sorted(bindings.items()))
		print(f"  bindings: {b}")
	disputed_used = []
	body_all = "\n".join(lines)
	for orig, vals in {**globals_map, **locals_map}.items():
		if vals[4] and re.search(r"\b" + re.escape(orig) + r"\b", body_all):
			disputed_used.append((orig, vals[0], vals[5]))
	if disputed_used:
		print("  ---- DISPUTED symbols used here (do not trust): ----")
		for orig, new, reason in disputed_used:
			print(f"     {new} <- {orig}: {reason or 'see history'}")
	if any_shown:
		revtool.warn_if_previously_investigated(con, name)
	print("=" * 78)
	revtool.render_notes_banner(related_notes, field_note_items)

	used = {}
	for i, ln in enumerate(lines):
		if not in_range(i):
			continue
		m = re.match(r"^([0-9a-fA-F]{8})(\s+)(.*)$", ln)
		if not m:
			print(ln)
			continue
		laddr, ws, code = m.group(1).lower(), m.group(2), m.group(3)
		field_hits = revtool.annotate_fields(code, bindings, fields_by_type, rename)
		shown = revtool.apply_renames(code, rename) if not args.no_apply else code
		if not args.no_apply:
			for orig in rename:
				if re.search(r"\b" + re.escape(orig) + r"\b", code):
					used[orig] = rename[orig]
		ann = []
		if field_hits:
			ann.extend(field_hits)
		if laddr in line_notes:
			ann.extend("NOTE: " + n for n in line_notes[laddr])
		suffix = "    // " + " | ".join(ann) if ann else ""
		print(f"{laddr}{ws}{shown}{suffix}")

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
	if any_shown:
		revtool.record_access(con, name, "viewed", revtool.get_author(args))
		con.commit()
		revtool.print_debt_footer(con, name, author=revtool.get_author(args))
	return 0


# =====================================================================
# rename
# =====================================================================

def cmd_rename(args, con, rz):
	"""`rename` over the rizin provider: revtool stores the claim.

	Only the depth gate's text source differs from the dump backend:
	for a global rename at a confident tier under strict enforcement,
	the function's disassembly lines (the same lines `view` renders)
	are measured for struct-field offsets. Everything else - the
	can-fire warning, the claim itself, its history - is
	revtool.cmd_rename unmodified. The session opens lazily, so
	guess-tier and local renames never pay for rizin.
	"""
	if args.scope == "global" and args.tier in revtool.DEEP_TIERS \
			and revtool.strict_enabled(args, con):
		row = revtool.resolve_func(con, args.a)
		if row:
			session = open_session(args, con, True)
			try:
				_func_by_va, data_by_va = token_maps(con)
				args.gate_lines = function_lines(
					session, int(row[0], 16), row[2], data_by_va)
			finally:
				session.close()
	return revtool.cmd_rename(args, con)


# =====================================================================
# xref
# =====================================================================

def cmd_xref(args, con, rz):
	gmap = revtool.load_globals(con)
	scan_sym = revtool.resolve_scan_symbol(con, args.symbol)
	m = re.fullmatch(r"(?:sub|data)_([0-9a-fA-F]+)", scan_sym or "")
	if m:
		va = int(m.group(1), 16)
	else:
		# A function's own name (a seed-applied project carries BN's)
		# is as address-keyed as its sub_ token: the index holds both.
		row = con.execute(
			"SELECT addr FROM functions WHERE name = ?", (scan_sym,)).fetchone()
		if not row:
			print(f"error: rizin xref needs a sub_/data_ symbol or an indexed "
			      f"function name (got {args.symbol!r} -> {scan_sym!r}); "
			      f"analysis xrefs are address-keyed. Use `find` for text "
			      f"queries, `strings` for string data.", file=sys.stderr)
			return 1
		va = int(row[0], 16)
	gate = revtool.investigation_gate(con, args, scan_sym)
	if gate is not None:
		return gate
	func_rows = sorted(
		(int(a, 16), n) for a, n in
		con.execute("SELECT addr,name FROM functions"))
	_func_by_va, data_by_va = token_maps(con)

	refs = rz.xrefs_to(va)
	hits = []
	for ref in refs:
		src = ref.get("from")
		if src is None:
			continue
		line = ""
		doc = rz.cmdj(f"pdj 1 @ {src:#x}")
		if doc:
			line = doc[0].get("disasm") or doc[0].get("opcode") or ""
		hits.append((f"{src:08x}", owner_of(func_rows, src),
		             normalize_line(line, data_by_va), ref.get("type")))

	applied = gmap.get(scan_sym)
	label = scan_sym + (f" ({applied[0]})" if applied else "")
	if scan_sym != args.symbol:
		label = f"{args.symbol} -> {label}"
	print(f"xref {label}: {len(hits)} reference(s)")
	print("note: rizin xrefs are analysis-derived (axtj); code rizin "
	      "has not analyzed is invisible here.", file=sys.stderr)
	if not hits and revtool.meta_get(con, "rizin_analysis") == "project+none":
		print("warning: this project was indexed with --refs none, so it holds "
		      "no references; rerun `index` (it runs `aar`) before trusting 0.",
		      file=sys.stderr)
	identified = revtool.symbol_is_identifiable(
		con, scan_sym, None, found_in_dump=bool(hits), consult_dump=False)
	# A name resolved through the index is a real function: never "unknown".
	named_func = not m
	if not identified and not named_func:
		if hits:
			print(f"note: {scan_sym} has references but is not a labelable "
			      f"anonymous symbol; treating it as a research query, not "
			      f"rename debt.", file=sys.stderr)
		else:
			print(f"warning: {scan_sym} is not a known symbol, so 0 "
			      f"references is NOT evidence that it is unreferenced - "
			      f"check the spelling (`find`).", file=sys.stderr)
	if identified:
		revtool.warn_if_previously_investigated(con, scan_sym)
	last_fn = None
	for laddr, fname, text, _rtype in hits[: args.limit]:
		fn_new = gmap.get(fname)
		fn_disp = fname + (f"/{fn_new[0]}" if fn_new else "")
		if fname != last_fn:
			tag = ""
			if fname not in ("?", scan_sym):
				tag = "  [labeled]" if is_labeled(con, fname) else "  [NEEDS RE]"
			print(f"\n  in {fn_disp}:{tag}")
			last_fn = fname
		print(f"    {laddr}  {text}")
	if len(hits) > args.limit:
		print(f"\n... {len(hits) - args.limit} more (use --limit)")

	author = revtool.get_author(args)
	callers = []
	for _laddr, fname, _text, _rtype in hits:
		if fname in ("?", scan_sym) or fname in callers:
			continue
		callers.append(fname)
	unlabeled = [f for f in callers if not is_labeled(con, f)]
	if callers:
		print(f"\ncallers: {len(callers)}  |  unlabeled (need RE): {len(unlabeled)}")
	if not identified:
		print(f"\nno investigation debt charged: {scan_sym} is a search "
		      f"probe or an already-named function, not an anonymous "
		      f"program symbol; there is nothing to label.", file=sys.stderr)
		return 0
	revtool.record_access(con, scan_sym, "xref", author)
	for fname in unlabeled:
		revtool.record_access(con, fname, "caller", author)
	con.commit()
	revtool.print_debt_footer(con, scan_sym, author=author)
	return 0


# =====================================================================
# strings
# =====================================================================

def string_ref_offsets(row, match_start):
	"""Byte offsets from a string's vaddr where a reference may land.

	rizin's izj sometimes starts a string a character or two early: it
	reported `oUIIT_MSG_GUILD_RECALL_ANSWER` at 0xbd4a8e while the code
	points at 0xbd4a90 (seen 2026-11-09). So every character boundary
	up to the match is a candidate, not only vaddr, capped so a long
	prefix cannot cost hundreds of round trips.
	"""
	width = STRING_CHAR_BYTES.get(row.get("type"), 1)
	chars = min(match_start, STRING_REF_SCAN_MAX - 1)
	return [i * width for i in range(chars + 1)]


def cmd_strings(args, con, rz):
	"""Strings matching PATTERN, and the functions that reference them.

	A contributor's first move on a feature is usually its UI or log
	text: this goes straight from that text to the code using it.
	"""
	try:
		rx = re.compile(args.pattern if args.regex else re.escape(args.pattern), re.I)
	except re.error as exc:
		print(f"error: bad --regex pattern: {exc}", file=sys.stderr)
		return 1
	func_rows = sorted(
		(int(a, 16), n) for a, n in con.execute("SELECT addr,name FROM functions"))
	if not func_rows:
		print("warning: no function index; run `index` first or callers show as ?",
		      file=sys.stderr)
	matches = []
	for row in rz.strings():
		m = rx.search(row.get("string") or "")
		if m and row.get("vaddr") is not None:
			matches.append((row, m.start()))
	print(f"strings /{args.pattern}/: {len(matches)} match(es)")
	n_unref = 0
	for row, start in matches[: args.limit]:
		vaddr = row["vaddr"]
		users = {}
		for off in string_ref_offsets(row, start):
			for ref in rz.xrefs_to(vaddr + off):
				src = ref.get("from")
				if src is not None:
					users.setdefault(owner_of(func_rows, src), []).append(src)
		print(f"\n  {vaddr:08x}  {row.get('type', '?'):7s} {row['string']!r}")
		if not users:
			n_unref += 1
			print("    (no analysis references)")
		for fname, srcs in sorted(users.items()):
			tag = "" if fname == "?" else (
				"  [labeled]" if is_labeled(con, fname) else "  [NEEDS RE]")
			at = ", ".join(f"{s:08x}" for s in sorted(srcs))
			print(f"    {fname}{tag}  @ {at}")
	if len(matches) > args.limit:
		print(f"\n... {len(matches) - args.limit} more (use --limit)")
	if n_unref and revtool.meta_get(con, "rizin_analysis") == "project+none":
		print("warning: indexed with --refs none; rerun `index` before trusting "
		      "'no analysis references'.", file=sys.stderr)
	return 0


# =====================================================================
# coverage
# =====================================================================

def cmd_coverage(args, con, rz):
	row = revtool.resolve_func(con, args.target)
	if not row:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		return 255
	addr, name, sig, _sb, _eb, _sl, _el = row
	va = int(addr, 16)
	entry = next((e for e in rz.functions() if e.get("offset") == va), None)
	if entry is None:
		print(f"error: {name} is indexed but rizin has not analyzed it; "
		      f"rerun `index` (or analyze deeper).", file=sys.stderr)
		return 255

	callees = set()
	datasyms = set()
	for ref in entry.get("callrefs") or []:
		to = ref.get("to")
		if to is not None and to != va:
			callees.add("sub_" + format(to, "x"))
	for ref in entry.get("datarefs") or []:
		to = ref.get("to")
		if to is not None:
			datasyms.add("data_" + format(to, "x"))
	refs = sorted(callees) + sorted(datasyms)
	unl_callees = [t for t in sorted(callees)
	               if not revtool.symbol_is_annotated(con, t)]
	unl_data = [t for t in sorted(datasyms)
	            if not revtool.symbol_is_annotated(con, t)]
	n_unl = len(unl_callees) + len(unl_data)
	sym_pct = 100.0 if not refs else 100.0 * (len(refs) - n_unl) / len(refs)
	noteonly = [t for t in refs
	            if t not in unl_callees and t not in unl_data
	            and not revtool.symbol_is_renamed(con, t)]
	n_noteonly = len(noteonly)
	renamed = len(refs) - n_unl - n_noteonly
	read_pct = 100.0 if not refs else 100.0 * renamed / len(refs)

	_func_by_va, data_by_va = token_maps(con)
	lines = function_lines(rz, va, sig, data_by_va)
	offsets = revtool.function_field_offsets(lines)
	bindings = revtool.load_bindings(con, addr)
	fields_by_type = {t: revtool.load_fields(con, t)
	                  for t in set(bindings.values())}
	named_off = 0
	unl_offsets = []
	for var, off in sorted(offsets):
		t = bindings.get(var)
		if t and fields_by_type.get(t, {}).get(off):
			named_off += 1
		else:
			unl_offsets.append((var, off, t))
	depth_pct = 100.0 if not offsets else 100.0 * named_off / len(offsets)

	self_named = revtool.symbol_is_annotated(con, name)
	self_deep = revtool.depth_satisfied(con, name, addr)
	structured = len(offsets) >= revtool.STRUCTURE_MIN_OFFSETS
	if self_deep:
		depth_label = "deep"
	elif not structured:
		depth_label = "flat (no struct depth to capture)"
	else:
		depth_label = "SHALLOW (owes a breadcrumb)"
	applied = (revtool.load_globals(con).get(name) or [None])[0]

	disp = name + (f" -> {applied}" if applied else "")
	print(f"coverage of {disp} @ 0x{addr}  (rizin refs; field depth from disassembly)")
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


# =====================================================================
# Main
# =====================================================================

def open_session(args, con, need_rizin):
	if not need_rizin:
		return None
	binary = revtool.get_binary(con, args) or getattr(args, "binary", None) \
		or revtool.DEFAULT_BINARY
	rzdb = getattr(args, "rzdb", None)
	if rzdb is None:
		rzdb = os.path.splitext(args.db)[0] + ".rzdb"
	return RizinSession(binary, rzdb=rzdb, analyze=getattr(args, "analyze", "aac"))


def main(argv=None) -> int:
	parser = argparse.ArgumentParser(
		prog="revtool-rizin",
		description="revtool with the rizin backend (no BN dump needed).")
	parser.add_argument("--db", default=revtool.DEFAULT_DB, help="overlay database path")
	parser.add_argument("--binary", default=None,
	                    help="retail binary to analyze (default: revtool's)")
	parser.add_argument("--author", default=None,
	                    help="attribute claims to this author (default: $REVTOOL_AUTHOR or OS user)")
	parser.add_argument("--strict", action="store_true",
	                    help="force strict enforcement for this call")
	parser.add_argument("--no-strict", dest="no_strict", action="store_true",
	                    help="disable strict enforcement for this call")
	parser.add_argument("--rzdb", default=None,
	                    help="rizin project file (default: <db>.rzdb; loaded if present, saved on index)")
	parser.add_argument("--analyze", choices=ANALYSIS_CHOICES, default="aac",
	                    help="analysis for a fresh session (default aac; aaa is thorough and slow)")
	sub = parser.add_subparsers(dest="cmd", required=True)

	sp = sub.add_parser("index", help="build the function index from rizin (aflj)")
	sp.add_argument("--refs", choices=PROJECT_REFS_CHOICES, default="aar",
	                help="reference pass on a loaded project before indexing (default aar: "
	                     "a seeded project has no references until it runs)")
	sp.set_defaults(handler=cmd_index, need_rizin=True)

	sp = sub.add_parser("strings", help="strings matching a pattern, and the functions that use them")
	sp.add_argument("pattern", help="case-insensitive substring (a Python regex with --regex)")
	sp.add_argument("--regex", action="store_true", help="treat PATTERN as a Python regex")
	sp.add_argument("--limit", type=int, default=40)
	sp.set_defaults(handler=cmd_strings, need_rizin=True)

	sp = sub.add_parser("find", help="find functions by name/signature substring")
	sp.add_argument("pattern")
	sp.add_argument("--limit", type=int, default=40)
	sp.set_defaults(handler=lambda a, c, _rz: revtool.cmd_find(a, c), need_rizin=False)

	sp = sub.add_parser("view", help="render one function (rizin disassembly + overlay)")
	sp.add_argument("target", help="sub_xxxx | applied name | address")
	sp.add_argument("--raw", action="store_true", help="pristine rizin text, no overlay")
	sp.add_argument("--no-apply", action="store_true", help="keep original tokens, only annotate")
	sp.add_argument("--no-legend", action="store_true", help="hide the rename legend")
	sp.add_argument("--lines", help="restrict to function-local line range A:B")
	sp.add_argument("--force", action="store_true",
	                help="bypass the strict label-debt gate once (audited in history)")
	sp.set_defaults(handler=cmd_view, need_rizin=True)

	sp = sub.add_parser("xref", help="analysis references to a symbol (axtj)")
	sp.add_argument("symbol")
	sp.add_argument("--limit", type=int, default=200)
	sp.add_argument("--force", action="store_true",
	                help="bypass the strict label-debt gate once (audited in history)")
	sp.set_defaults(handler=cmd_xref, need_rizin=True)

	sp = sub.add_parser("audit", help="scan a file for sub_/data_ symbols that have no label")
	sp.add_argument("path", help="file to scan (doc, source, RE writeup, ...)")
	sp.add_argument("--quiet", action="store_true", help="print only the unlabeled symbols")
	sp.set_defaults(handler=lambda a, c, _rz: revtool.cmd_audit(a, c), need_rizin=False)

	sp = sub.add_parser("coverage", help="how thoroughly one function is RE'd (rizin refs)")
	sp.add_argument("target", help="function name / applied name / address")
	sp.set_defaults(handler=cmd_coverage, need_rizin=True)

	sp = sub.add_parser("rename", help="rename a symbol (global or local scope)")
	rsub = sp.add_subparsers(dest="scope", required=True)
	rg = rsub.add_parser("global", help="rename sub_/data_ everywhere")
	rg.add_argument("a", metavar="orig")
	rg.add_argument("b", metavar="new")
	rg.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(revtool.TIERS)}")
	rg.add_argument("--evidence", default=None, help="why you believe this (RVA, rizin, etc.)")
	rg.add_argument("--basis", default=None, help=f"evidence basis: {'|'.join(revtool.BASES)}")
	rg.add_argument("--method", default=None, help="how derived (e.g. 'rizin /x absolute-addr')")
	rg.add_argument("--blindspots", default=None, help="what `method` cannot see")
	rg.add_argument("--shallow", action="store_true",
	                help="ack a confident claim has no locals/fields worth naming (bypasses depth gate, audited)")
	rl = rsub.add_parser("local", help="rename a local within ONE function")
	rl.add_argument("func")
	rl.add_argument("a", metavar="orig")
	rl.add_argument("b", metavar="new")
	rl.add_argument("--tier", default="guess", help=f"confidence: {'|'.join(revtool.TIERS)}")
	rl.add_argument("--evidence", default=None)
	sp.set_defaults(handler=cmd_rename, need_rizin=False)

	sp = sub.add_parser("doctor",
	                    help="fail-closed provenance, DB, and ledger audit (rizin checks)")
	sp.add_argument("--ledger", default=None,
	                help="claim/event ledger path (default: selected DB's ledger)")
	sp.add_argument("--json", action="store_true", help="emit machine-readable result")
	# The backend is explicit here, so the rizin audit runs even on a DB
	# no `index` has marked yet: the dump doctor would read BN-only
	# arguments this parser never defines (args.lock) and crash.
	sp.set_defaults(handler=lambda a, c, _rz: revtool.cmd_doctor_rizin(a, c), need_rizin=False)

	sp = sub.add_parser("sync", help="git-mergeable JSONL export/import for collaboration")
	sp.add_argument("action", choices=["export", "import", "status", "conflicts", "resolve"])
	sp.add_argument("path", nargs="?",
	                help="default: <selected --db stem>_symbols.jsonl beside the database")
	sp.add_argument("--take", choices=["local", "remote"], default=None,
	                help="with resolve: explicit side to preserve")
	sp.set_defaults(handler=lambda a, c, _rz: revtool.cmd_sync(a, c), need_rizin=False)

	args = parser.parse_args(argv)
	con = revtool.open_db(args.db)
	try:
		rz = open_session(args, con, args.need_rizin)
		try:
			return args.handler(args, con, rz)
		finally:
			if rz is not None:
				rz.close()
	finally:
		# The dump CLI closes its DB the same way; a held handle keeps
		# Windows from removing the file (or its folder) afterwards.
		con.close()


if __name__ == "__main__":
	raise SystemExit(main())
