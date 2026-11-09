# revtool

revtool is the overlay/evidence CLI for native work: it keeps a SQLite
overlay of names, types, fields, bindings and notes beside the
immutable Binary Ninja HLIL dump (or, on a machine without the dump,
beside rizin's reading of the retail binary), and it gates claims with
the label-debt and evidence rules the port follows. It shipped here
from the research tree; the research copy stays for its own tests.

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

Python 3.12, no third-party packages for the dump backend. The rizin
backend and the live commands use the pinned venv from
`docs/native/REVERSE_ENGINEERING.md` (`.state/re-venv`, rzpipe).

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
py tools/re/revtool/revtool.py --backend rizin --db revtool.db coverage sub_402a70
py tools/re/revtool/revtool.py --backend rizin --db revtool.db find CIFMain
py tools/re/revtool/revtool.py --backend rizin --db revtool.db audit some_writeup.md
```

The first `index` runs rizin's call analysis (`aac`, ~1 minute on
SRO_Client.exe) and saves a `.rzdb` beside the DB; later commands
reload it instead of re-analyzing. `--analyze` chooses `aac` (default),
`aap`, `aaa` or `none`.

### Where each command gets its facts

| command  | source                     | notes |
| -------- | -------------------------- | ----- |
| index    | `aflj` + `afsj`            | names normalized `fcn.00402a70` -> `sub_402a70`; signatures are rizin's |
| find     | overlay DB (delegated)     | identical to the dump backend |
| view     | `pdfj`                     | disassembly at true addresses, overlay applied by revtool; rizin 0.8.2 has no `pdc`/`pdg` |
| xref     | `axtj`                     | analysis-derived; un-analyzed code is invisible (printed on every run) |
| coverage | `aflj` call/data refs      | exact refs; field depth from disassembly (see below) |
| audit    | overlay DB (delegated)     | identical to the dump backend |

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

## The contributor seed

Maintainers analyse in Binary Ninja databases that embed the retail
executable and can never leave their machine. The seed is what ships
instead (#2830): one xz file per target with only conclusions -
every function's address and current (overlay-applied) name, plus
the target's JSONL evidence ledger. No executable bytes, no
decompiler text: the payload is JSON, and `seed verify` proves no
16-byte run of the binary's `.text` appears in it. Both seeds are
~0.2 MB against a 5 MB cap. Released as GitHub release assets;
`seed-manifest.json` beside the tool pins their hashes.

```
rem maintainer: build (hashes must agree across DB, ledger, binary)
py revtool.py --db <overlay.db> --binary SRO_Client.exe seed build --target client
py revtool.py seed verify revtool-seed-client.xz --binary SRO_Client.exe

rem contributor: rizin + the pinned venv, next to their retail binary
py revtool.py --binary SRO_Client.exe seed apply revtool-seed-client.xz --rzdb SRO_Client.rzdb
```

`apply` hand-defines every seed function (`af+` name, one `afb+`
block to the next start), saves the project and samples the result
against the seed: client 94,865 of 95,290 rows defined in ~73 s
(the rest are import thunks rizin already owns), sampled labelled
names 20/20. Run rizin's own analysis on top for blocks and xrefs.

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
