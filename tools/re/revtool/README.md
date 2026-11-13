# revtool

revtool is the overlay/evidence CLI for native work: it keeps a SQLite
overlay of names, types, fields, bindings and notes beside the
immutable Binary Ninja HLIL dump (or, on a machine without the dump,
beside rizin's reading of the retail binary), and it gates claims with
the label-debt and evidence rules the port follows. It shipped here
from the research tree; the research copy stays for its own tests.

## Quick start: a contributor with no Binary Ninja

Everything below was run end to end on Windows 11 with Python 3.13 on
2026-11-13 (seed apply about 70 s, index about 2 min 20 s). You need
your own retail `SRO_Client.exe` (v1.150, SHA-256
`375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a`)
or the v1.188 `SR_GameServer.exe`; the seed is useless without it.

1. Rizin 0.8.2 on PATH: [docs/native/RIZIN.md](../../../docs/native/RIZIN.md)
   section 1. On Windows the zip unpacks into
   `rizin-win-installer-clang_cl-64\`; its `bin` is the folder to add.
2. The venv, from the repository root:

   ```
   py -3 -m venv .state/re-venv
   .state/re-venv/Scripts/python.exe -m pip install --only-binary=:all: pefile==2024.8.26 capstone==5.0.9 rzpipe==0.6.2
   ```

3. The seed: the latest `revtool-seed-*` release on
   [the releases page](https://github.com/opensro-dev/opensro/releases)
   (`gh release download <tag> -R opensro-dev/opensro -p revtool-seed-client.xz`).
   `seed-manifest.json` pins its hash; a different download is refused.
4. Verify, apply, index (global options always go before the command):

   ```
   set RT=.state/re-venv/Scripts/python.exe tools/re/revtool/revtool.py
   %RT% seed verify revtool-seed-client.xz --binary SRO_Client.exe
   %RT% --binary SRO_Client.exe seed apply revtool-seed-client.xz --rzdb SRO_Client.rzdb
   %RT% --backend rizin --binary SRO_Client.exe --rzdb SRO_Client.rzdb index
   ```

5. Work: every command takes the same three global options.

   ```
   %RT% --backend rizin --binary SRO_Client.exe --rzdb SRO_Client.rzdb doctor
   %RT% --backend rizin --binary SRO_Client.exe --rzdb SRO_Client.rzdb find CIFMainFrame
   %RT% --backend rizin --binary SRO_Client.exe --rzdb SRO_Client.rzdb strings GUILD_RECALL
   %RT% --backend rizin --binary SRO_Client.exe --rzdb SRO_Client.rzdb view <name or sub_xxxxxx>
   %RT% --backend rizin --binary SRO_Client.exe --rzdb SRO_Client.rzdb xref <name or sub_xxxxxx>
   ```

Save what you learn the way [RESEARCH_PROGRESS.md](../../../docs/native/RESEARCH_PROGRESS.md)
says, and cite the binary hash and the address in your pull request's
native-claims table.

## Layout

| file | owns |
| --- | --- |
| `revtool.py` | entry point, public surface (re-exports), `--backend` dispatch |
| `revtool_cli.py` | the argument parser and `main` for the dump backend |
| `revtool_paths.py` | the one path resolver (repo layout + `REVTOOL_*` overrides) |
| `revtool_syntax.py` | tiers, bases, dump grammar, header parsing |
| `revtool_store.py` | overlay schema, SQLite store, history, artifacts |
| `revtool_dump.py` | whole-dump scans, symbol resolution, artifact paths |
| `revtool_debt.py` | label debt, the strict gate, structure depth |
| `revtool_index.py` | `index` |
| `revtool_overlay.py` | `view`, `find`, `xref`, rename application, note banners |
| `revtool_claims.py` | `rename`, `type`, `field`, `bind`, `note`, review queue |
| `revtool_asserts.py` | `assert`, `observe`, `asserts` |
| `revtool_ingest.py` | `ingest`, `sync`, `export`, type inference, `reconstruct` |
| `revtool_pe.py` | PE reading, `vtable`, `deref`, the RTTI indexes, `classes` helpers |
| `revtool_d3d.py` | `d3dstate` |
| `revtool_runtime.py` | the live-process reader/debugger |
| `revtool_break.py` | `runtime`, `break` |
| `revtool_evidence.py` | evidence artifacts, `doctor`, `dossier`, `status`, `classes` |
| `evidence_dossier.py` | the hash-bound per-function dossier (run by `dossier`) |
| `pe_image.py`, `msvc_rtti.py` | the strict PE reader and the MSVC RTTI reader |
| `revtool_rizin.py` | the rizin backend (below) |

## Setup

Python 3.12 or newer, no third-party packages for the dump backend.
The rizin backend and the live commands use the venv from the quick
start above (`.state/re-venv`: pefile, capstone, rzpipe 0.6.2).

Per-machine state (overlay DB, JSONL ledger, evidence lock, derived
evidence tree) lives in `.state/revtool/` inside the repo and is never
committed. Every path resolves through `revtool_paths`:

| override | default |
| --- | --- |
| `REVTOOL_DATA_DIR` | `<repo>/.state/revtool` |
| `REVTOOL_DB` / `--db` | `<data>/revtool.db` |
| `REVTOOL_DUMP` / `--dump` | `<data>/SRO_Client_psuedo.txt` |
| `REVTOOL_BINARY` / `--binary` | the workspace's `SRO_Client.exe` |
| `REVTOOL_CORPUS_ROOT` | `<data>` (dossier's authored-tree cross-read) |

## Use

```
py -3 tools/re/revtool/revtool.py --db <overlay.db> status
py -3 tools/re/revtool/revtool.py --db <overlay.db> find fog
py -3 tools/re/revtool/revtool.py --db <overlay.db> view sub_8b3180
py -3 tools/re/revtool/revtool.py --db <overlay.db> xref data_f0673f
py -3 tools/re/revtool/revtool.py --db <overlay.db> deref 0xce8a38 5
```

Commands that need an artifact which is absent fail by naming it (the
path or hash actually seen), never by answering as if it were there:
`xref` without the dump stops instead of reporting "0 callers", and
`dossier`'s preflight lists each failed check with its artifact.

## The rizin backend

`revtool_rizin.py` lets the six dump-reading commands run on a machine
that has rizin and the retail binary but not Binary Ninja or its
110 MB HLIL dump export. revtool.py's `--backend rizin` switch hands
those commands to it with the same argument shapes; it can also be run
directly:

```
py .state/re-venv/Scripts/python.exe tools/re/revtool/revtool.py \
    --backend rizin --db revtool.db --binary SRO_Client.exe index
py tools/re/revtool/revtool.py --backend rizin --db revtool.db view sub_402a70
py tools/re/revtool/revtool.py --backend rizin --db revtool.db xref data_f0c90c
py tools/re/revtool/revtool.py --backend rizin --db revtool.db xref CIFGuildSummonReq_OnCreate
py tools/re/revtool/revtool.py --backend rizin --db revtool.db strings GUILD_RECALL
py tools/re/revtool/revtool.py --backend rizin --db revtool.db coverage sub_402a70
py tools/re/revtool/revtool.py --backend rizin --db revtool.db find CIFMain
py tools/re/revtool/revtool.py --backend rizin --db revtool.db audit some_writeup.md
py tools/re/revtool/revtool.py --backend rizin --db revtool.db rename global sub_402a70 WidgetDraw
py tools/re/revtool/revtool.py --backend rizin --db revtool.db doctor
```

The first `index` runs rizin's call analysis (`aac`, ~1 minute on
SRO_Client.exe) and saves a `.rzdb` beside the DB; later commands
reload it instead of re-analyzing. `--analyze` chooses `aac` (default),
`aap`, `aaa` or `none`.

On a loaded project (a seed-applied `.rzdb`) `index` first runs the
reference pass `--refs aar` (default; about 7 s on the seeded client),
because a seeded project defines functions but holds no references
until rizin reads the code once; without it every `xref` and
`strings` answers 0. `--refs none` skips it, and `xref` then warns
before you trust a 0.

`strings PATTERN` (case-insensitive; `--regex` for a regex) lists the
binary's data strings that match and every function referencing each,
tagged `[labeled]` or `[NEEDS RE]`: the usual first step from a
feature's UI or log text to its code.

### Where each command gets its facts

| command  | source                     | notes |
| -------- | -------------------------- | ----- |
| index    | `aflj` + `afsj`            | names normalized `fcn.00402a70` -> `sub_402a70`; signatures are rizin's |
| find     | overlay DB (delegated)     | identical to the dump backend |
| view     | `pdfj`                     | disassembly at true addresses, overlay applied by revtool; rizin 0.8.2 has no `pdc`/`pdg` |
| xref     | `axtj`                     | analysis-derived; un-analyzed code is invisible (printed on every run); takes a `sub_`/`data_` token or an indexed function name |
| strings  | `izj` + `axtj`             | refs checked at every character boundary up to the match (izj can start a string early) |
| coverage | `aflj` call/data refs      | exact refs; field depth from disassembly (see below) |
| audit    | overlay DB (delegated)     | identical to the dump backend |
| rename   | overlay DB (delegated)     | the strict depth gate measures the function's disassembly (below) |
| doctor   | overlay DB + artifact rows | rizin provenance audit; the BN evidence checks do not apply |
| sync     | overlay DB (delegated)     | identical to the dump backend |

### Semantics that differ from the dump backend

These are properties of the data source, not bugs to paper over:

* rizin finds what its analysis reaches: ~23k functions (`aac`) out
  of the ~95k the BN dump indexes. `index` prints the count.
* `view` shows assembly, not pseudo-C, so the HLIL field-access
  shapes (`ident + 0xNN`) mostly do not occur and field annotations
  rarely fire. Coverage says "not measurable from disassembly"
  rather than inventing offsets.
* xref answers cover analyzed code only; 0 references under this
  backend is never evidence of absence (the warning that
  `require_dump` exists to prevent applies here too).
* Argument names are rizin's (`arg_8h`), so overlay locals/bindings
  recorded against BN variable names do not attach until names
  coincide.
* `view --lines A:B` is a function-local 1-based range under this
  backend (the dump backend's are whole-dump line numbers), since
  rizin text is fetched per function. The flag itself is unchanged.
* `start_byte`/`start_line` in the functions table are dump
  geometry; rizin rows store a stable ordinal. Use one backend's
  `index` per DB file (meta records `backend=rizin`).
* `rename`'s can-fire test consults the function index, so it stays
  silent for `data_` keys (absent from a function index by
  construction), and its depth gate measures disassembly - which
  rarely carries the `ident + 0xNN` shape, so a confident rename
  normally passes unmeasured rather than refusing on a guess.
  `doctor` audits this backend's chain (binary/index binding, the
  saved project, the ledger) and passes a fresh seed setup that has
  no claims yet.

## The contributor seed

Maintainers analyse in Binary Ninja databases that embed the retail
executable and can never leave their machine. The seed is what ships
instead (#2830): one xz file per target with only conclusions -
every function's address and current (overlay-applied) name, plus
the target's JSONL evidence ledger. No executable bytes, no
decompiler text: the payload is JSON, and `seed verify` proves no
16-byte run of the binary's `.text` appears in it. Both seeds are
~0.3-0.4 MB against a 5 MB cap. They ship as assets of a dated
`revtool-seed-<date>` release (a refresh is a new tag, never an
overwritten asset), and `seed-manifest.json` beside the tool pins the
current pair.

```
rem maintainer: build (hashes must agree across DB, ledger, binary)
py revtool.py --db <overlay.db> --binary SRO_Client.exe seed build --target client --names bn-names-client.json
py revtool.py seed verify revtool-seed-client.xz --binary SRO_Client.exe

rem contributor: rizin + the pinned venv, next to their retail binary
py revtool.py --binary SRO_Client.exe seed apply revtool-seed-client.xz --rzdb SRO_Client.rzdb
```

`apply` hand-defines every seed function (`af+` name, one `afb+`
block to the next start), samples the result against the seed, and
saves the project via a temp file + `os.replace` under an exclusive
lock, so two applies at one `.rzdb` refuse rather than race: client
94,865 of 95,290 rows defined in 66 s on the maintainer's box with
the locked save (the rest are import thunks rizin already owns),
sampled labelled names 20/20. Functions rizin defines on load (entry0
and the like) are renamed in place rather than re-added. Then run
`revtool.py --backend rizin ... index` for the reference pass; apply
prints the exact line.

`--names` merges the Binary Ninja labels, which are the bulk of the
RE: without it the seed carries only the dump index's names and
revtool claims (1,373 of the client's 95,290 functions named, against
13,814 in the BN database, 2026-11-09). The first published pair
(`revtool-seed-2026-11-09`) was built that way and carried 996 real
client names; the 2026-11-13 pair carries 14,070 client and 12,027
server names. `seed build` prints the `names:` split, so check it
before publishing. Precedence: a revtool claim,
then the BN name, then the dump's; an anonymous `sub_` never replaces
a real name, and BN functions the dump lacks are added. The export
is one JSON file per target, written from the Binary Ninja executor
(MCP) against the open database:

```
import json
# `bv` follows the active tab, which may be another database: check it.
assert bv.file.filename.replace("\\", "/").endswith("SRO_Client.exe.trimmed.bndb"), bv.file.filename
rows = [[f.start, f.name] for f in bv.functions]
json.dump({"format": "sro-bn-function-names", "version": 1,
           "target": "client", "source": bv.file.filename.split("/")[-1],
           "functions": rows}, open("bn-names-client.json", "w"))
```

## Tests

```
py -3 -m unittest tools.re.revtool.test_revtool_cli_contract
py -3 -m unittest tools.re.revtool.test_revtool_index_and_unpayable
py -3 -m unittest tools.re.revtool.test_revtool_port
py -3 -m unittest tools.re.revtool.test_revtool_rizin
py -3 -m unittest tools.re.revtool.test_revtool_seed
```

`test_revtool_rizin.py` runs against a fake rzpipe session — no rizin,
no binary. A live end-to-end (real rizin + retail client) is opt-in
via `SRO_RIZIN_IT=1` and `SRO_RIZIN_BINARY`. The suites open a fresh
overlay DB per case, so they take minutes on this machine (54 schema
ALTERs per fresh DB); that cost predates the port.
