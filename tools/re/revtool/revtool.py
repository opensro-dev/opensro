"""
===========================================================================
revtool.py - the revtool entry point: a non-destructive symbol overlay
for the SRO_Client pseudocode dump

The pseudocode file (SRO_Client_psuedo.txt) is treated as an IMMUTABLE
Binary Ninja hypothesis snapshot. Original PE bytes and exact proofs
outrank it. This tool never edits it. Instead it keeps a separate
SQLite "overlay" of names, types and notes, and composites them on top
of the raw dump when you `view` a function. The pristine code is always
one `--raw` away.

Three naming SCOPES are modelled (this is the whole point - you cannot
just "replace all", because the same token means different things in
different places):

  global   sub_8b3180 / data_f0673f          -> renamed everywhere in the dump
  local    var_d0 / eax_3 / arg3 (per func)  -> renamed ONLY inside one function
  field    +0x67e6 on a typed pointer        -> a field of a struct TYPE; resolved
                                                wherever a var is bound to that type

This file is only the entry point and the public surface. The work
lives in the revtool_* modules beside it, split by command group;
revtool_rizin.py (the rizin backend) imports this module and uses the
names re-exported below, unmodified. Paths resolve through
revtool_paths (repo layout + REVTOOL_* overrides), never a hard-coded
drive.

Quick start (from the repo root, pinned venv per
docs/native/REVERSE_ENGINEERING.md):

  py -3 tools/re/revtool/revtool.py --db <overlay.db> index
  py -3 tools/re/revtool/revtool.py --db <overlay.db> find fog
  py -3 tools/re/revtool/revtool.py --db <overlay.db> view sub_8b3180
  py -3 tools/re/revtool/revtool.py --db <overlay.db> view sub_8b3180 --raw
  py -3 tools/re/revtool/revtool.py --db <overlay.db> xref data_f0673f
  py -3 tools/re/revtool/revtool.py --db <overlay.db> rename global sub_8b3180 WorldRenderPass
  py -3 tools/re/revtool/revtool.py --db <overlay.db> type new MapRenderer
  py -3 tools/re/revtool/revtool.py --db <overlay.db> field MapRenderer 0x67e6 fogColor --type uint32
  py -3 tools/re/revtool/revtool.py --db <overlay.db> bind sub_8b1050 arg3 MapRenderer
  py -3 tools/re/revtool/revtool.py --db <overlay.db> note add sub_8b3180@008b37ac "FOGEND = data_f06743 (else branch)"
  py -3 tools/re/revtool/revtool.py --db <overlay.db> export sub_8b3180

On a machine without Binary Ninja or the dump, the same six
dump-reading commands run on the rizin backend against the retail
binary itself:

  py -3 tools/re/revtool/revtool.py --backend rizin --binary SRO_Client.exe index

===========================================================================
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from revtool_paths import DATA_DIR, DEFAULT_BINARY, DEFAULT_DB, DEFAULT_DUMP
from revtool_paths import DEFAULT_EVIDENCE_LOCK, DEFAULT_LEDGER, EVIDENCE_CURRENT
from revtool_paths import EVIDENCE_PROGRESS, HERE, REPO_ROOT, TARGET_SHA256
from revtool_paths import resolve_default_binary, resolve_default_dump

from revtool_syntax import *  # noqa: F401,F403
from revtool_store import *  # noqa: F401,F403
from revtool_dump import *  # noqa: F401,F403
from revtool_debt import *  # noqa: F401,F403
from revtool_index import *  # noqa: F401,F403
from revtool_overlay import *  # noqa: F401,F403
from revtool_claims import *  # noqa: F401,F403
from revtool_asserts import *  # noqa: F401,F403
from revtool_ingest import *  # noqa: F401,F403
from revtool_pe import *  # noqa: F401,F403
from revtool_d3d import *  # noqa: F401,F403
from revtool_runtime import *  # noqa: F401,F403
from revtool_break import *  # noqa: F401,F403
from revtool_evidence import *  # noqa: F401,F403

from revtool_cli import build_parser
import revtool_cli

# The dump-hit memo is shared state the test-suite clears between
# cases; `import *` does not carry underscore names, so it is bound
# explicitly (the same dict object revtool_dump reads and writes).
from revtool_dump import _DUMP_HIT_MEMO  # noqa: F401


"""
================
main

Dispatch: --backend rizin hands the six dump-reading commands to
revtool_rizin (same argument shapes, facts from the retail binary);
everything else is the dump backend in revtool_cli. The flag is
stripped before either parser sees the arguments.
================
"""
def main(argv=None):
	if argv is None:
		argv = sys.argv[1:]
	argv = list(argv)
	backend = "dump"
	if "--backend" in argv:
		at = argv.index("--backend")
		if at + 1 >= len(argv):
			raise SystemExit("error: --backend needs a value (dump or rizin)")
		backend = argv[at + 1]
		del argv[at:at + 2]
	elif any(a.startswith("--backend=") for a in argv):
		at = next(i for i, a in enumerate(argv) if a.startswith("--backend="))
		backend = argv[at].split("=", 1)[1]
		del argv[at]
	if backend == "rizin":
		import revtool_rizin
		return revtool_rizin.main(argv)
	if backend != "dump":
		raise SystemExit(f"error: unknown --backend {backend!r} (dump or rizin)")
	return revtool_cli.main(argv)


if __name__ == "__main__":
	sys.exit(main())
