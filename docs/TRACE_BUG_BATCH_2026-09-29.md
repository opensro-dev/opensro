# Trace and shared native geometry, 2026-09-29

This continues PR #26 on top of `dfd11b4`. It fixes BUG-025 through the
client and server contract and corrects shared vector arithmetic exposed
by the native review. Production publication is separate from this work.

## BUG-025: Trace/follow

The action panel and quickslots did not dispatch action 1003. The server
also misclassified its native `[01 03 01 u32le target]` body as an alternate
basic attack. Enabling just the client action would therefore be incorrect.

The client now sends the follow family through the worker's shared command
admission. It requires a selected, present, living player and an eligible
local actor. Action-panel and quickslot activation use the same command.

The server separates follow from attack at the wire classifier, validates
the exact payload, and admits only another living player in the same live
world population. Follow shares the existing continuation slot and normal
navigation/movement publication. It never creates a combat skill or deals
damage. It holds between the native body-radius distance bands and resumes
when its target moves away.

Cancellation retires both the continuation and the movement already issued.
An accepted pickup also retires pursuit, preventing competing movement
owners. Death, target logout, teleport, world departure and excessive range
retire the command. The target's session identity prevents a reconnect from
inheriting pursuit admitted for an earlier session.

Native evidence inspected in Binary Ninja:

| Binary / address | Contract |
| --- | --- |
| Client `695420` | Action 1003 sends command family 3 on opcode `72CD`. |
| Server `4AD030`, family 3 | Resolve a living player target in compatible adjacent sectors. |
| Server `4AE3D0` | Follow inner/outer bands use both body radii plus 50/80. |
| Server `4B0350`, `4B0490`, `4B0B20` | Start pursuit, update it, and retire lost targets; 100 ms steering interval, 1,000-unit maximum separation. |

The inspected functions already had descriptive labels, including the follow
handler corrected during the previous batch. New evidence comments were
saved and read back from server database snapshot 233.

## Shared geometry correction

The existing vector helper divided each component by length. Native server
`4328C0` instead stores the reciprocal length as float32 and multiplies each
component by that rounded value. Directional skill admission and follow now
use the same corrected helper.

Server `58AF60` also stores the acos result, the sine helper's result and
the scaled lateral distance as float32. These boundaries are now explicit
in the port. A geometric expectation that `(50, 0, 10)` lies outside a width
of 10 was wrong for the original rounded arithmetic: the machine code
accepts it with zero actor radii and direction `(100, 0, 0)`.

Verification executed original server instructions in Unicorn. Normalization
ran from `4328C0` through its original sqrt helper to a caller sentinel. The
directional probe entered `58B07D` after position projection/range admission,
with their explicit results on the stack; vector, sqrt, acos and sine code
remained original. Only the candidate radius virtual method was a zero-radius
fixture. Tests pin five normalization vectors by exact float32 bits and
cover both sides of the corrected width boundary.

## Other reviewed reports

- BUG-033 remains open. Client `88D340` normalizes a ground-pick ray to
  1,000 units, matching the current port's limit. The old extended-ray
  convenience path was an intentional deviation, not evidence that the
  current limit is a regression. A concrete failed click is still needed
  before changing terrain or navigation admission.
- BUG-035 needs a current-client retest. The authenticated browser capture
  shows the existing shared “Connecting to server” panel while retaining
  the selected character on the dock. A duplicate caption was discarded.
  This batch does not claim to shorten server admission time.

## Validation and test maintenance

- Focused wire and action tests cover command-family isolation, malformed
  requests, range bands, pursuit/hold/resume, cancellation, pickup replacement,
  death, logout/reconnect, teleport and world changes.
- Client regressions exercise direct Trace and quickslot resolution through
  the actual worker, including unavailable actors and unsuitable targets.
- The audio audit test previously rewrote an exact source string and failed
  when gameplay was formatted. It now loads the shipped modules, checks the
  actual world audio bridge, and injects a silent consumer to distinguish
  inventory acceptance from sound delivery. Its esbuild exemption was removed.
- Maintained source was hand-edited to the repository's id Software style,
  formatted by dprint/gofmt, and removed from the formatting debt ledger where
  applicable. No generated source was hand-edited.
- `pnpm check source` passed all eight tasks, including the complete server
  gate: tidy, gofmt, vet, pinned lint, tests, race subset and vulnerabilities.
- `pnpm --filter @sro/client-next check` passed all eleven gates.
- Authenticated Chrome smoke on the updated local server reached the world
  with scratch character `asd2`, no browser errors, ready navigation, and no
  pending/failed UI images. No sources or responses were rewritten.
- A two-browser Trace check entered `CodexProbe` and `asd2` through the actual
  character dock, selected the visible player and issued action 1003 through
  the public worker command. The follower moved about 192 units from an
  initial 250-unit separation, settled about 58 units from the target, and
  held its live gameplay position. Both browsers recorded no page errors.
  The fixture restored the follower's saved position after closing them.
  Target movement/resumption and cancellation are covered by server tests;
  this live check validates a stationary target through the complete port.

The local cluster uses its existing checkout-owned Nomad deployment tool and
state, with the newly built Agent/GameWorld binaries. Source edits in the
other checkout, including its active loot work, were left untouched.
