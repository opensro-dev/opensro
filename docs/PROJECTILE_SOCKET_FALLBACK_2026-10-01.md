# Projectile socket fallback — 2026-10-01

A player reported a red client error for skill 8074, SHOT event 1, stage 3:
`AT_MOV_1TAR/MOV_STRAIGHT/arrow`, using `cha_arrow_normal.bsr`.
This is Rogue crossbow Power Shot. Its launch marker is `ai_end`.

The published release contains the skill definition, model manifest entry and
arrow GLB. Their delivered bytes were checked during investigation. The old
error combined missing assets, missing endpoints and unsupported operations;
the screenshot alone cannot establish which admission predicate failed. No
browser trace was supplied. Do not describe this patch as a confirmed replay
of that player's incident, or the timing after a server update as proof of a
server regression.

## Native evidence

Checked instructions in the v1.150 client through Binary Ninja:

- `8DE2B4` calls `Animation_CalculateBoneSocketWorldPosition` (`8D6330`)
  for the distributed mover's destination.
- `CIDecoSkillEffectEntity_Initialize` (`8D8E30`) calls the same helper at
  `8D8ED2` for the launch point.
- `8D64E5` queries the named socket. On a miss, `8D64ED` resolves the mount,
  and `8D653F` retries there. If there is no further mount, `8D64F6` branches
  to `8D656C`, retaining the root transform and applying the offset. A missing
  bone does not reject this supported projectile.
- Attached effects have a corresponding fallback in `8D6880`; that behavior
  was already present in the renderer's attachment transform path. The socket
  query path had remained strict.

These functions already had descriptive names. No unnamed functions were
emitted by the investigation, and no database renames were needed.

## Port change and coverage

Effect queries now explicitly request `mount-root` fallback from the shared
renderer socket owner. This covers launch, destination, retarget and other
effect anchors through their existing callback. It is not specific to skill
8074, arrows, race or local-player status. Existing socket hits retain priority.
Strict string queries still return null for absent markers; footstep/contact
consumers must not invent a ground contact at the character root.

An unavailable actor/model remains unavailable. It is not treated as proof that
its skeleton lacks a marker. Cold-model callback admission is a separate
lifecycle issue and has not been changed here. Effect admission errors now
include the failing source/target GID and marker, missing resource or region
pair where applicable, allowing a future report to distinguish those cases.

Tests exercise actual renderer poses and the moving effect owner: body marker
hit, root fallback, rotated offsets, mount marker and mount-root fallback,
missing/cold actors, cyclic mounts, Power Shot launch/arrival with missing
source and target markers, and actionable cold-source diagnostics.

No server protocol, gameplay damage, inventory, or release data change is
required for this client correction. Shipping status must be checked against
the release identity; a source merge alone does not update production.

## Verification

- Focused effect, moving-stage, compound-socket and fallback suites: 46 tests passed.
- Final client check: all 11 gates passed, including runtime/architecture tests
  and the test-type gate (93.2 seconds).
- Source check: all 12 tasks passed with the existing licensed server-data
  projection selected (68.7 seconds).
- `git diff --check` and formatting checks passed.

The first client run found incomplete types in the new test fixture; those were
corrected without extending the debt ledger. The first source run lacked the
worktree's licensed-data path and hit a vulnerability-service connection error;
both checks passed on the configured retry. No browser-session reproduction or
production deployment was performed.
