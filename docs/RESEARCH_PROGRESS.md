# Saving labels and research progress

Store research by **binary SHA-256 and RVA**, with evidence attached to each
claim. A function name is editable interpretation; the bytes and their identity
are the anchor. This works with Python alone and can accompany a Binary Ninja
database. Start with [the tutorial](REVERSE_ENGINEERING.md).

## Choose one durable home

Use an absolute research directory outside the product checkout. A sibling
`research` directory is the existing project convention, but a fresh clone does
not contain it and a worktree's sibling may be a different directory. Record the
canonical path in your untracked `CLAUDE.local.md` or session setup.

For a new standalone investigation, use this layout:

```text
research/
  targets/
    <full-binary-sha256>/
      manifest.json
      labels/
        002a01b0.json
      notes/
        002a01b0.md
      experiments/
        window-restore.py
        window-restore-cases.json
      sessions/
        2026-10-08-agent-a.md
```

The address above illustrates a filename, not a claim about your executable.
Record the actual module and claimed version in `manifest.json`, along with
`schema_version`, `sha256`, `file_size`, `image_base`, `machine`, acquisition
context, Python version and package versions. Version is provenance supplied by
the owner; a filename does not authenticate a release. Hash each module separately.

Keep binaries outside this notes tree. Keep machine-specific binary paths in
local settings; portable notes identify the file by hash. Do not import these
research files into the product at runtime.

## Give every label evidence and a scope

Use one JSON record per function RVA. A data label also needs a kind and size;
a field label belongs to a specific hypothesized structure and field offset,
not to every occurrence of the same numeric offset. Local-variable labels belong
to their function. Do not globally rename all `arg1` or `+0x20` occurrences.

Example function record; replace the illustrative hash/address before use:

```json
{
  "schema_version": 1,
  "binary_sha256": "REPLACE_WITH_FULL_SHA256",
  "rva": "0x002a01b0",
  "kind": "function",
  "name": "candidate_restore_window_position",
  "aliases": [],
  "status": "hypothesis",
  "claim": "Candidate position restoration path; call order not yet checked.",
  "evidence": [],
  "author": "agent-a",
  "updated_utc": "2026-10-08T09:00:00Z"
}
```

This is a portable tutorial schema, not an existing product API or an automatic
Binary Ninja/Revtool import format. A consuming script must reject a mismatched
hash, unsupported schema, invalid/out-of-image RVA or inconsistent filename.
Read JSON as data; never evaluate its fields or use imported paths as commands.
Derive display VA from the image base and saved RVA. Refuse mismatches before
displaying a label as belonging to the currently opened binary.

In each evidence entry, record the instruction range's start RVA and byte count,
exact bytes or range SHA-256, observation, method, and the relevant caller/callee
addresses. A range hash binds an observation to bytes; it does not establish that
the observation is correct. Keep full disassembly trails in research notes.

Use these statuses per claim, splitting records/notes when confidence differs:

| Status | Meaning |
| --- | --- |
| `hypothesis` | A search anchor, proposed name, decompiler interpretation or inference |
| `instruction-checked` | The stated bounded claim was checked against bytes and relevant control flow |
| `bounded-emulation-checked` | Listed fixtures executed the real routine, with limits and substituted dependencies disclosed |
| `port-tested` | Named tests also compare the port with those native cases; retain the underlying native evidence |

These are evidence descriptions, not an automatic ladder of universal proof.
Keep unsupported parts explicit. When a label is disproved, retain its old name
in `aliases` and explain why it was rejected in the history; don't let an alias
appear to endorse the old meaning. Preserve earlier evidence in Git history.

## Notes that another person can resume

Use this template for a function or a connected group:

```markdown
# Investigation: <specific question>

- Target: <module, supplied version, full SHA-256, image base>
- Function(s): <RVA; preferred VA for convenient navigation>
- Current claim and status: <bounded statement; distinguish inference>
- ABI and state: <arguments, return, registers, field offsets/widths, initialization>
- Evidence: <instruction ranges/bytes, callers, callees, order and side effects>
- Experiments: <script and hash, interpreter/packages, command, inputs, results>
- Limits: <unvisited branches, stubs, unresolved indirect calls>
- Rejected hypotheses: <old label/claim and counterexample>
- Port connection: <repository commit, source path, behavior tests>
- Next step: <one concrete investigation or test>
- Attribution: <researcher, reviewer, UTC checkpoint time>
```

Checkpoint after a meaningful finding, a disproved hypothesis, before switching
tasks and at session end. The handoff names the last saved revision and next
command/experiment. Do not put passwords, account tokens or private player data
in notes or experiment fixtures.

## Make the files survive the machine and the chat

A folder outside the product, an ignored `.state` file and an ignored SQLite
database are **not backups**. Put authored notes and harnesses in a dedicated
private Git repository or another backed-up location. For a new empty notes
directory, initialize Git there; do not blindly initialize and add an existing
research tree containing binaries and large dumps.

Before adding files, create exclusions for retail executables/libraries, database
files, caches, virtual environments and large regenerated exports, for example:

```gitignore
*.exe
*.dll
*.bndb*
*.db*
*.sqlite*
.venv/
__pycache__/
cache/
dumps/
```

Stage only selected authored records, notes, scripts and small fixtures. Review
`git diff --cached`, commit with a meaningful finding in the message, then back
up that repository to the team's chosen private destination. A local commit is
recoverable history on one disk; it is not an off-machine backup. Configure a
remote/publish destination only with the owner's authorization. Public product
PRs should contain concise provenance and tests, not retail binaries or complete
disassembly dumps.

With several agents, claim the investigation first. Use a separate session file
per agent and coordinate ownership of shared per-function records. Do not have
two writers read-modify-write the same JSON/JSONL file. Write updates to a temporary
file in the same directory and replace the destination atomically; this avoids
partial files but does not solve lost updates between competing writers. Resolve
conflicting interpretations with evidence and preserve attribution.

## Resume and share across tools

At the next session:

1. Read the manifest, newest handoff and relevant function records.
2. Hash the supplied executable and compare it with the target directory and
   manifest. Stop applying labels if any identity differs.
3. Verify saved instruction bytes/range hashes before building on a claim.
4. Verify the interpreter and package versions, reproduce the smallest relevant
   experiment, then perform the recorded next step.
5. Save new evidence and history before handing off again.

For Binary Ninja, save both the `.bndb` and portable annotations. When exporting,
include symbol addresses converted to RVAs, comments, relevant types/field
layouts, provenance and statuses; a list of names alone loses the investigation.
When importing, validate binary identity first, preview conflicts, and preserve
existing stronger evidence. Test a round trip on a copy before replacing any
authoritative database. Python-only contributors can read the JSON and Markdown
even when the database is unavailable.

Some established private workspaces have Revtool, a SQLite overlay and JSONL
symbol ledgers. Reuse that workspace's annotation/export workflow instead of
creating a competing source of truth. Its `sync export` / `sync import` and
`doctor` operations can preserve and validate the ledger, but verify the installed
tool's help and current paths first. Old notes may reference removed
`reconstruct/` paths. Those private tools and decompiler dumps are not shipped
with a public clone and are not prerequisites for the Python tutorial. An ignored
overlay database alone must never be the only copy of label work.
