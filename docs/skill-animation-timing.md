# Skill animation timing audit

## Server preparation correction (2026-10-06)

The earlier visual repairs below did not establish complete cast timing.
The server loader has an additional normalization step that the port omitted:
`SkillGlobal_LoadReferenceData` reads preparation from reference `+70` at
`5894E3` and adds it into casting at `+74` at `5894E6`. This runs for every
loaded reference, before parameter indexing. Runtime handlers therefore read
columns **11 + 12**, not raw column 12. The addition wraps as a 32-bit integer.

The shared server loader now performs that addition once per loaded row.
Action lifecycle, player/support casts, monster casts and the published action
duration consume the normalized field through their existing owners. Anti
Devil 951 now releases after 970 ms (670 + 300), with 530 ms recovery, instead
of releasing after 300 ms. This supersedes the earlier implication that the
unchanged server release times were correct.

The authored census checks all 27,835 rows; 4,297 have nonzero preparation.
Tests cover the strict release boundary, repeated loads, zero preparation,
preparation-only rows, integer wrapping and malformed timing values. The full
server gate passed, including race checks, followed by all 13 source gates.
Binary Ninja server snapshot 447 contains the normalization annotation.

The isolated build was installed into the local Nomad gameworld, preserving
its authority database. A connected CodexProbe capture observed Anti Devil's
received-to-shot interval increase from 416 ms before the replacement to
992 ms afterward (browser timestamps include scheduling and transport).
The successful capture reported no page errors or caught exceptions. An
initial post-restart login timeout is excluded from gameplay evidence.

The wider skill-column/handler audit is still in progress. Missing column
constants are not by themselves missing features: other parsers own targets,
encoded parameters, UI placement and descriptions. This timing correction
does not establish equivalence for all those owners.

The v1.150 client holds a one-shot animation at its first pose during the
entry blend. The webport previously advanced its pose, sound and stage-key
cursors during that interval. Both the Anti Devil Bow READY/SHOT clips and
Cold Wave Arrest SHOT use the shared 200 ms entry blend.

## Native evidence

- `8DF527..8DF56E` and `8D9816..8D984F` pass 200 ms entry/exit intervals
  and the caster's animation rate at `+4DC` into the animation dispatcher.
  The decompiler incorrectly renders the x87 temporary as the skill object's
  pointer in this path; the assembly loads `[caster+4DC]`.
- `ADF310` initializes the entry countdown at `+8C` and enters state 3.
- `AE0890..AE0905` consumes the entry countdown and skips cursor integration
  while it remains. `AE08E6` subtracts the remaining entry time from the
  crossing frame; `AE090B..AE0931` integrates only the remainder.
- `ADF3A0` clamps a one-shot sample to `length - 1`; natural completion is
  based on the un-clamped cursor in `AE0AD6..AE0B52`.
- `8DF180` still permits a server release to interrupt the READY stage when
  a WAIT motion exists. The repair does not delay the authoritative release.

Binary Ninja client snapshot 394 contains the five newly identified
one-shot playback labels. They were read back from its saved symbol table.

`apps/client-next/tools/native-action-entry.py` executes original reset,
transition setup and advancement bytes. It isolates key/sound/root-motion
receivers, not the clock under test, and verifies execution returned rather
than silently accepting an instruction-count cutoff. The frozen capture is
`apps/client-next/tests/fixtures/native/action-entry.json`. Regenerate and
compare with `SRO_NATIVE_ACTION_ORACLE=1` when running action-schedule tests.

## Live evidence

CodexProbe was tested on the isolated worktree's localhost:5180 client using
normal account login, target selection and skill commands. Actor samples
were copied immediately because the renderer reuses its actor objects.
An earlier capture retained references and is invalid for body timing.

The valid before capture advanced Cold Wave's cursor to 91 ms at 45.5%
blend weight, and Anti Devil's READY cursor to 28.5 ms at 14.2% weight.
After the repair, both retain cursor zero throughout the entry blend.
No page error or caught exception occurred in either valid capture.
Server release timestamps remained unchanged (Cold Wave 96 ms in this
session, Anti Devil 304 ms); those are distinct from authored visual events.

The production GPU rig test passed with boundary pixel change 89,105 versus
746,257 for the hard-cut negative control, and zero difference from idle
after the exit blend settled. Source gates passed (13; unchanged server
checks cached), client gates passed (11), and the original-byte fixture and
presenter tests passed. These are scoped checks, not whole-client parity.

## Action-speed publication and installation capture

The client now retains the reciprocal action speed from player and shared
character spawn rows, local bootstrap and packet 0x3453. The latter routes a
rider GID through the active mount, as 775EB0 does. READY and SHOT capture the
actor rate when installed; later updates do not retime existing clips. Entry
and natural-exit blends remain wall-clock intervals.

Server 4AA530 publishes parameter 8C as a GID and float denominator (v1.188
opcode 3200, v1.150 opcode 3453). Frostbite and slow callbacks call both the
movement and action-speed publishers. The port now does so for players, COS
and monsters, and projects the same value into local, companion and remote
scope-entry snapshots. New hooks are wired in sro-gameworld.

Original-byte captures include half-speed and double-speed one-shots. They
match the port's pose and natural-exit envelopes. Tests cover status apply and
clear publication, entry values, mounted routing, malformed updates, despawn
and replacement, plus preserving an existing installation across a rate update.
The full server gate (including race and lint), all 11 client gates and 13
source gates passed. This follow-up has not yet been exercised against the
running local server binary; the Vite client serves the current source.

## Phase ownership and replacement

The adjacent phase branches are now ported and tested:

- `8E06E0` installs WAIT at cast creation, alongside READY, and leaves base
  action state 3. The presenter now removes the base idle/movement state for
  WAIT-bearing casts and restores it when the cast exits.
- `8D97D0` removes WAIT and installs SHOT without removing READY. The model
  retains that earlier one-shot through its natural completion, including
  after the skill decoration is removed. A single remaining WAIT/SHOT layer
  must still reach the renderer; the old single-layer optimization discarded it.
- Timed WAIT blend time follows its captured rate (`AE05D0`). Its rate also
  reaches the renderer's independent cursor owner. One-shot entry and natural
  exit remain wall-time intervals. Explicit cancellation after entry follows
  the cursor increment multiplied by rate again (`AE0B5F`). Cancellation during
  entry finishes entry and returns to the playing state (`AE08E4`). The native
  fixture now includes early/late cancellation at half, normal and double rate.
- `8DF18C` refuses WAIT release while the guided-trajectory callback owns the
  skill. Both already active travel and travel installed by that same B505
  payload preserve WAIT; result application still proceeds normally.
- `A96E50` first returns a cached geometry prefix/state installation. `ADECF0`
  resets that same installation when it is replayed. The presenter permanently
  retires the old motion producer when replaced, preserving distinct clips and
  other actors. A zero-weight entry also replaces the old installation.

Binary Ninja snapshots 398 and 399 contain the investigated timed-installation,
binding-cache, command-dispatch and container labels. Saved symbols were read
back rather than relying on the save return value.

## Coverage and limits

The authored CH/EU player census resolves all 277 distinct referenced phase
clips: 22 READY, 24 WAIT and 231 SHOT. Every READY/SHOT is a one-shot and every
WAIT is cyclic. All phases are tested at half, normal and double rate (831
phase-clock cases), with no unresolved player phase references. This census
does not cover every monster or system-effect motion.

The focused suite passed 48 tests, including regenerated original-byte
captures, live-presenter ownership, prediction/adoption, guided movement and
the authored census. The production GPU blend test passed: 89,105 changed
pixels across the blended boundary versus 746,257 for the hard-cut negative
control, with zero settled difference. All 11 client gates and 13 source gates
passed, including the server gate.

The connected follow-up captured Anti Devil 951 and Cold Wave 1152 on
CodexProbe without page errors. A failed request for the previously learned
Cold Wave rank 1150 is excluded: that rank is no longer in the character's
learned list. Anti Devil showed READY/WAIT entering together and READY/SHOT
overlapping after release; Cold Wave retained cursor zero during entry.

Evidence establishes the investigated control-flow rules and tested numerical
cases, not bit-exact equivalence for every arbitrary frame sequence. Tiny
native float remainders at zero weight are tolerated by the oracle comparison.
An original-client/original-server visual comparison remains unperformed.
The localhost Vite client serves these changes; the running local server binary
has not been restarted with the preceding action-speed publication changes.
