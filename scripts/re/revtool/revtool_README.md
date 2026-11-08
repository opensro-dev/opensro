# Revtool — provenance-aware symbol overlay for `SRO_Client_psuedo.txt`

A small CLI that makes reverse-engineering the 2.6M-line pseudocode dump faster, by
letting you **rename symbols, name struct fields, and attach notes** without ever
editing the dump. The `.txt` is an immutable **Binary Ninja hypothesis snapshot,
not ground truth**; Revtool keeps a separate SQLite overlay
(`reconstruct/revtool.db`) and composites names on top **on demand**.
The pristine code is always one `--raw` away ? so when an assumption turns out wrong,
nothing was lost.

The retail PE is byte truth. Exact providers prove one interval by compiling,
relocating, and comparing it. Binary Ninja, Rizin, RTTI, Revtool, and the browser
reconstruction corpus are independent evidence sources; disagreement is recorded,
never averaged into a pretend answer.

## Mandatory start for every RE task

From `rebuild`:

```text
.\bootstrap-re.cmd
.\re-python.cmd reconstruct\tools\revtool.py doctor
.\re-python.cmd reconstruct\tools\revtool.py dossier 0x<VA>
```

`bootstrap-re.cmd` is idempotent; run it once initially and whenever
`requirements.txt` changes. All later commands use `re-python.cmd`, which
selects `rebuild\.venv` rather than whichever interpreter `py` happens to
resolve on that machine.

`doctor` fails closed on a wrong retail binary, stale/missing analyzer artifact,
unbound DB, unsynchronized JSONL ledger, or unresolved claim conflict. `dossier`
then gives one hash-bound packet: retail bytes, separate BN/Rizin extents, RTTI and
direct-call anchors, exact-provider state, Revtool claims and history, direct
dependencies, family/planner context, and existing repository work. Do not start
from an isolated decompiler window.

It also checks relationships between artifacts. A fresh BN coverage JSON must
identify the pinned PE and `.text`, match its companion span CSV hash and row
count, and use the supported exporter schema. The reconciled index, progress
map, and family plan must embed the hashes of their current inputs. Thus
`doctor --refresh-lock` cannot turn a stale derived file into trusted evidence.
Regenerate the derived graph in dependency order with:

```text
.\re-python.cmd tools\re\cpp-replacement-proof\refresh_evidence_graph.py
```

Run everything through the repository launcher from `rebuild`:

```
.\re-python.cmd reconstruct\tools\revtool.py <command> ...
```

> Global options `--db` / `--dump` must come **before** the subcommand:
> `.\re-python.cmd reconstruct\tools\revtool.py --db other.db view sub_8b3180`.
> Defaults point at the workspace
> dump and `reconstruct/revtool.db`, so you normally omit them.

---

## Why not just find/replace?

Because the same token means different things in different places. revtool models
three **scopes**:

| Scope      | Example token            | Renamed where                                    |
|------------|--------------------------|--------------------------------------------------|
| **global** | `sub_8b3180`, `data_f0673f` | everywhere in the dump                        |
| **local**  | `var_d0`, `eax_3`, `arg3`  | only inside the one function you name         |
| **field**  | `+0x67e6` on a typed ptr   | wherever a var is *bound* to that struct type |

`var_d0` in `sub_8a7d10` (zenith.r) and `var_d0` in another function are unrelated,
and revtool keeps them that way.

---

## Provisioning (fresh clone)

Two artifacts revtool needs are **intentionally untracked**, so a fresh clone starts
without them. Neither is a bug; both are (re)built locally.

**1. The pseudocode dump (`SRO_Client_psuedo.txt`) — you must supply it.**
It is a **Binary Ninja HLIL linear-view export** of `SRO_Client.exe` (the SSA-subscripted
locals `eax_1`/`edx_3`, `(uint8_t)eax_1 = …` truncating-cast stores, `__thiscall`
demangled C++ signatures, and headers rendered as `struct DOS_Header … =` are all
Binary Ninja's linear view — not Ghidra/Hex-Rays/Rizin). The legacy dump
predates recorded exporter settings, so it is pinned by SHA-256 in
`reconstruct/evidence-lock.json`, marked `legacy-unrecorded`, and explicitly has
no claimed exact regeneration recipe; matching only its approximate size is not
sufficient. The reproducible structured BN exporter is
`tools/re/cpp-replacement-proof/export_binary_ninja_text_coverage.py`. It emits
sorted function spans, block coverage, and a direct-control CSV containing
calls, tail jumps, and unresolved indirect call-like sites, all hash-bound to
the binary and `.text`. These remain analyzer hypotheses, not function-boundary
or semantic ground truth.

`classes` and `vtable --base` do not parse class structure from that old dump.
They use the same PE/MSVC RTTI parser as the function reconciler and UI-family
planner, with the current reconciled entry inventory as candidate code starts.
The command prints its structural source and bounded slot count. This prevents
the old failure where Revtool said "no primary vftable" while the planner found
one, or where `--count` walked from a secondary vtable into the next object.

Drop it at **either** candidate path revtool resolves automatically — `temp/dumps/` is
searched first, then the workspace root:

```
temp/dumps/SRO_Client_psuedo.txt      # preferred (temp/ is gitignored)
SRO_Client_psuedo.txt                 # workspace root (legacy fallback)
```

It is **not tracked** because at ~110 MB it is far too big for the repo, and `temp/` is
gitignored for exactly this class of large regenerable artifact. Keep the (mis)spelling
`psuedo` unless the file is renamed deliberately.

**Validation target — a correct export measures (on this build):**

| Property | Value |
|---|---|
| size | `115,553,624` bytes (110.2 MiB) |
| lines | `2,648,908` |
| functions indexed | `95,290` |

A regenerated dump should land close to these; a wildly smaller size/line count means a
partial or wrong export.

**2. The overlay db (`reconstruct/revtool.db`) — rebuilt per machine.**
It is a local SQLite store (gitignored — a binary that must not be shared; the symbol work
travels as the mergeable `revtool_symbols.jsonl` via `sync export`/`import`). Its function
**index** is fully derivable from the dump — just run `index` once (~1 min):

```
.\re-python.cmd reconstruct\tools\revtool.py index          # scan dump, build the function index (~1min, ~95k funcs)
.\re-python.cmd reconstruct\tools\revtool.py status         # verify: paths, present/size, function count, db state
.\re-python.cmd reconstruct\tools\revtool.py sync import    # pull shared symbol work from the JSONL
.\re-python.cmd reconstruct\tools\revtool.py doctor         # mandatory hash/binding/ledger preflight
```

**Verify provisioning with `doctor`**. `status` is informational; `doctor`
verifies exact artifact hashes, target identity, DB bindings, claim/event parity,
unresolved conflicts, and every derived source-hash edge. `status` also prints the resolved dump path + present/size,
the db path + present/size, and the indexed function count:

```
dump:  ...\temp\dumps\SRO_Client_psuedo.txt  (present, 110.2 MiB (115,553,624 bytes))
db:    ...\reconstruct\revtool.db            (present, 31.3 MiB (32,800,768 bytes))
functions indexed: 95290
```

**Fallback while the dump is absent.** Dump-scanning commands (`xref`, `view`, `find`)
`fail loudly` — they never report a misleading `0 line(s)`. Until you supply the dump, get
cross-references from raw Rizin byte-pattern search on the exe:

```powershell
& $RIZIN -q -c "e scr.color=false; /x 90f55300" SRO_Client.exe   # little-endian VA bytes of 0x0053f590
```

Use `/x` (byte search), **not** `/c` (crypto search on this build). See
`docs/reverse-engineering/RIZIN_GUIDE.md`.

## First run

```
.\re-python.cmd reconstruct\tools\revtool.py index          # scan dump, build the function index (~1min, ~95k funcs)
.\re-python.cmd reconstruct\tools\revtool.py status         # sanity check: dump present, counts
```

Re-run `index` only if the dump file changes.

---

## Reading

```
.\re-python.cmd reconstruct\tools\revtool.py find fog                       # functions whose name/sig/alias contains "fog"
.\re-python.cmd reconstruct\tools\revtool.py view sub_8b3180                # render with overlay applied + legend
.\re-python.cmd reconstruct\tools\revtool.py view sub_8b3180 --raw          # pristine original, untouched
.\re-python.cmd reconstruct\tools\revtool.py view sub_8b3180 --lines 1384289:1384293   # just a dump-line window
.\re-python.cmd reconstruct\tools\revtool.py view 0x8b3180                  # address works too
.\re-python.cmd reconstruct\tools\revtool.py view WorldRenderPass           # so does an applied (renamed) name
.\re-python.cmd reconstruct\tools\revtool.py xref data_f0673f               # every line in the dump that mentions a symbol
```

`view` flags: `--raw` (pristine), `--no-apply` (keep original tokens, only add
annotations), `--no-legend`, `--lines A:B`.

> **`xref` requires the dump.** The dump lives at
> `temp/dumps/SRO_Client_psuedo.txt` (gitignored - at ~110 MB it is too big to
> track); revtool resolves it automatically from a candidate list (`temp/dumps/`
> first, then the workspace root), so no `--dump` flag is needed. If it is missing
> or empty at BOTH locations, `xref` **fails loudly** (non-zero exit) naming the
> path and how to regenerate it, instead of silently reporting `0 line(s)` (which
> reads as "no callers" and produces false RE conclusions). Regenerate:
> whole-program pseudocode decompilation of `SRO_Client.exe` ->
> `temp/dumps/SRO_Client_psuedo.txt` (see `tools/re/pseudo/README.md`),
> then `.\re-python.cmd reconstruct\tools\revtool.py index`. Fallback while it is missing: raw Rizin `/x`
> byte-pattern search (NOT `/c` -> crypto search on this build).

`xref` is the "who calls / who writes this?" tool ? a whole-dump whole-word scan
(mmap, sub-second) grouped by the function each hit lives in. It accepts either the
original token (`data_effe90`) or an applied name (`g_pWorld`) ? an applied name is
resolved back to the original token before scanning (the dump only ever contains the
original `sub_`/`data_` tokens), and the header shows `g_pWorld -> data_effe90`.

### Resolving a virtual call `(*(*obj + 0xNN))()` — `vtable` / `deref`

The text dump has **no data arrays**: a vtable's slot→function table, an `.rdata`
descriptor, a jump table — none of it is in the pseudocode, so `view`/`xref` alone
can't tell you what a virtual call lands on. `vtable` and `deref` read those straight
from the PE image and resolve each pointer back to a (possibly renamed) function with
its RE status. (Point revtool at the binary once: `config binary <SRO_Client.exe>`;
it defaults to `SRO_Client.exe` next to the dump.)

```
.\re-python.cmd reconstruct\tools\revtool.py vtable 0xbcc27c                 # dump the vtable, resolving every slot
.\re-python.cmd reconstruct\tools\revtool.py vtable 0xbcc27c --slot 0x2c     # just the slot a (*(*obj+0x2c))() call uses
.\re-python.cmd reconstruct\tools\revtool.py vtable 0x00c15160 0x00c15290 --slot 0x0c
                                              # compare this slot across explicit vtables
.\re-python.cmd reconstruct\tools\revtool.py vtable --base ClassName --slot 0x2c
                                              # compare across primary-chain RTTI vtables
.\re-python.cmd reconstruct\tools\revtool.py deref 0xce8a38 --count 8        # read raw dwords (pxw) from any VA
```

```
+0x2c   0x004fb7e0  CIFConsole_Update  [labeled]      <- slot resolved + RE status
```

**The full virtual-call recipe (dump-first, binary only for the last hop):**

1. **Type of the object.** Find where it's constructed/assigned (`xref` the global, or
   read the `<field> = sub_factory(...)` line). The MSVC decompiler dump already carries
   **demangled C++ names** — grep for them. E.g. a ctor line reads
   `*(uint32_t*)this = &CIFConsole::``vftable'`{for `CIFWnd'}`, and class registrations
   read `register_class(0xce8a38, "CIFConsole", <ctor_fn>, ...)`. So `xref CIFConsole` or
   `xref "vftable"` often names the type and its ctor for free.
2. **The vtable address.** The ctor's `*(uint32_t*)this = &Class::``vftable'` line names
   it symbolically; the concrete VA you get from `vtable`/`deref` on the ctor, or from
   the one binary read below.
3. **The slot → function (binary-only).** `vtable <vtableVA> --slot 0xNN` gives the
   concrete target and whether it's already RE'd. Then `bind` the object's type and
   `coverage` the resolved function — the normal loop continues from there.

4. **Monomorphic vs polymorphic.** Before writing WIP TS, classify the slot:
   `vtable --base <ReceiverClass> --slot 0xNN` compares the primary inheritance-chain
   vtables for that receiver family. `MONOMORPHIC` means devirtualize the WIP to a
   direct function call and keep the vtable fact in comments. `POLYMORPHIC` means keep
   an interface/dispatch shape and add JSDoc `@see {@link ...}` links to every known
   concrete target. Use `--all-subobjects` only when you intentionally want secondary
   multiple-inheritance vtables included; those are different receiver subobjects and
   can make unrelated slots look polymorphic.

What still needs raw **rizin** (revtool deliberately doesn't reimplement a disassembler):
finding *writers of a global by machine-code pattern* (`mov [imm], reg` byte search) when
no decompiled `<global> = ...` line exists, and reading/parsing actual instruction bytes.
See **[`rizin_recipes.md`](./rizin_recipes.md)** for the exact commands.

### "Does this draw consume that binding?" — `d3dstate`

Born from the color0x18c incident (notes 359→380): a `SetMaterial` immediately before a
draw was attributed to that draw, but `D3DRS_LIGHTING` was OFF at the bind, so the
fixed-function pipeline ignored the material entirely. **Call proximity proves nothing;
only the pipeline state at the draw does.** `d3dstate` walks a function's pseudocode in
order, extracts every device state event (SetRenderState* helpers + raw device
vtable calls: material/light/TSS/FVF/draws), tracks `LIGHTING`, and warns on the trap:

```
.\re-python.cmd reconstruct\tools\revtool.py d3dstate sub_8b3180                 # full FF-state timeline of the function
.\re-python.cmd reconstruct\tools\revtool.py d3dstate sub_8b3180 --at 0x8b359c   # accumulated state AT a draw site
```

```
008b32f4  SetRenderStateCached(LIGHTING=0)   <<< FF lighting OFF
008b34eb  SetMaterial(&var_38)   !! bound while LIGHTING=0 - FF IGNORES the material ...
008b359c  call DrawCloudLayerPass(...) [callee may draw/set state]
state at 0x008b359c: LIGHTING=OFF; active material = bound @0x008b34eb
  -> any draw here CANNOT consume the material (FF ignores D3DMATERIAL9 while lighting is off).
```

Run this BEFORE attributing any per-frame value (material/TFACTOR/light) to a draw.
It is static and per-function: inherited state shows as `INHERITED/unknown` (check the
caller), and opaque callees are surfaced as `[may draw/set state]` rather than hidden.
For the definitive cross-function answer, pair it with a live trace: resolve the device
vtable slot via `runtime` (e.g. `[0xf0f538] -> vtbl+0xc4`) and `break exec` the
implementation with `--read "[esp]:hex"` to capture the per-frame caller sequence.

**Enum decoding + blend conclusion (added after the color0x1c0 incident, notes 347→381).**
The second proximity-class trap is *misremembered D3D enums*: `SetTextureStageState(0,1,7)`
was once recorded as "COLOROP MODULATE" (MODULATE=4; 7 is `ADD`), and undecoded
`SRCBLEND=1/DESTBLEND=3` hid that a pass was a framebuffer MULTIPLY, so the terrain
lightmap pass masqueraded as "some alpha-blended tint" for weeks. `d3dstate` now decodes
every enum it knows (D3DRS values, TSS type+value: D3DTOP/D3DTA incl. modifier bits,
D3DBLEND/D3DCMP/D3DCULL/D3DFOGMODE, draw primitive types, transform-state ids, and the
`IDirect3DDevice9` vtable slots incl. `0x158/0x15c` Create/SetVertexDeclaration — once
misread as a texture bind) and prints a derived conclusion you can paste into notes:

```
008a4d74  SetRenderStateCached(SRCBLEND=1 (ZERO))
008a4d82  SetRenderStateCached(DESTBLEND=3 (SRCCOLOR))
008a4e50  SetTextureStageState(stage0, COLOROP = 7 (ADD))
...
blend/stage conclusion (accumulated over this function; caller state may differ):
  framebuffer = src*ZERO + dest*SRCCOLOR   == framebuffer *= srcColor  (MULTIPLICATIVE darken/tint - NOT alpha blending)
  stage0 colour = TEXTURE ADD TFACTOR
```

It also joins multi-line wrapped calls before parsing (a wrapped `TEXTUREFACTOR=` write
used to be invisible to the parser). NEVER hand-decode a D3D enum in a note again — run
`d3dstate` and copy its decode. Battle-tested against the three verified passes:
`sub_8a4d40` (lightmap multiply), `sub_8ac9c0` (sun: SELECTARG1 TFACTOR + alpha blend),
`sub_8b3180 --at 0x8b359c` (inert material warning + cloud `TEXTURE MODULATE CURRENT`).

### Resolving a call through a RUNTIME field (state machines, `currentX->Method()`)

The cookbook above assumes the object's vtable is in the image. It isn't when the call
goes through a pointer/field that is **installed at runtime** — e.g. a `ProcessManager`'s
*current process*, a plugin, a swapped scene. `deref`/`vtable` on that field returns
`uninitialised (.bss/virtual) -> runtime-populated` (the tool now says so explicitly):
the value is zero in the image and written while the program runs. You can't read it
statically — but you can enumerate every concrete type that gets installed there:

1. **Find the setter.** The field has a fixed offset (e.g. current process at
   `ProcessManager+0x24`, pending at `+0x28`). Find the tiny method `this->field = arg`
   — a `\b+ 0x28\) = arg` grep, or `xref` the field. That's `ProcessManager_SetProcess`.
2. **`xref` the setter → callers install the concrete types.** `xref ProcessManager_SetProcess`
   showed `CPSCharacterSelect_Update` / `CPSVersionCheck_Update` calling it — so the
   installed objects are the **`CPS*`** state classes.
3. **Enumerate the family by naming convention.** `find CPS`, `find _Render` lists every
   sibling state and its render method (the decompiler's demangled names make this work).
   Pick the one for the scene you care about (login = `CPSTitle`).
4. **Resolve that concrete type's slot.** `vtable <CPSTitle vtable> --slot 0x30` (or it may
   already be labeled, like `CPSTitle_Render`). Now `bind` + `coverage` as usual.

The key shift: a *runtime-polymorphic* call is resolved by **who writes the field**, not by
reading the field. revtool's `xref` + `find <Prefix>` do this entirely from the dump; only a
truly static vtable needs the binary. (If even the setter's value is opaque, the writer hunt
drops to rizin — `rizin_recipes.md` recipe 1/2.)

### Reading a value LIVE from the running client — `runtime`

When a value is `.bss`/runtime-populated and the writer hunt is painful, just **ask the
running program**. `runtime` launches `SRO_Client.exe` minimised in the background, polls
the address until it is set & stable (that poll *is* the lifecycle wait: splash → title),
resolves the value back to a (possibly renamed) function, then kills the client. No DLL
injection — it reads the process from outside with `ReadProcessMemory`, read-only. (The
client has no ASLR, `ImageBase=0x400000`, so a static VA equals its runtime address; the
tool still rebases off the real module base.)

```
.\re-python.cmd reconstruct\tools\revtool.py runtime --list-clients                 # just list running client PIDs and exit
.\re-python.cmd reconstruct\tools\revtool.py runtime 0xeffea4                       # launch, read 1 dword, resolve, kill
.\re-python.cmd reconstruct\tools\revtool.py runtime 0xeffe90 --count 12 --expect any   # dump a live object header
.\re-python.cmd reconstruct\tools\revtool.py runtime 0xXXXX --expect code           # wait for a real function pointer
.\re-python.cmd reconstruct\tools\revtool.py runtime 0xXXXX --pid 1234              # attach to a client YOU already
                                                     #   drove to the right scene (no kill)
.\re-python.cmd reconstruct\tools\revtool.py runtime 0xXXXX --observe SWorld+0x14   # also record it; reconciles asserts
```

**Is a client already running? `--list-clients`.** Launching a second client crashes at
splash (single-instance lock), so `runtime`/`break` need an existing `--pid`. `runtime
--list-clients` snapshots running processes by image name and prints their PIDs (it never
launches, attaches, or kills — so it can't hang on warmup), then you pass one to `--pid`:

```
$ .\re-python.cmd reconstruct\tools\revtool.py runtime --list-clients
running sro_client.exe: 1 instance(s)
  pid 38432   ->  read in place:  .\re-python.cmd reconstruct\tools\revtool.py runtime <addr> --pid 38432
```

Key flags: `--expect {nonzero|code|any}` (use `code` for function pointers — it ignores
early-init/heap transients and waits for a pointer *into the module*), `--samples N`
(consecutive equal reads required), `--timeout`, `--interval`, `--warmup S` (wait past
splash before reading), `--pid` (attach, don't launch/kill), `--keep`, `--args`, `--exe`.

**Don't guess the instant — `--watch`.** For per-frame / per-draw values, polling for "the
first nonzero" is fragile: you might snapshot between draws and conclude a field is `0` when
it isn't (or latch a transient). `--watch` samples the **whole** `--timeout` window and
reports, per offset, `nonzero N/total`, the set of `distinct` values, the *time of first
nonzero*, and the last value:

```
.\re-python.cmd reconstruct\tools\revtool.py runtime 0xf0ff28 --count 12 --watch --timeout 30 --interval 150
  +0x0   nonzero 152/169  distinct 2 [0, 0.54353]   first@4.4s  last=0.54353   # settles, stable
  +0x20  nonzero 0/169    distinct 1 [0]            NEVER nonzero  last=0       # genuinely 0
```

That one run proves "diffuse settles at 4.4s and holds" **and** "ambient is really 0 the
whole time" — neither of which a single snapshot can establish.

**Window handling.** A launched client is shoved **off-screen** (not minimised — off-screen
keeps the D3D render loop running, minimise can pause it, and the game re-`ShowWindow`s its
own window so the `STARTUPINFO` hint alone doesn't hold). It renders normally but never pops
up. `--no-hide` to leave it on-screen; window taming only applies to clients we launched, not
`--pid` attaches.

Values are shown as hex, signed int, and IEEE-754 float (so fog/colour/time fields read
directly); a value pointing into the module is resolved to its function name.

**Two modes, by where the value gets set:**

- **Set at startup / ctor** (vtable pointers, config) → plain `runtime <addr>` launches and
  reads it in ~1s. Good for confirming an object's vtable address before `vtable`-resolving
  its slots.
- **Reached at the title/login scene** (the title world/sky object, login UI state) → the
  auto-launched client **does** reach login on its own, minimised (CPU active, window up,
  no server needed — login runs before any gateway connect). Plain `runtime <addr>` works;
  give it a `--warmup` if the value settles a second or two in.
- **Set only in-game** (post-connect world-entry pointers, party/inventory/movement state) →
  needs a real server session, so the auto-launched client won't reach it. Drive a client
  into the world yourself, then `--pid`.

> **It catches static-RE errors.** Real case: the dump rendered the title world-render call
> as `(*(uint32_t*)(data_effe90 + 0x14))(0)`, so `SWorld+0x14` got labeled `pfnRenderWorld`.
> `runtime 0xeffe90 --count 12` showed `+0x14` holds a **heap pointer that changes every
> run** — not a function. The disasm then revealed the decompiler had folded a vtable deref
> (`mov edx,[eax]; mov eax,[edx+0x14]; call eax`): the real call is `SWorld->vtable[0x14]`,
> i.e. `SkyRenderDriver`. A live read in two seconds corrected a wrong field, a wrong name,
> and a wrong mental model. When a decompiled `*(obj+off)(...)` "function pointer" looks odd,
> read it live before trusting it.

### Hardware breakpoints — `break` (what *writes* this? regs *here*?)

`runtime` reads memory; `break` watches **execution and writes**. It attaches to a
**running** client (`--pid`, required — it never launches one), arms a **CPU debug
register** (DR0–DR3 — *no* code patching, no `0xCC` bytes), pumps the Win32 debug loop
until the breakpoint fires, captures the trapped thread's register context, then **detaches
without killing the client** (`DebugSetProcessKillOnExit(FALSE)`).

Use it when static pseudocode can't tell you *which* of many candidate sites writes a field,
or what the registers/operands are at a point of interest.

```
# exec: trap when EIP reaches a VA; dump regs + optional captures.
#   --read exprs: reg | reg+0xN (an address) | [reg+0xN]:TYPE (a 4-byte read),
#                 TYPE in f32|i32|u32|hex.
.\re-python.cmd reconstruct\tools\revtool.py break exec 0x8b1398 --pid 1234 --read "[ecx+0x90]:f32,ecx,edi"

# write: trap when 4 bytes at ADDR (code VA or absolute heap addr) are written;
#        report the WRITER instruction (EIP→function) and the new value.
.\re-python.cmd reconstruct\tools\revtool.py break write 0x00f065b0 --pid 1234

# field: the two-phase killer. Exec-trap at a VA, take <reg>+<off> as a live
#        instance-field address, then WRITE-trap THAT address to find what
#        computes/stores it. --hits N shows the SEQUENCE of writers per frame.
.\re-python.cmd reconstruct\tools\revtool.py break field 0x8b1398 ecx 0x90 --pid 1234 --hits 8
```

Notes / gotchas baked in:

- **WOW64-correct.** The 32-bit client raises `STATUS_WX86_SINGLE_STEP` (0x4000001E), not the
  native `0x80000004`, when a 64-bit Python debugs it — `break` handles both. (Missing this
  passes every hit through unhandled and crashes the client.)
- **Clean detach.** Debug registers are cleared on every thread (each is suspended around the
  write) before `DebugActiveProcessStop`, so no stale breakpoint fires post-detach. `--verbose`
  traces the loop (events, arm counts, exception codes).
- **Code VAs are auto-rebased** by the live module base (the client is `ImageBase=0x400000`,
  delta 0 in practice); heap addresses (e.g. an instance field) are taken absolute.
- Single-shot by default; `--hits N` captures a sequence (e.g. a zero-init writer alternating
  with the real compute), `--timeout S` bounds the wait (default 30s).

> **It nails writers static RE can't — and overturns wrong conclusions.** Real case:
> `RenderComponent` +0x90 — the field the static-object LOD split compares against
> `m_staticObjLodDist` (−170) — has no greppable writer (`+0x90` is a near-universal offset).
> `break field 0x8b1398 ecx 0x90 --pid … --hits 8` caught two writers: **`sub_888140`** (the
> `g_pWorld` vtable+0x1c region-delta transform) writes the home→current region delta, whose
> **Y component is always 0**; then **`sub_853b20` @0x853b5e** adds the object's local offset
> (`arg1[0x24] = arg1[0x20] + regionDelta[1]`). An `exec` breakpoint reading `[edi+0x80]` (localY)
> vs `[edi+0x90]` showed them **equal** (e.g. 83.7375), while `+0x8c` = `(xHome−xCur)*1920 + localX`
> = 2992.87 = 1920 + 1072.87. Conclusion: **+0x90 is component[1] = the object's Y elevation**, not a
> distance/"view-depth" metric — the field was renamed `m_objDistance → m_regionRelPosY` and the old
> "signed view-depth" notes were corrected. So the −170 test is a **height-axis split**, not a
> distance LOD. The earlier static guess was wrong; the live breakpoint settled it.

## Investigate ⇒ label (enforced)

The failure mode this prevents: an engineer (or agent) `view`s/`xref`s a pile of
functions during an investigation, reaches a conclusion, and walks away **without
annotating anything** — so the next person hits those same functions and they read
as if they were never RE'd. revtool now treats investigation as a debt you must
repay with a label.

**What gets tracked.** Every `view` and `xref` is recorded in a local `access_log`
ledger, and an `xref` also records the **callers it reveals**. A symbol is
"labeled" once it has *any* deliberate annotation — a global rename, a symbol note,
or an assertion — even at tier `guess`. Anything investigated but unlabeled is
**investigation debt**.

```
.\re-python.cmd reconstruct\tools\revtool.py status                 # shows "investigated", "UNLABELED debt", strict on/off
.\re-python.cmd reconstruct\tools\revtool.py review --debt          # the debt list + callers an xref revealed but nobody reviewed
```

**Strict mode (ON by default) — with an exploration budget.** RE is non-linear: you
read a function plus a few of its neighbours before any one of them makes sense.
Blocking after a *single* unlabeled view would force premature, low-quality labels
(fake coverage is worse than an honest blank). So strict mode lets you carry a small
backlog — the **exploration budget** (default 3) — and only **refuses** (exit 2) the
next `view`/`xref` once the budget is full. The footer shows your live budget
(`2/3 — 1 more before you must label`) so the block is never a surprise. The symbol
you are currently opening is always exempt. `xref` output also tags every caller
`[labeled]` / `[NEEDS RE]` so the work it uncovers is never invisible.

**The budget is PER AUTHOR — so many agents share one DB safely.** The debt that
gates you counts only the symbols *you* (your `--author`/`$REVTOOL_AUTHOR`) investigated
and left unlabeled. A dozen agents can run against the same `revtool.db` and each carries
its own budget of 3; labels/notes/renames stay shared (a symbol labeled by **anyone**
discharges it for **everyone**), and the "already investigated by X" banner still fires
across agents. The one requirement: **each concurrent agent must set a distinct author.**
If two agents both fall back to the bare OS username they collapse into one shared budget
again — the gate prints a NOTE when it sees the OS-username fallback under contention. Set
it once per agent:

```
set REVTOOL_AUTHOR=agent-kestrel      # (PowerShell: $env:REVTOOL_AUTHOR="agent-kestrel")
# or pass --author agent-kestrel on every call
```

`status` shows both numbers — `YOUR debt (author): N/limit` (what gates you) and
`ALL authors' debt: M` (the shared backlog). The DB is opened in WAL mode with a 30s busy
timeout, so concurrent writes queue instead of throwing "database is locked".

```
.\re-python.cmd reconstruct\tools\revtool.py view sub_4df160        # budget 1/3
.\re-python.cmd reconstruct\tools\revtool.py view sub_72d9b0        # budget 2/3  (roam the neighbourhood)
.\re-python.cmd reconstruct\tools\revtool.py view sub_4df5b0        # budget 3/3 - FULL
.\re-python.cmd reconstruct\tools\revtool.py view sub_7465d0        # REFUSED until you label some of the above
.\re-python.cmd reconstruct\tools\revtool.py seen sub_4df160 "camera-controller advance: accumulates +0xe8, samples track"
.\re-python.cmd reconstruct\tools\revtool.py view sub_7465d0        # now allowed
.\re-python.cmd reconstruct\tools\revtool.py config debt_limit 1    # want label-after-each? set the budget to 1
.\re-python.cmd reconstruct\tools\revtool.py config debt_limit 5    # roam wider before being forced to label
```

`seen <symbol> "<hypothesis>"` is the one-shot triage: a tier-`guess` note that
discharges the debt when you are not sure enough to commit a rename. The body is
**required and must be a real one-liner** (≥8 chars) — `seen X "idk"` is rejected so
triage can't become fake coverage. A real `rename global` / `note add` / `assert`
discharges debt too. Debt is counted **per symbol**, so a function you both `xref`'d
and `view`'d is one unit of debt (one label clears it), not two.

**Lost-work alarm.** The hardest failure to prevent is the engineer who reads a few
functions, ignores the footer, and closes the terminal — the block only fires on the
*next* command, maybe days later or by someone else. So when you `view`/`xref` a
symbol that was **already investigated but never labeled**, revtool fires a banner up
front naming who touched it and when:

```
!! ALREADY INVESTIGATED, NEVER LABELED (xref x1, last 2026-06-09T13:34 by alice).
   You may be repeating lost work. Label it now (or ask the author) ...
```

This turns abandoned debt into a signal at the exact moment it would otherwise be
silently redone — the collaborative payoff the ledger exists for.

**Depth on confidence (so a bare name can't hide lost knowledge).** A function name is
the cheapest possible label and it discharges all debt — so the *compounding* knowledge
(the `this`/main object, struct fields) was never captured (`local renames: 0` for the
life of the project). Fix: when you assert a **confident tier** (`mechanism`/`pipeline`/
`verified`) on a function that actually has structure (≥4 distinct field-access offsets),
you must leave **one** breadcrumb — a `rename local`, a `bind`, or a named `field` — or
pass `--shallow`. `guess`/`unknown` stay frictionless (you're not claiming full
understanding). One breadcrumb breaks the zero-inertia and pushes the highest-value
capture (the struct), which pays off everywhere that struct appears.

```
.\re-python.cmd reconstruct\tools\revtool.py rename global sub_a0f430 ProcessManager_FramePump --tier mechanism
#  REFUSED: touches 11 field offsets, ZERO named locals/fields/bindings
.\re-python.cmd reconstruct\tools\revtool.py bind sub_a0f430 arg1 ProcessManager      # one breadcrumb (the this ptr)
.\re-python.cmd reconstruct\tools\revtool.py rename global sub_a0f430 ProcessManager_FramePump --tier mechanism   # now OK
# escapes:  --tier guess  (not claiming full understanding)  |  --shallow  (audited ack)
```

The same gate applies to `mark` when you promote a function claim to a confident tier.
`--shallow` is recorded as an audited `[shallow-ack]` note + history entry, so over-use is
visible in review rather than silent.

**Work done outside revtool.** The gate only sees investigation done *through* `view`/
`xref`. RE that happens in a doc, an IDA/Ghidra session, or a code comment is invisible
to it. `audit <file>` closes that gap: it scans any file for `sub_`/`data_` tokens and
lists the ones revtool still reads as un-RE'd, so findings written up elsewhere get
captured. Its exit code is the count of unlabeled symbols, so it can gate CI/pre-commit:

```
.\re-python.cmd reconstruct\tools\revtool.py audit ../docs/reverse-engineering/lifecycle/WorldRenderPass.md
#  -> 121 unique symbol(s), 121 labeled, 0 UNLABELED   (exit 0)
```

**Escape hatches.** `--force` on `view`/`xref` bypasses the gate once and records the
bypass in `history` (auditable). Soften globally with `config strict off` (or
`REVTOOL_STRICT=0`); even then the loud footer reminder still prints — it just never
blocks.

### How "done" is a function? — `coverage`

`audit` answers "are these symbols labeled?"; `coverage <func>` answers "how thoroughly
is *this one function* reverse-engineered?". It reads the function body and measures its
**direct references** — every callee it invokes and every `data_` global it touches —
then reports the labeled fraction, the function's own name/depth, and field-offset depth.
Exit code is the number of unlabeled references (scriptable / CI-gateable).

```
.\re-python.cmd reconstruct\tools\revtool.py coverage WinMain
#  self:        named / flat (no struct depth to capture)
#  callees:     20/20 labeled
#  data globals:10/10 labeled
#  field depth: 0/0 offsets named (100%)
#  SYMBOL COVERAGE: 100%  (30/30 refs)
#  VERDICT: FULLY REd
```

The self-depth line distinguishes three states so absence-of-obligation isn't misread as
a gap: **deep** (has a breadcrumb), **flat** (fewer than `STRUCTURE_MIN_OFFSETS` field
accesses — nothing structural to capture), or **SHALLOW** (structured but owes a
breadcrumb). `VERDICT: FULLY REd` requires all refs labeled, every field offset named, and
the function itself named + (deep *or* flat).

When field depth is < 100%, it lists the exact unnamed offsets and their base var (and
whether the var is unbound), so you know precisely what to `field`/`bind`:

```
#  unnamed field offsets:
#    +0x94   eax_13 (unbound - bind it to a type)
```

**Field detection excludes non-field arithmetic.** Only genuine `(char*)var + 0xNN` /
`var[0xNN]` accesses count toward depth. Four shapes that *look* like `base + 0xNN` but are
not receiver fields are filtered out, so the depth denominator stays honest and neither
virtual calls nor jump tables inflate it: (1) **vtable indirection** — `*(T*)var + 0xNN`
adds the offset to the *dereferenced* value (a vtable slot), not to `var`; (2) **code
symbols** — a base of `sub_`/`loc_`/`jump_table_` (e.g. `sub_86ca90+0x91` in a jump-table
entry) is a `.text` address, not a struct; (3) **implausibly large offsets** — anything
`>= FIELD_OFFSET_MAX` (1 MiB) is an absolute `.data` address (`arg2 + 0xc1bce4`), a
sentinel/mask constant (`result + 0x7fffffff`, `+ 0xffffffff`), or a wrapped-negative index
whose `*4` explodes (`ebx[0xffffffff]` → `0x3fffffffc`) — the largest real field in this
codebase is `CRTNavMeshTerrain+0x1b384` (~111 KB), a ≥9× margin; (4) **subscript / declaration
/ address-of** forms (`base[idx + 0xNN]`, `TYPE name[0xNN];`, `&p[0xNN]`).

**Labeled is not the same as readable.** `SYMBOL COVERAGE` counts a symbol as labeled if it
carries *any* annotation — including a `seen` guess, which is just a note and discharges
debt but does **not** rename anything. So the disassembly view still prints the raw
`sub_`/`data_` token. `coverage` therefore reports a second number, `READABLE` = the
fraction of refs with an *applied rename*, and lists the "labeled but NOT renamed" symbols:

```
#  SYMBOL COVERAGE: 100%  (13/13 labeled)
#  READABLE:        85%   (11/13 renamed; 2 note-only)
#  VERDICT: covered, but 2 ref(s) note-only -> raw token still in view
#  labeled but NOT renamed (view still shows the raw token):
#    data_ceb798
#    data_ceb8cc
```

`VERDICT: FULLY REd` now requires **renamed**, not merely labeled (plus depth + self). A
`covered, but N note-only` verdict means the function is fully investigated but `N` refs are
still raw tokens — rename them (promote the guess) to make the body actually readable.
Note: a virtual call `(*(*p + 0xNN))()` stays unnamed even at 100% — that's a vtable-slot,
not a symbol; resolve the slot via the object's vtable if it matters.

```
.\re-python.cmd reconstruct\tools\revtool.py config strict on|off   # persist the policy in the overlay db
.\re-python.cmd reconstruct\tools\revtool.py --no-strict xref ...   # one call, no gate (footer still nags)
```

The ledger is **local workflow state and is not synced** — what syncs (and makes RE
collaborative) is the labels it forces you to create.

---

## Annotating (the overlay)

### Rename ? global
```
.\re-python.cmd reconstruct\tools\revtool.py rename global sub_8b3180 WorldRenderPass
.\re-python.cmd reconstruct\tools\revtool.py rename global data_f0673f g_fogStartNear
```

### Rename ? local (function-scoped)
```
.\re-python.cmd reconstruct\tools\revtool.py rename local sub_8a7d10 var_d0 zenith_r
```

### Struct types + fields (the powerful one)
```
.\re-python.cmd reconstruct\tools\revtool.py type new MapRenderer
.\re-python.cmd reconstruct\tools\revtool.py field MapRenderer 0x67e6 fogColor --type uint32 --note "D3DRS_FOGCOLOR"
.\re-python.cmd reconstruct\tools\revtool.py bind  sub_8b3180 arg1 MapRenderer
```
Now in `view sub_8b3180`, any `*(...)(arg1 + 0x67e6)` is annotated `// arg1->fogColor`,
and the same field name shows up in **every** function where you `bind` a var to
`MapRenderer`. Inspect a type any time:
```
.\re-python.cmd reconstruct\tools\revtool.py type list
.\re-python.cmd reconstruct\tools\revtool.py type show MapRenderer
```

### Notes (free text, line- or symbol-level)
```
.\re-python.cmd reconstruct\tools\revtool.py note add sub_8b3180@008b37ac "FOGSTART=g_fogStartNear, FOGEND=g_fogEndFar (else branch)"
.\re-python.cmd reconstruct\tools\revtool.py note add data_f0673f "config/weather global, default 0; writer not in dump"
.\re-python.cmd reconstruct\tools\revtool.py note list fog          # filter by substring
.\re-python.cmd reconstruct\tools\revtool.py note rm 7              # remove by id
```
A `symbol@addr` note renders inline on that line in `view`; a plain-`addr` note does too.
A symbol-level note (no `@addr`, e.g. `note add SWorld_SetDayTime "..."`) shows in the
`view`/`export` header ? the target may be the original or the applied name.

#### Notes are surfaced automatically — you never have to remember to look
You do **not** invoke `note list` to discover notes; they scream at you where you work:

- **`view <func>` prints a loud `>>> NOTES (...)` box** listing every symbol-level note on
  the function **and on everything it touches** — the types bound in the function, *their
  fields*, and the globals/locals it references. So a wrong/important note attached to a
  *field* or *global* (the classic example: the old `m_objDistance` "view-depth" claim) is
  shown the instant you open **any** function that uses it, not buried behind `note list`.
  The box flags low-confidence/`CORRECTION` notes ("verify, don't assume") and tags each
  with its tier (`[verified]`, `[mechanism]`, …). Field notes also annotate the code lines
  inline (`// ecx->m_regionRelPosY [V]`).
- **`find <pat>` shows a notes badge** per hit (`<3 notes - view/note list …>`) so RE'd
  functions advertise their notes at search time.
- **Why this matters:** the failure mode is reaching a conclusion, writing it on a field/
  global, and the next engineer (or you, weeks later) `view`s a *function* and never sees
  it. The auto-surfacing closes that gap — notes follow the symbol, not just the function.

---

## Confidence, wrong assumptions & collaboration

Every claim (a rename / field / note) carries **provenance** so a half-finished or
mistaken assumption is never mistaken for fact, and several engineers can work at once.

### Confidence tiers
Matches the repository `AGENT.md` ladder: `unknown < guess < mechanism < pipeline < verified`.
New renames/fields default to **guess** (deliberately honest). Promote with evidence:

```
.\re-python.cmd reconstruct\tools\revtool.py rename global sub_8b3180 WorldRenderPass --tier pipeline --evidence "call graph @008b9d34"
.\re-python.cmd reconstruct\tools\revtool.py field MapRenderer 0x67e6 fogColor --tier verified --evidence "rizin @008b3783 D3DRS_FOGCOLOR"
.\re-python.cmd reconstruct\tools\revtool.py mark field:MapRenderer:0x67e6 verified --evidence "..."   # re-tier an existing claim
```
Tiers show in `view` (header `[tier]`, inline `[V]/[p]/[m]/[g]/[?]`, and the legend) and
in `status` (a confidence histogram).

### Wrong assumptions ? dispute, don't delete
When you discover a claim is wrong, **dispute** it instead of deleting. It stays visible
(struck in `view`, listed in `review`) so nobody re-makes the mistake:

```
.\re-python.cmd reconstruct\tools\revtool.py dispute global:g_fogStartNear "approximation; real value is config-driven, writer not found"
.\re-python.cmd reconstruct\tools\revtool.py view sub_8b3180          # shows a "DISPUTED symbols used here (do not trust)" banner
.\re-python.cmd reconstruct\tools\revtool.py review --disputed        # everything currently in doubt
.\re-python.cmd reconstruct\tools\revtool.py resolve global:g_fogStartNear --tier mechanism   # once corrected
```

### Note threads ? supersede, don't accumulate

Disputes cover *claims* (renames/fields); the same rot happens to **notes**. During the
color0x18c investigation, five notes on one site accumulated as a contradictory thread
(a wrong attribution, two caveats, a channel fix, the closure) and a reader could not
tell which was current. Two guards now exist:

- `note add` prints a **heads-up listing other live notes on the same base symbol** so a
  new claim is written *against* the existing thread, not next to it.
- `note add --supersedes "N1,N2"` retires outdated notes **visibly**: they get a
  `[SUPERSEDED by #new]` body prefix (shows everywhere: `view`, `note list`, the JSONL)
  plus a history entry. Nothing is deleted; the trail stays auditable.

```
.\re-python.cmd reconstruct\tools\revtool.py note add "sub_8b3180@0x8b34eb" "CANONICAL status: ..." --tier verified --supersedes "359,363,365"
#   note [380] ...
#   superseded note(s): #359, #363, #365
#   heads-up: 7 other live note(s) on sub_8b3180* - reconcile or supersede if any now contradict: ...
```

Rule of thumb: when an investigation closes, leave exactly ONE canonical note carrying
the conclusion and supersede the interim thinking it replaced.

### Who claimed what + audit trail
Attribute work with `--author NAME` (or set `REVTOOL_AUTHOR`, or it defaults to your OS user).
Every change is appended to an immutable history:

```
.\re-python.cmd reconstruct\tools\revtool.py --author alice rename local sub_8a7d10 var_d0 zenith_r --tier verified
.\re-python.cmd reconstruct\tools\revtool.py history global:data_f0673f    # full dispute/resolve/rename trail for one claim
.\re-python.cmd reconstruct\tools\revtool.py history                        # recent activity across the project
.\re-python.cmd reconstruct\tools\revtool.py review --tier guess            # what still needs verifying
.\re-python.cmd reconstruct\tools\revtool.py review --by bob                # one person's claims
```

### Multiple engineers at once (git-mergeable sync)
Don't share the binary `.db`. Each engineer keeps a local DB and syncs through the repo
as a stable, sorted, one-claim-per-line JSONL that git merges cleanly:

```
.\re-python.cmd reconstruct\tools\revtool.py sync export       # -> reconstruct/revtool_symbols.jsonl  (commit this)
git pull                        # teammate's changes arrive in the JSONL
.\re-python.cmd reconstruct\tools\revtool.py sync import       # add non-conflicting work; never overwrites a conflict
.\re-python.cmd reconstruct\tools\revtool.py sync conflicts    # inspect same-claim divergence
.\re-python.cmd reconstruct\tools\revtool.py sync resolve <id> --take local|remote  # explicit human decision
```
The JSONL is a claims projection plus append-only provenance events. Import is
idempotent. Identical rows are left untouched; a different value for the same
semantic key is stored in `sync_conflicts` and **neither side wins automatically**.
Export refuses to erase remote-only claims/events or unresolved differing work.

## Assertions & dynamic ground truth (so static RE can't mislead you)

This exists because of a real miss. We recorded **"`data_f0673f` is statically `0`, no
writer"** at `verified` tier. It was wrong: the value is `1692.86` at runtime. The
static search ran `rizin /x <absolute-addr>` and found only readers — but the writer is
a `base+disp` store the absolute search **can't see**. We had treated *absence of
evidence* as *evidence of absence*, and the flat tier ladder couldn't tell the two apart.

revtool now models that difference directly.

### Evidence basis — *how* you know, not just *how sure*
Every rename/field and every assertion can carry a **basis**:

| basis | meaning |
|---|---|
| `observed` | positive disassembly/decompiler evidence (the read/write is right here) |
| `dynamic` | confirmed by a runtime trace / injected observation |
| `inferred` | derived from other claims or reasoning (no direct evidence) |
| `absence` | concluded from a search that found **nothing** |

**Guardrail:** an `absence` or `inferred` claim **cannot** be `pipeline`/`verified`. The
tool refuses it. To carry a top tier you must cite `--basis observed` (positive evidence)
or `--basis dynamic` (a confirmed observation). Always pair `--basis absence` with
`--blindspots "what your method can't see"` so the next person knows the limits.

```
# This is REJECTED (the miss can't be recorded as fact):
.\re-python.cmd reconstruct\tools\revtool.py assert add data_f0673f "statically 0" --predicate zero --basis absence --tier verified
#   error: a 'absence' claim cannot be tier 'verified'. ...

# Honest version (capped, and flagged "NEEDS DYNAMIC CONFIRMATION"):
.\re-python.cmd reconstruct\tools\revtool.py assert add data_f0673f "statically 0; no writer found" \
    --predicate zero --basis absence --tier mechanism \
    --method "rizin /x absolute-addr" --blindspots "base+disp, computed-ptr, dynamic"
```

### Assertions — testable propositions, not names
An **assertion** is a falsifiable fact about runtime state, with a machine-checkable
**predicate**: `zero | nonzero | eq:N | ne:N | lt:N | le:N | gt:N | ge:N | range:LO:HI | const | any`.

```
.\re-python.cmd reconstruct\tools\revtool.py assert add data_f0673f "fog near (CPSTitle 0x694E)" \
    --predicate range:1600:1800 --basis dynamic --tier verified \
    --method "native-trace sub_8b3180 field dump"
.\re-python.cmd reconstruct\tools\revtool.py assert list                 # [ ] open  [OK] confirmed  [XX] refuted
.\re-python.cmd reconstruct\tools\revtool.py assert list --status refuted
```

### Observations — close the static↔dynamic loop
Feed runtime facts back in. An observation that **contradicts** an assertion's predicate
auto-flips it to `refuted` **and disputes it** (so it screams in `view`/`review`); a
matching observation **confirms** it and upgrades its basis to `dynamic`.

```
.\re-python.cmd reconstruct\tools\revtool.py observe data_f0673f 1692.86 --source native-trace.log:19
#   assert #1: REFUTED !!  (1692.86 != 0)  -- statically 0; no writer found

# Or bulk-reconcile a whole injected-DLL capture in one shot:
.\re-python.cmd reconstruct\tools\revtool.py ingest temp/logs/native-trace/sro-native-trace.log
#   parses WORLD_RENDER_PASS data_f0673f(near)=... and FOG_RS FOGSTART float=...
#   REFUTED assert #1 by data_f0673f=1692.86: statically 0; no writer found
#   assertions: 2 confirmed, 2 refuted.
```

### The standing "what might be lying to us" report
```
.\re-python.cmd reconstruct\tools\revtool.py review --needs-dynamic
```
Lists every high-tier claim resting on `absence`/`inferred` that was **never confirmed at
runtime**, plus open/refuted assertions. Had this existed, the `data_f0673f` claim would
have been the headline — with its blind spots printed right next to it.

> **Workflow rule:** any claim that predicts a runtime value (a constant, a "this is
> always 0", a fog distance, an enabled/disabled flag) should be an **assertion with a
> predicate**, not a `verified` rename. Then one `ingest` of an injected-DLL log either
> confirms it or refutes it — exactly the loop that would have caught the fog bug.

## Codebase mapping — turn the binary into structured C (7 formats)

Hand-RE'ing 95k functions one `view` at a time doesn't scale. These seven commands bulk-map
the codebase by mining sources the pseudocode body alone can't give — debug **assert
strings**, **RTTI/vtable** tables, and **injected runtime traces** — and feed the results
into the *same* overlay tables (`globals`/`fields`/`bindings`/`notes`/`assertions`) the
manual commands write. So everything they produce carries provenance/tiers, shows up in
`view`/`coverage`, and travels through `sync`. **No Ghidra, no external decompiler** — only
revtool, the PE, and the dump (the dump used only where the binary can't answer).

Two are **static** (run anytime), four **ingest dynamic traces** (a JSONL the
[`native-trace`](native-trace) DLL emits — schema below), and one
**emits typed C**. All seven are covered by `tests/test_codebase_mapping.py` (20 checks,
real dump/PE for the static ones, JSONL fixtures into a throwaway DB copy for the rest).

| # | command | kind | mines | produces |
|---|---|---|---|---|
| 1 | `asserts` | static | `ASSERT(...,"file.cpp","expr")` strings | source module + member/method names per function |
| 2 | `classes` | static | RTTI Base-Class-Array + vftable | class hierarchy + per-class vtable slots |
| 3 | `ingest-calls` | dynamic | `{kind:call}` records | call edges + `arg1` type from observed `this` vtable |
| 4 | `ingest-allocs` | dynamic | `{kind:alloc/ctor}` | `sizeof(class)` |
| 5 | `ingest-layout` | dynamic | `{kind:field}` (guard-page) | struct fields (unnamed offsets only) |
| 6 | `infer-types` | dynamic | `{kind:sample}` | field/global C type + a dynamic assertion |
| 7 | `reconstruct` | export | the overlay | typed C: struct decls + `v->field`-rewritten body |

### 1. `asserts` — source modules & member names for free

The client is an assert-heavy build: every `ASSERT` compiles to a call carrying its
`__FILE__` and the **stringified condition**, which routinely names real members
(`m_sDungeonInfo`), methods, and invariants (`iRid <= 0x7FFF`). Scraping the adjacency of a
`"…cpp"` literal and the following expression maps each function to its **original source
module** and yields rename/field candidates — zero runtime needed.

```
.\re-python.cmd reconstruct\tools\revtool.py asserts                          # summary: top source modules by assert count
.\re-python.cmd reconstruct\tools\revtool.py asserts --file maploader.cpp      # functions whose asserts cite this .cpp + exprs
.\re-python.cmd reconstruct\tools\revtool.py asserts --func MapLoader_FindDungeonInfo   # one function's source + members
.\re-python.cmd reconstruct\tools\revtool.py asserts --suggest                 # functions with named members = rename targets
.\re-python.cmd reconstruct\tools\revtool.py asserts --apply-notes             # attach `src=<file>; asserts name: m_x,…` notes
```

It captures **both** assert encodings the decompiler emits:
1. **inline** — `sub_467c50(cond, 0x26c, ".\maploader.cpp", "it != m_sDungeonInfo.end()")`;
2. **pointer** — `sub_467c50(arg3, 0x17f, ".\maploader.cpp", &data_c43d30)`, where a second
   pass resolves `&data_xxxx` to its string, handling both the `char const data_…[] = "…"`
   form and the raw hex-dump form short strings use (`<short_string_data>:` → `30 00 00 00` → `"0"`).

Pointer-form conditions are usually generic sentinels (`"%s"`, `"0"`), so they rarely add a
*member* name — but they still attribute their function to the right **source module**
(e.g. `<network_packet_handler>` → `netprocessininner.cpp`), which inline-only scraping missed entirely.
The summary line reports the split: `213 assert strings (198 inline, 15 pointer-form, 15
resolved)`. Repeated identical conditions are collapsed with an `(xN)` count in `--func`/
`--file` views. `--apply-notes` is idempotent (skips functions that already have a `src=`
note). Remaining limitation: asserts whose **file** path is itself a `&data_` pointer (rare;
all observed call sites inline the file string) are not matched.

### 2. `classes` — the C++ type graph from RTTI

The dump fully labels MSVC RTTI. A class's **Base Class Array** lists, nearest-first, the
class then *all* its ancestors, so one array is the whole inheritance chain. `classes` parses
every array into a hierarchy and resolves each class's vtable slots straight from the PE.

```
.\re-python.cmd reconstruct\tools\revtool.py classes                  # totals + root classes (+descendant counts)
.\re-python.cmd reconstruct\tools\revtool.py classes CObj             # direct base, all ancestors, subclasses, vtable slots
.\re-python.cmd reconstruct\tools\revtool.py classes --base CObj      # every subclass of CObj (e.g. 349 of them)
.\re-python.cmd reconstruct\tools\revtool.py classes --list           # every class with its base + vtable VA
```
Example: `classes CObj` shows `direct subclasses: CObjChild` and resolves `+0x0/4/8` of its
vtable to the concrete `0x005930b0/c0/e0`; `classes CGWndBase` recovers `CGWndBase : CObjChild
: CObj`.

### 3–6. Dynamic-trace ingest (JSONL from `native-trace`)

These read a **JSONL trace** — one JSON object per line, a `kind` field selects the record
(`#`/`//` comment lines and malformed lines are skipped). Run with `--apply` to write facts
into the overlay; without it they only print a report. All applied facts get `basis=dynamic`.
`vtable` fields are mapped to class names via the RTTI index (so point `--dump` at the dump).

```jsonc
// format 3 — ingest-calls
{"kind":"call","caller":"0x8b9050","callee":"0x863330","this_vtable":"0xbdbc14"}
// format 4 — ingest-allocs   (correlate ret↔obj, or the pre-correlated {class,size} shortcut)
{"kind":"alloc","ret":"0x10000000","size":320,"site":"0x401000"}
{"kind":"ctor","obj":"0x10000000","vtable":"0xbdbc14"}
{"kind":"alloc","class":"CRegionManager","size":8284}
// format 5 — ingest-layout   (guard-page field access: offset, width, r/w)
{"kind":"field","type":"CObj","offset":"0x78","size":2,"access":"r"}
{"kind":"field","vtable":"0xbdbc14","offset":"0x7c","size":4,"access":"w"}
// format 6 — infer-types     (value samples; optional "as":"f32"|"u32"|"ptr" override)
{"kind":"sample","subject":"CObj+0x7c","value":"0x42c80000"}
{"kind":"sample","subject":"data_f0673f","value":"0x44d39b85"}
```
Addresses/sizes accept `0x..`, bare 8-hex (like the dump), or decimal.

```
.\re-python.cmd reconstruct\tools\revtool.py ingest-calls calls.jsonl  --apply   # binds callee arg1 to the observed class
.\re-python.cmd reconstruct\tools\revtool.py ingest-allocs allocs.jsonl --apply  # sizeof(class) -> type note + observation
.\re-python.cmd reconstruct\tools\revtool.py ingest-layout layout.jsonl --apply  # fields for UNNAMED offsets (never clobbers)
.\re-python.cmd reconstruct\tools\revtool.py infer-types  samples.jsonl --apply  # field type + a confirmed dynamic assertion
```
- **3 `ingest-calls`** resolves indirect/virtual dispatch from ground truth: when a record
  carries the live `this` vtable, it maps it to a class and binds the callee's `arg1` to it.
- **4 `ingest-allocs`** correlates an allocation's returned pointer with the ctor that stamps
  a vtable into it → `sizeof(class)`, which sizes the struct and labels the alloc site.
- **5 `ingest-layout`** turns guard-page access records into fields (`field_0xNN`, size→C type,
  observed r/w). It **only fills unnamed offsets** — hand-named fields are never overwritten.
- **6 `infer-types`** classifies value samples (float / pointer / enum-const / bitmask / int
  range) and, with `--apply`, sets the field type for `Class+0xNN` subjects and records a
  `dynamic`-basis assertion for any subject.

### 7. `reconstruct` — typed C, not `*(uint32_t*)((char*)a1+0x78)`

Emits source-like C for a function from the overlay: the struct declarations its bound types
imply (offset-ordered, with padding) and the body with global renames applied **and** every
offset-arithmetic field access rewritten to `var->field`.

```
.\re-python.cmd reconstruct\tools\revtool.py reconstruct CObj_IsVisibleInSector       # to stdout
.\re-python.cmd reconstruct\tools\revtool.py reconstruct sub_863330 --out out.c       # to a file
```
Given `arg1` bound to `CObj` with `+0x78 = m_homeSector`, the line
`*(uint16_t*)((char*)arg1 + 0x78) & 0x8000` becomes `arg1->m_homeSector & 0x8000`, preceded
by a generated `struct CObj { … uint16_t m_homeSector; … };`. It is **not** a
compiler-faithful decompile — it's the pseudocode body with the field/name overlay resolved
so it reads like the original. The more you `bind`/`field`, the closer the output.

### Trust & limits (read before you lean on these)

These commands are accelerators, not oracles. Where they guess, they say so — but know the
edges:

- **Static (`asserts`, `classes`) is high-trust.** It reads strings/RTTI the compiler emitted;
  treat it as fact. One caveat: `classes` recovers ancestors/descendants and `--base` from the
  full RTTI Base-Class-Array (always correct), but the **"direct base"/"direct subclasses"**
  labels assume single inheritance. For the handful of multiple-inheritance classes a second
  base shows up under ancestors, not as a "direct" base. `asserts` member-mining is `m_*`-only
  by design (high signal); read the printed expression for everything else.
- **The dynamic four (`ingest-*`, `infer-types`) have NO producer yet.** The `native-trace`
  DLL does not emit the JSONL these consume — the call/alloc/ctor/guard-page/sample hooks are
  **still to be built** (see the native-trace README for the schema and suggested hook points).
  Until then these run only against hand-written fixtures. The commands are correct; the
  capture pipeline is the unfinished half. Don't plan work assuming "dynamic mapping" is live.
- **`infer-types` is a heuristic, and is recorded as one.** Sampled values are real
  (`basis=dynamic`), but float/pointer/bitmask classification from a few samples misfires
  easily (random ints reinterpret to plausible floats). It writes fields at `tier=guess` and
  assertions as `[heuristic] … status=open` on purpose — they appear in `review` as *needs
  confirmation*, never as ground truth. Verify before promoting.
- **`ingest-calls --apply` binds `arg1` to the most-frequently-observed `this` class.** For a
  genuinely polymorphic callee (a base virtual hit via several derived types) that "winner"
  may be a derived class where the real parameter type is the base. It's a `mechanism` hint,
  not gospel — sanity-check the binding.
- **`reconstruct` is a readability LENS, not a decompile.** It rewrites only a few
  `*(T*)((char*)v+0xNN)` shapes; nested/indexed/unbound derefs stay raw. The header prints the
  exact rewrite coverage (`N -> v->field, M left raw`) so you can see how much is real. Never
  paste its output as source-of-truth.

What these deliberately do **not** try to be (use raw **rizin** instead, per
[`rizin_recipes.md`](./rizin_recipes.md)): an instruction-byte reader, a writer-by-machine-code
finder, or a true data xref engine. None of the formats above touch those — they mine strings,
RTTI, and traces only.

### The compounding loop

`asserts --apply-notes` tags every function with its source module → `classes` gives the type
graph → `ingest-*` fill bindings/sizes/fields/types from a single live capture → `reconstruct`
renders readable C, which makes the *next* manual `view` faster, which produces better
bindings. Each format feeds the others, all through the one provenance-tracked overlay.

---

## Exporting to a `verified_*` doc

```
.\re-python.cmd reconstruct\tools\revtool.py export sub_8b3180 > ../draft_sub_8b3180_WorldRenderPass.txt
```
Emits a doc in the house style: signature, bindings (with their fields), locals,
notes, then the annotated body. Use it to seed/refresh a `verified_*.txt`.

---

## How it works / safety

- **Never writes the dump.** All state lives in `reconstruct/revtool.db` (SQLite).
  Delete that file to reset; back it up to share your symbol work.
- `index` records each function's byte range, so `view` seeks directly ? instant
  regardless of the 2.6M-line size. Headers are detected three ways so nothing is
  missed: an explicit calling convention, a self-referential `sub_<own-addr>(`
  definition (covers convention-less leaf/math helpers), or a declaration line
  immediately followed by a same-address `{` (covers convention-less named funcs).
  A mistyped `view --lines A:B` outside the function warns instead of showing blank.
- Renames are whole-word token substitutions applied only within the relevant scope,
  with a `new <- orig` legend so the mapping is always visible. `--raw` shows the
  untouched source for when an assumption needs re-checking.

---

## Command reference

| Command | Purpose |
|---|---|
| `index` | scan dump, (re)build function index |
| `status` | overlay/index stats |
| `find <substr>` | locate functions by name / signature / alias |
| `view <fn> [--raw\|--no-apply\|--no-legend\|--lines A:B] [--force]` | render a function (records investigation; strict-gated) |
| `xref <symbol> [--force]` | every dump line mentioning a symbol, grouped by function (tags callers `[NEEDS RE]`; strict-gated) |
| `seen <symbol> <body> [--tier T]` | one-shot triage: label an investigated symbol with a hypothesis (body required, ≥8 chars; default tier `guess`) |
| `audit <file> [--quiet]` | scan any file (doc/source/RE writeup) for `sub_`/`data_` symbols with no label; exit code = #unlabeled (CI-gateable) |
| `coverage <func>` | how thoroughly one function is RE'd: % of referenced callees/data labeled + self name/depth + field depth + verdict; exit code = #unlabeled refs |
| `vtable [addr ...] [--count N\|--slot 0xNN] [--base Class] [--all-subobjects]` | dump a vtable from the PE, resolving each slot to a function; with multiple addrs or `--base` plus `--slot`, classify the slot as monomorphic/polymorphic across receiver vtables |
| `deref <addr> [--count N]` | read N dwords from a VA in the PE (the `pxw` equivalent) for `.rdata`/descriptors/jump tables the text dump omits |
| `runtime <addr> [--count N] [--watch] [--expect nonzero\|code\|any] [--samples N] [--warmup S] [--pid P] [--exe ...] [--args ...] [--timeout S] [--interval MS] [--no-hide] [--keep] [--observe SUBJ]` | read a value **LIVE** from a running client (launches it **hidden off-screen**, polls until set, resolves to a function/float, kills it); resolves `.bss`/runtime values `deref` can't see. `--watch` summarises what each offset does over the whole window (nonzero%, distinct, first-nonzero) instead of guessing the instant. `--pid` attaches to a client you drove to a scene (no kill) |
| `config strict [on\|off]` | show/persist strict enforcement of investigate⇒label |
| `config debt_limit [N]` | show/persist the exploration budget (unlabeled symbols allowed before the gate blocks; default 3) |
| `config binary [<path>]` | show/persist the PE image path used by `vtable`/`deref` (default `SRO_Client.exe`) |
| `rename global <orig> <new> [--tier T] [--evidence ...]` | rename a `sub_`/`data_` everywhere |
| `rename local <fn> <orig> <new> [--tier T] [--evidence ...]` | rename a local within one function |
| `type new\|list\|show [name]` | manage struct types |
| `field <type> <0xoff> <name> [--type T] [--note ...] [--tier T] [--evidence ...]` | name a struct field |
| `bind <fn> <var> <type> [--tier T]` | bind a function-local var to a struct type |
| `note add\|list\|rm <target> [body] [--tier T]` | attach / list / remove notes |
| `mark <claim> <tier> [--evidence ...] [--basis B]` | set the confidence tier of a claim (basis guardrail enforced) |
| `dispute <claim> <reason>` | flag a claim as wrong/suspect (kept visible) |
| `resolve <claim> [--tier T]` | clear a dispute, optionally re-tier |
| `history [<claim>] [--limit N]` | audit trail for a claim, or recent activity |
| `review [--tier T\|--disputed\|--needs-dynamic\|--debt\|--by AUTHOR]` | list claims needing attention (`--debt` = investigated-but-unlabeled) |
| `assert add <subject> <claim> [--predicate P --basis B --method ... --blindspots ... --tier T --expected ...]` | record a testable runtime assertion |
| `assert list [--subject S\|--status open\|confirmed\|refuted]` | list assertions |
| `observe <subject> <value> [--source ...]` | record one runtime fact; auto-reconciles assertions |
| `ingest <native-trace.log>` | bulk-observe from an injected-DLL log; confirms/refutes assertions |
| `sync export\|import [path]` | git-mergeable JSONL for multi-engineer collaboration |
| `export <fn>` | emit a `verified_*`-style doc from the overlay |
| `asserts [--file F\|--func FN\|--suggest\|--apply-notes] [--limit N]` | [fmt1] scrape `ASSERT(…,"file.cpp","expr")` strings → per-function source module + member/method names |
| `classes [name] [--base B\|--list] [--count N] [--limit N]` | [fmt2] RTTI/vftable → class hierarchy (bases/subclasses) + per-class vtable slots |
| `ingest-calls <log.jsonl> [--apply] [--limit N]` | [fmt3] dynamic call graph; observed `this` vtable → bind callee `arg1` to its class |
| `ingest-allocs <log.jsonl> [--apply]` | [fmt4] alloc size + ctor vtable → `sizeof(class)` |
| `ingest-layout <log.jsonl> [--apply]` | [fmt5] guard-page field accesses → struct fields (unnamed offsets only) |
| `infer-types <log.jsonl> [--apply]` | [fmt6] value samples → field/global C type + a dynamic assertion |
| `reconstruct <fn> [--out FILE]` | [fmt7] emit typed C: struct decls + body with `*(T*)(…+0xNN)` rewritten to `v->field` |

Global options (before the subcommand): `--db`, `--dump`, `--author`, `--strict`,
`--no-strict`. Strict enforcement (investigate⇒label) is ON by default; set with
`config strict on|off`, `REVTOOL_STRICT=0/1`, or per-call `--strict`/`--no-strict`.
The exploration budget (unlabeled symbols allowed before the gate blocks) defaults
to 3; set with `config debt_limit N` or `REVTOOL_DEBT_LIMIT`.
Claim refs for `mark`/`dispute`/`resolve`/`history`:
Additional binding claim ref: `bind:FUNC:var`. Disputed binding rows are ignored
by `view`, `coverage`, and other overlay readers, so a wrong type hint can stay
auditable without polluting future output.
`global:NAME` · `local:FUNC:orig` · `field:TYPE:0xoff` · `note:ID` · `assert:ID`.
Evidence basis (rename/field/assert/mark): `observed` · `dynamic` · `inferred` · `absence`
(absence/inferred can't reach `pipeline`/`verified`).

## If the tool has bug, you need to fix the tool!
Feel free to extend the tool too, if there are missing features
