"""
===========================================================================
revtool_cli.py - the argument parser (kept apart so tests can render help)

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
import argparse

from revtool_asserts import cmd_assert, cmd_asserts, cmd_observe
from revtool_break import cmd_break, cmd_runtime
from revtool_claims import cmd_audit, cmd_bind, cmd_config, cmd_coverage
from revtool_claims import cmd_dispute, cmd_field, cmd_history, cmd_mark
from revtool_claims import cmd_note, cmd_rename, cmd_resolve, cmd_review
from revtool_claims import cmd_seen, cmd_type
from revtool_d3d import cmd_d3dstate
from revtool_evidence import cmd_classes, cmd_doctor, cmd_dossier, cmd_status
from revtool_index import cmd_index
from revtool_ingest import cmd_export, cmd_infer_types, cmd_ingest
from revtool_ingest import cmd_ingest_allocs, cmd_ingest_calls
from revtool_ingest import cmd_ingest_layout, cmd_reconstruct, cmd_sync
from revtool_overlay import cmd_find, cmd_view, cmd_xref
from revtool_paths import DEFAULT_DB, DEFAULT_DUMP, DEFAULT_EVIDENCE_LOCK
from revtool_pe import cmd_deref, cmd_vtable
from revtool_seed import cmd_seed_apply, cmd_seed_build, cmd_seed_publish, cmd_seed_verify
from revtool_store import open_db
from revtool_syntax import ASSERT_STATUS, BASES, TIERS

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
	p.add_argument("--backend", default="dump", choices=("dump", "rizin"),
				   help="where index/view/xref/coverage/find/audit get their facts: "
						"the Binary Ninja dump (default) or rizin on the retail "
						"binary (dispatched by revtool.py; see revtool_rizin.py); "
						"rename/doctor/sync act on the overlay store either way")
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
	sp.add_argument("count", nargs="?", type=int, default=None,
					help="dwords to read (positional form of --count)")
	sp.add_argument("--count", dest="count_flag", type=int, default=None,
					help="dwords to read (default 8)")
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

	sp = sub.add_parser("seed",
						help="build / verify / apply the contributor seed (#2830)")
	ssub = sp.add_subparsers(dest="seed_cmd", required=True)
	sb = ssub.add_parser("build", help="emit the seed for --db's target")
	sb.add_argument("--target", required=True, choices=("client", "server"))
	sb.add_argument("--ledger", default=None,
					help="JSONL evidence ledger (default: <db stem>_symbols.jsonl beside --db)")
	sb.add_argument("--binary", dest="seed_binary", default=None,
					help="retail binary to hash + scan (default: the top-level --binary)")
	sb.add_argument("--out", default=None,
					help="seed file (default: <data dir>/seed/revtool-seed-<target>.xz)")
	sb.add_argument("--names", default=None,
					help="Binary Ninja name export (sro-bn-function-names JSON) to merge; "
						 "without it the seed misses the BN labels")
	sb.set_defaults(handler=cmd_seed_build)
	sv = ssub.add_parser("verify", help="check a seed: format, pin, hash, text-run scan")
	sv.add_argument("seed", help="seed file")
	sv.add_argument("--binary", dest="seed_binary", default=None,
					help="retail binary to hash + scan (default: the top-level --binary)")
	sv.add_argument("--manifest", default=None,
					help="pin manifest (default: seed-manifest.json beside revtool; 'none' for an unreleased build)")
	sv.set_defaults(handler=cmd_seed_verify)
	sa = ssub.add_parser("apply", help="define + name the seed's functions in rizin, save an .rzdb")
	sa.add_argument("seed", help="seed file")
	sa.add_argument("--binary", dest="seed_binary", default=None,
					help="retail binary (default: the top-level --binary)")
	sa.add_argument("--rzdb", default=None,
					help="project to save (default: <seed stem>.rzdb beside the seed)")
	sa.add_argument("--limit", type=int, default=0,
					help="apply only the first N functions (dogfood runs)")
	sa.add_argument("--force", action="store_true",
					help="replace an existing .rzdb")
	sa.set_defaults(handler=cmd_seed_apply)
	spb = ssub.add_parser("publish",
						help="publish the manifest-pinned seeds as a GitHub release (#2830)")
	spb.add_argument("--seed-dir", default=None,
					help="directory holding the built seeds (default: <data dir>/seed)")
	spb.add_argument("--manifest", default=None,
					help="pin manifest (default: seed-manifest.json beside revtool)")
	spb.add_argument("--tag", default=None,
					help="release tag (default: revtool-seed-<UTC date>)")
	spb.add_argument("--repo", default="opensro-dev/opensro",
					help="repository that holds the release")
	spb.add_argument("--dry-run", action="store_true",
					help="verify the pins and print the gh commands, run none")
	spb.set_defaults(handler=cmd_seed_publish)

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
