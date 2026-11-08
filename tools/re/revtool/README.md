# revtool rizin backend

`revtool_rizin.py` lets revtool's six dump-reading commands run on a
machine that has rizin and the retail binary but not Binary Ninja or
its 110 MB HLIL dump export. revtool itself ships in the repo; before
revtool.py grows its own `--backend` switch, this module is run
directly with the same argument shapes:

```
py .state/re-venv/Scripts/python.exe tools/re/revtool/revtool_rizin.py \
    --db revtool.db --binary SRO_Client.exe index
py tools/re/revtool/revtool_rizin.py --db revtool.db view sub_402a70
py tools/re/revtool/revtool_rizin.py --db revtool.db xref data_f0c90c
py tools/re/revtool/revtool_rizin.py --db revtool.db coverage sub_402a70
py tools/re/revtool/revtool_rizin.py --db revtool.db find CIFMain
py tools/re/revtool/revtool_rizin.py --db revtool.db audit some_writeup.md
```

Setup follows docs/native/REVERSE_ENGINEERING.md (pinned venv at
`.state/re-venv` with rzpipe; rizin 0.8.2). The first `index` runs
rizin's call analysis (`aac`, ~1 minute on SRO_Client.exe) and saves
a `.rzdb` beside the DB; later commands reload it instead of
re-analyzing. `--analyze` chooses `aac` (default), `aap`, `aaa` or
`none`.

## Where each command gets its facts

| command  | source                     | notes |
| -------- | -------------------------- | ----- |
| index    | `aflj` + `afsj`            | names normalized `fcn.00402a70` -> `sub_402a70`; signatures are rizin's |
| find     | overlay DB (delegated)     | identical to the dump backend |
| view     | `pdfj`                     | disassembly at true addresses, overlay applied by revtool; rizin 0.8.2 has no `pdc`/`pdg` |
| xref     | `axtj`                     | analysis-derived; un-analyzed code is invisible (printed on every run) |
| coverage | `aflj` call/data refs      | exact refs; field depth from disassembly (see below) |
| audit    | overlay DB (delegated)     | identical to the dump backend |

## Semantics that differ from the dump backend

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
* `start_byte`/`start_line` in the functions table are dump
  geometry; rizin rows store a stable ordinal. Use one backend's
  `index` per DB file (meta records `backend=rizin`).

Tests (`test_revtool_rizin.py`) run against a fake rzpipe session -
no rizin, no binary. A live end-to-end (real rizin + retail client)
is opt-in via `SRO_RIZIN_IT=1` and `SRO_RIZIN_BINARY`.
