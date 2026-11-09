"""
===========================================================================
revtool_dump.py - whole-dump scans and symbol resolution against the pseudocode dump

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
import mmap
import re

from revtool_paths import DEFAULT_BINARY, DEFAULT_DUMP, EVIDENCE_CURRENT
from revtool_paths import EVIDENCE_PROGRESS, RECONSTRUCT_DIR
from revtool_store import meta_get
from revtool_syntax import ANONYMOUS_FUNCTION_RE, GLOBAL_TOKEN_RE

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
			f"  {DEFAULT_DUMP} (or set REVTOOL_DUMP). Then rerun "
			f"`revtool.py index`, or run `--backend rizin`, which needs no dump.\n"
			f"  Meanwhile use raw Rizin `/x` byte-pattern search for cross-references "
			f"(NOT `/c` - that is crypto search on this build).")
	if os.path.getsize(dump) == 0:
		raise SystemExit(
			f"error: pseudocode dump is EMPTY (0 bytes): {dump}\n"
			f"  An empty dump makes `xref` find NOTHING for every symbol - that is a "
			f"broken export, not 'no callers'.\n"
			f"  Regenerate it (whole-program decompilation of SRO_Client.exe -> "
			f"{DEFAULT_DUMP}; or set REVTOOL_DUMP),\n"
			f"  then rerun `revtool.py index`.")


# --------------------------------------------------------------------------- #
# binary-backed reads (vtable / data resolution the text dump cannot do)
#
# The pseudocode dump has NO data arrays: a vtable's slots, an .rdata descriptor,
# a stamped vtable address - none of that is in the text. Resolving a virtual call
# `(*(*obj + 0xNN))()` to a concrete function therefore needs the PE image. This is
# a tiny, dependency-free PE32 reader (DOS->PE->optional->section map) so revtool
# can do `vtable`/`deref` itself instead of dropping the engineer into raw rizin.
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


def get_binary(con, args):
	"""Resolve the PE path: --binary flag > meta 'binary' > default SRO_Client.exe."""
	cand = getattr(args, "binary", None)
	if cand:
		return cand
	row = con.execute("SELECT value FROM meta WHERE key='binary'").fetchone()
	if row and row[0] and os.path.exists(row[0]):
		return row[0]
	return DEFAULT_BINARY


def evidence_artifact_paths(con, args):
	# The derived tree lives under the data dir (revtool_paths); the old
	# <workspace>/temp/sro-native-exact-all-cpp-v1150-current root was
	# deleted, which is what left dossier/doctor with nothing to read.
	current = EVIDENCE_CURRENT
	progress = EVIDENCE_PROGRESS
	return {
		"target": get_binary(con, args),
		"binary_ninja_hlil": get_dump(con, args),
		"binary_ninja_functions": os.path.join(current, "binary-ninja-function-spans.csv"),
		"binary_ninja_calls": os.path.join(current, "binary-ninja-direct-calls.csv"),
		"binary_ninja_coverage": os.path.join(current, "binary-ninja-text-coverage.json"),
		"rizin_function_index": os.path.join(current, "function-index.json"),
		"rizin_project": os.path.join(current, "sro.rzdb"),
		"reconciled_function_index": os.path.join(current, "function-index-reconciled.json"),
		"progress_data": os.path.join(progress, "progress.json"),
		"ui_family_plan": os.path.join(progress, "plan.json"),
		"exact_provider_manifest": os.path.join(
			RECONSTRUCT_DIR, "native", "replacements", "results", "all-exact",
			"provider-manifest.json"),
	}
