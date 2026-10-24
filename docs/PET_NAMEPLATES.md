# Pet owner nameplates

Every companion whose spawn carries an owner name (attack and loot pets,
trade transports, guild companions) shows that name on a separate, centered
row 20 pixels above its own name. The owner row follows the pet's name
visibility, including distance, hover, hidden pets, and ridden companions.
Pet status bars stack above the owner row.

The row copies the visible owner's current name color, including party and
special character colors. If the owner is outside the visible entity set,
the spawn's owner name still appears; the current party roster supplies
party green, otherwise the label is white. The pet's own name retains its
existing color. Joining, leaving, or dissolving a party updates the label
through the existing entity/social snapshots; no server or protocol change
is required.

## Native evidence

Checked against the maintainer-supplied v1.150 `SRO_Client.exe`, SHA-256
`f6580bf65213c0c074efe312514c55c73040667e7a9ad1aae9b8595d48ccd3a2`.
Addresses below are preferred virtual addresses. Instructions were read with
`objdump -d -Mintel --start-address=... --stop-address=...`; the constant was
read with `objdump -s`.

| Claim | Native evidence | Checked by | Status |
| --- | --- | --- | --- |
| COS has a separate owner text board | `85548F–85549A` supplies owner text to the board at `+0x794`; `855CBC–855CE0` draws that board | disassembly read | verified |
| The owner row is drawn whenever the owner name is non-empty, for every COS band | `855CB3` skips the draw only when `[+0x78C]` is 0; `+0x778` is the owner `std::string` (reset at `854D8E`: capacity 15, size `+0x78C` 0) | disassembly read | verified |
| The owner row is 20 pixels above the name | `855CBC–855CDA` subtracts the double at `C46198`; bytes `00000000 00003440` encode 20.0 | disassembly read | verified |
| The owner row inherits the owner's name color | `854B3F–854B68` resolves owner GID at `+0x774`, loads owner color at `+0x134`, and calls `781780` on the board at `+0x794`; `781780–781793` writes that color to its color fields | disassembly read | verified |
| A missing visible owner uses the spawn name and current party membership for color | Fallback in `companionOwnerNameplate`, explicitly marked as inferred | inference (stated in a code comment) | inferred |

`tests/runtime/pet-owner-nameplate.test.mjs` checks the published glyphs for
attack and loot pets, independent centering, owner colors, party changes,
an absent owner, range/hover/riding gates, and status-row spacing.
