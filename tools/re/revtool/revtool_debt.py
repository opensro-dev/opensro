"""
===========================================================================
revtool_debt.py - label debt: what counts as payable, the strict gate and its footer

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
import re

import revtool_dump
from revtool_dump import dump_path_from_meta, read_func_lines, resolve_func
from revtool_dump import symbol_is_annotated
from revtool_store import author_is_explicit, get_author, now_iso
from revtool_store import record_history
from revtool_syntax import SCALAR_LOCAL_DECL_RE, field_accesses

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
		if not revtool_dump.symbol_is_identifiable(con, sym, dump):
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


def depth_gate(con, args, dump, orig_name, tier, lines=None):
	"""A confident (mechanism+) function claim must capture some of what was understood.

	The failure this fixes: a bare function name discharges all debt, so the rich,
	compounding knowledge (the `this`/main object, struct fields) is never written down
	and a `mechanism` name even discourages the next engineer from digging. So a
	confident claim on a STRUCTURED function (>= STRUCTURE_MIN_OFFSETS field accesses)
	must leave >=1 breadcrumb: a local rename, a binding, a named field, or `--shallow`.
	`guess`/`unknown` stay frictionless. Returns exit code 2 to abort, else None.

	`lines` is the function text to measure when the caller already has it
	(the rizin backend passes the function's disassembly lines; its index
	rows carry no dump geometry). With neither lines nor a dump on disk
	there is no text to measure: unknown is not "structured", so the gate
	passes rather than refusing on a guess - the same rule
	`rename_key_can_fire` applies to an unavailable dump.
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
	if lines is None:
		if not dump or not os.path.exists(dump):
			return None  # no function text to measure (rizin backend)
		lines = read_func_lines(dump, row[3], row[4])
	offs = function_field_offsets(lines)
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
