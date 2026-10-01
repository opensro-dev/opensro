# Moving out of auto-attack — 2026-10-01

The browser could remain in repeating attacks after a ground click. Its
movement reservation waited for the local cast to close, but an old close
and a new attack could arrive in the same worker batch. The client had no
observable idle frame. The missing native contract was the command queue:
the server published B2CD for pickup but not ordinary attack/skill/Trace
admission, and the client treated the reply as pickup-specific.

## Native evidence

Instructions checked through Binary Ninja, not inferred from database names:

- Client `6932A0` (`CGInterface_MoveToWorldPoint`), at `6932D7`, checks the
  stored command count before posture/navigation checks. `67D140` compares
  that byte against two (`cmp`, `sbb`, `add`). Combined with the nonzero
  test, movement sends `72CD [02]` only for **exactly one** command.
- Client `75BAA0` reads B2CD kind and count; `67B0D0` stores the count and
  `67B0E0` clears target-move activity below two. Kind three reads one error
  byte at `75BAF5`, then invokes notification category `0x19`.
- Server `4ACC40` handles bare cancellation. With two entries it removes the
  **back**, preserving the executing front. With one skill command it
  rejects voluntary cancellation in execute phase or after chain commitment.
  Basic attack, approach and Trace cancellation stop their continuation.
- Server `4AD630` replaces a pending back entry while the front executes;
  instant non-activity commands take the separate `4AD870` path. This is a
  common queue rule, not an offensive-skill-only rule.
- `4AD390` finishes the front and reports the remaining count. `4AD270`
  writes that count in both normal replies and refusals. `4AD320` stops
  active ownership and `4AD780` reaches navigator stop `4B03F0`.
- `4AD8E0` and `4AE590` set execute phase when dispatching a cast. The skill
  branch stores chain commitment at `4AEC37`. Movement's existing casting
  admission remains authoritative; cancelling repetition does not erase
  damage already committed.

Corrected a misleading existing database label: `4AF560` is
`CGCharAutoCommandActor_PopQueueBack`, not PopQueueFront. Its sentinel+4
access is the list tail; the executing front uses sentinel+0. Server database
snapshot 313 contains the corrected symbol and explanatory comment; both
were read back. Other inspected functions were already labeled.

The server binary is v1.188; the client wire is v1.150. The former's `0x4004`
committed-action refusal is represented by low byte `4` for the latter's
one-byte reader. This version translation is explicitly an inference. The
v1.150 notification table has no category-25/code-4 text, so it remains silent
while retaining the active count.

## Shared port behavior

| Situation | Behavior |
| --- | --- |
| One basic attack / approach / Trace | Movement requests cancellation before waiting or predicting travel. |
| One committed skill or combo | Voluntary cancellation is refused; its normal close releases the latest held move. |
| Two commands | Cancellation drops only the pending back entry. A held move reacts when the server reports one. |
| New command during a cast | Basic attack, skill, heal, buff, Trace and pickup use one pending slot; replacing it preserves the executing combo. |
| Instant imbue | Uses its existing immediate owner; does not replace the active queue. |
| Queued action execution | Runs through its ordinary authority checks at dequeue; no early damage, healing, item grant or resource charge. |
| Pickup replaces combat | Pickup takes over publication as well as approach. Combat retirement cannot clear its count. |
| Attack/skill replaces pickup | Accepted combat retires the previous pending pickup. |
| Buff cancellation during combat | Reports the remaining count instead of hiding the attack behind an incorrect zero. |
| Death, forced interruption, travel or disconnect | Discards held movement / pending commands through the existing lifecycle owners. World re-entry resets publication. |

The client action-count owner is separate from pickup request coalescing.
Only server replies change the count. A refused movement cancellation is not
resent on every frame. New explicit actions supersede held movement, and a
generic action release no longer rolls back an unrelated predicted walk.

All action-count replies are private to the actor. The public B245/B505
combat fixtures remain unchanged. Tests validate private replies separately
before comparing those native public captures.

## Verification and release boundary

Regression coverage includes batched close/open packets, latest-destination
movement, counts zero/one/two, cancellation coalescing, committed projectiles,
combo preservation, pending replacement, rejected admission, queued heal/buff/
Trace/pickup, independent effect cancellation, forced cancellation and re-entry.

Loot tests that assumed pickup could run before the killing cast closed now
drive the real simulation clock to its close. A dedicated test exercises
pickup queued during combat, its approach and eventual grant. No native
fixture bytes were changed to accommodate the private queue replies.

Browser evidence uses authenticated scratch character `asd2`, the published
Jangan outdoor region and the existing runtime hooks. No served source or
API response is patched. Before the server fix the wire trace contains no
movement cancellation; escaping depends on an observable cast gap. An
authored test area was unsuitable because its client region was not published,
so the scratch character was returned through the ordinary GM warp command.

The patched browser run passed both a runtime movement command and a real
mouse click on the ground during auto-attack. Each sent exactly one cancel
and one movement request, finished the current strike, reached its destination
and left no continuing local cast or client error. The real-click destination
and final server-authoritative X/Z matched at `(518, 312)` in region 25258.
Screenshots, bounded Playwright traces and semantic/wire records are retained
in the worktree's ignored `.state/attack-movement-before`,
`.state/attack-movement-after` and `.state/attack-movement` directories.

Final source verification passed all 12 tasks (61.2 seconds), including Go
tests, race checks, lint, formatting, encoding and vulnerability scanning.
Complete client verification passed all 11 gates (106.6 seconds), including
the new runtime regression suite and test typing.

This change requires both client and server code in the next release, with no
new wire protocol number or database migration. A source merge does not ship
it to players. No production release or deployment is part of this task.
