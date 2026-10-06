# Skill animation timing audit

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

## Audit still open

The entry-hold repair is established. Adjacent native branches require
separate closure: WAIT installation begins alongside READY rather than
only on READY completion; release/cancellation must preserve the
native ownership of already-installed animations. Do not claim those
branches are covered merely because the entry-hold tests pass.
