# BUG-046: berserk hair and independent compound attachments

The published Chinese male and female berserk hair was present but rotated
sideways beside the head. The ordinary hair was correctly covered, leaving the
upper head visibly bare. An actor-presence assertion could pass despite this
rendering defect.

## Native evidence

Checked the v1.150 client instructions through Binary Ninja, rather than relying
on existing function names:

- `8E9060`: chooses the male or female Chinese Hwan hair resource and attaches it
  through the character's compound-part path at `8E917D`.
- Both authored Hwan BSRs declare `Bip01 Head` as their skeleton attachment bone.
- `ABC680`: finds the branch containing the authored marker, links the child's
  root to that marker, and writes the attach-root flag at `ABC720..ABC723`.
- `AB58C5..AB58E0`: copies the parent skin matrix from its matrix block at `+80`.
- `AB58E2..AB5916`: replaces that matrix's translation with the parent's sampled
  world translation from the matrix block at `+40`.
- `AB5919..AB5924`: multiplies the child root by this corrected parent frame.

The result retains the parent's animated change from bind pose, but cancels the
bind rotation itself. Ordinary marker attachment instead retains the full
sampled bone rotation, which caused the sideways hair.

All five unnamed helpers encountered were labeled and their saved symbols were
read back from client database snapshot 210. The BUG-046 instruction comment was
also verified in that snapshot:

| Address | Label |
| --- | --- |
| `A97F00` | `CCompoundObj_FindBranchContainingMarker` |
| `AB9470` | `CRTBranchChildList_CreateNode` |
| `ABA5B0` | `CRTBranchChildList_AddSizeChecked` |
| `A92870` | `CRTBranchMapIterator_Increment` |
| `AB8100` | `CRTBranchChildList_AllocateNodes` |

## Ownership and related paths

| Boundary | Responsibility and result |
| --- | --- |
| Server `action/berserk.go` | Owns activation, body status, expiry and death cleanup. These remain server authority. |
| Character presentation | Chooses the gender-specific resource, covers ordinary hair, and publishes a separate animated hair actor. |
| Animated avatar auxiliaries | Use the same private-skeleton attachment rule and now request the same compound basis. |
| Character renderer | Resolves the requested basis on the wearer and on the existing mount-marker fallback. |
| Animation pose | Produces the compound frame from current rotation and authored bind rotation while retaining current translation. |
| Equipment sockets | Shares the bind-pose reader already used by embedded private branches. |
| Actor snapshots | Already retain the attachment basis when publishing and updating actors. |

Independently decoded child models carry their own import-basis adapter. Their
parent correction includes the complete imported bind frame so that adapter is
applied exactly once. Embedded equipment branches already share the body adapter
and continue to exclude it from their bind correction. The extra retained bind
matrices are included in the character pose memory budget.

This is a client code change. Existing published model bytes, protocol and store
schema remain compatible. No server gameplay change or data rebuild is required.

## Regression evidence

- `compound-attachments.test.mjs`: rotated bind pose, subsequent animation,
  unchanged ordinary sockets, copied result ownership, wearer scale, missing
  marker, missing owner and mount-marker fallback.
- `character-presentation.test.mjs`: both Chinese genders, local and remote
  actors, cold hair admission, expiry and despawn. The avatar-wing lifecycle
  regression also checks the shared compound basis across all four body rigs.
- Existing `compound-sockets.test.mjs` and `equipment-particles.test.mjs` continue
  to exercise embedded equipment branches and their private markers.
- `compound-hair.test.mjs`: production decoder, renderer and published models
  on WebGPU. It captures visible pixels for both genders and rejects ordinary
  socket attachment as a negative control. No served source or responses are
  rewritten. Images and pixel metrics are saved under
  `apps/client-next/temp/artifacts/compound-hair/`.

The initial authenticated scratch-session attempt reached the world, but its GM
item command was refused and the potion wait timed out. It is not counted as a
successful live berserk test. The isolated renderer regression exercises the
reported visual defect without changing the shared server or its privileges.
