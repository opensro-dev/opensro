# Pet owner nameplates

Every companion whose spawn carries an owner name (attack and loot pets,
trade transports, guild companions) shows that name on a separate, centered
row 20 pixels above its own name, while the Options window's "Monster
names" box is on. With the box off the row is hidden, even on a hovered
pet. The row also follows the pet's name visibility (distance, hidden pets,
ridden companions). Pet status bars stack above the owner row, and drop
back to their usual height when the row is hidden.

The row copies the visible owner's current name color, including party and
special character colors. If the owner is outside the visible entity set,
the spawn's owner name still appears in white. The pet's own name retains
its existing color. Joining, leaving, or dissolving a party updates the
label through the existing entity snapshots; no server or protocol change
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
| The owner row needs the "Monster names" option | `855C51–855C67` leave the COS pass for byte `+0xE` of the option block (`4B3890`) at 0; byte 14 is `UIIT_STT_MONSTER_SIGN` (`game-options.ts`) | disassembly read | verified |
| The status gauge rises only with the owner row | `855CDA` stores the lifted y in `+0x4E4` before the base pass 4 (`85FA80` → `85F660`) subtracts its own 25.0 (`C461B8`) for the gauge; all three gates above skip the lift | disassembly read | verified |
| The owner row is 20 pixels above the name | `855CBC–855CDA` subtracts the double at `C46198`; bytes `00000000 00003440` encode 20.0 | disassembly read | verified |
| The owner row inherits the owner's name color | `854B3F–854B68` resolves owner GID at `+0x774`, loads owner color at `+0x134`, and calls `781780` on the board at `+0x794`; `781780–781793` writes that color to its color fields | disassembly read | verified |
| With no visible owner the owner row is white | `854B52` jumps to `854BD1` when the owner lookup fails; `854C0E–854C16` pushes `0xFFFFFFFF` to `781780` | disassembly read | verified |
| Guild soldiers take the owner's guild label color and keep their color when the owner is absent | `854B95–854C02` gates the tid word to low byte `0xC6` in band 5 (the server's `MercenaryBand`; every reference carries `0x29C6`), `854B54..854BC5` reads `[[owner+0x788]+0x20]+0x10` (`8687E0`); `854C0C` skips the white | disassembly read | ported (#1502): owner row takes the owner's guild row colour, absent owner keeps the soldier's own spawn colour |

`tests/runtime/pet-owner-nameplate.test.mjs` checks the published glyphs for
attack and loot pets, independent centering, owner colors, party changes,
an absent owner, the guild soldier's guild label colour (owner visible) and
own colour (owner absent), the "Monster names" gate, range/hover/riding
gates, and status-row spacing.
