"""
===========================================================================
revtool_paths.py - one resolver for every path revtool reads or writes

Part of revtool, the non-destructive symbol overlay for the SRO_Client
pseudocode dump. revtool.py is the entry point; this module owns path
resolution and is imported by it (and by revtool_rizin.py through
revtool's re-exports). The dump is an immutable Binary Ninja
hypothesis snapshot: original PE bytes and exact proofs outrank it,
and nothing here edits it.

Why this module exists: the research copy derived its paths from its
own location (research/tools -> reconstruct -> rebuild -> workspace),
which silently pointed the repo copy at deleted temp directories and
a cpp-replacement-proof folder that is not in the repo. Every path
now resolves from the repo layout plus explicit environment
overrides; nothing names a drive letter.

Layout:
  repo root          three parents up from this file (tools/re/revtool)
  per-machine data   <repo>/.state/revtool (REVTOOL_DATA_DIR overrides)
                     the overlay DB, JSONL ledger, evidence lock and
                     the derived-evidence tree live here; .state is
                     ignored scratch, rebuilt by the producers
  the retail binary  REVTOOL_BINARY, else SRO_Client.exe beside the
                     repo (the workspace layout this tool grew up in),
                     else the bare name so the error says what is missing
  the HLIL dump      REVTOOL_DUMP, else the data dir, else the repo
                     root; it is a ~110 MB Binary Ninja export that is
                     never committed (see require_dump in revtool_dump)
===========================================================================
"""

import os

HERE = os.path.dirname(os.path.abspath(__file__))
TOOL_DIR = HERE
REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(HERE)))

# Historical names kept so the ported command bodies read as written.
REBUILD_ROOT = REPO_ROOT
WORKSPACE_ROOT = os.path.dirname(REPO_ROOT)

DATA_DIR = os.environ.get("REVTOOL_DATA_DIR") or os.path.join(
    REPO_ROOT, ".state", "revtool")
RECONSTRUCT_DIR = DATA_DIR
EVIDENCE_DIR = os.path.join(DATA_DIR, "evidence")
EVIDENCE_CURRENT = os.path.join(EVIDENCE_DIR, "current")
EVIDENCE_PROGRESS = os.path.join(EVIDENCE_DIR, "progress")

# The proof helpers (msvc_rtti.py, pe_image.py, evidence_dossier.py)
# ship beside this module now; the old cpp-replacement-proof path is
# kept as a name for status output only.
PROOF_TOOLS = HERE

DUMP_CANDIDATES = (
    os.path.join(DATA_DIR, "SRO_Client_psuedo.txt"),
    os.path.join(REPO_ROOT, "SRO_Client_psuedo.txt"),
)


def resolve_default_dump():
	candidates = DUMP_CANDIDATES
	env = os.environ.get("REVTOOL_DUMP")
	if env:
		candidates = (env,) + candidates
	for candidate in candidates:
		if os.path.exists(candidate):
			return candidate
	return candidates[0]



DEFAULT_DUMP = resolve_default_dump()
DEFAULT_DB = os.path.join(DATA_DIR, "revtool.db")
DEFAULT_LEDGER = os.path.join(DATA_DIR, "revtool_symbols.jsonl")
DEFAULT_EVIDENCE_LOCK = os.path.join(DATA_DIR, "evidence-lock.json")


def resolve_default_binary():
	env = os.environ.get("REVTOOL_BINARY")
	if env:
		return env
	beside_repo = os.path.join(WORKSPACE_ROOT, "SRO_Client.exe")
	if os.path.exists(beside_repo):
		return beside_repo
	return "SRO_Client.exe"



DEFAULT_BINARY = resolve_default_binary()

REVTOOL_SCHEMA_VERSION = 2
LEDGER_SCHEMA = "sro-revtool-evidence-ledger-v2"
EVIDENCE_LOCK_SCHEMA = "sro-re-evidence-lock-v1"
TARGET_SHA256 = "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a"
REVTOOL_VERSION = "2.0"
