# Abnormal status power, grade and tooltip ownership

BUG-040 reported a Burn tooltip headed `Burn 38level`. The value 38 is not
evidence of an excessive character or skill level: the port placed the
elemental power byte in the grade suffix and omitted its native detail row.

## Native evidence

The v1.150 client instructions distinguish these fields:

| Owner | Evidence | Meaning |
| --- | --- | --- |
| Private status snapshot | `77C1D4` through `77C1FC` | Mask `0x203F` stores the byte as power/level; `0x017FCFC0` stores it as grade. |
| Local HUD heading | `6E488C`, `6E48AF` | Only slot `+0x4D`, the grade, receives the localized grade suffix. |
| Local HUD detail | `6E4A54` through `6E4AD7` | Positive slot `+0x4C` is displayed separately under `PARAM_POWER`. The shipped English text is **Effect**. |
| Local HUD duration | `6E4ADC` onward | The local status help includes the remaining time for finite effects. |
| Target/pet viewer | `6DF8AD` through `6DF8C4` | Look up the supplied character's record and copy its grade byte at `+2`. |
| Party viewer | `6DF8AB` | A missing character skips the grade lookup. |

The server's `abnormal.Record` already separates `Level` and `Grade`.
`playerAbnormalSnapshotPayload` sends the appropriate byte in the v1.150
`0x36C7` contract. `abnormalVitalsPayload` publishes the public grade list,
and the client `vitalsUpdate` parser reads it for the addressed actor.
The v1.188 server's `590680` roll and `4A5C60` snapshot also distinguish the
elemental level and grade fields; their opcode and widths are not substituted
for the v1.150 wire contract.

No damage, duration, resistance or cure formula needs to change to repair
this report. Reducing the server's value to make the tooltip look plausible
would change gameplay without evidence.

## Shared port correction

`buffTooltip` keeps power, grade and remaining time separate. The local
board uses its private snapshot. Target and pet viewers use the selected
actor's public grade, and party cells remain unlevelled. An explicit viewer
flag also preserves these rules when a viewer targets the local player.

The previous unconditional lookup in `game.abnormalRecords` let another
actor's tooltip borrow the local player's power or grade. The fix covers
every named status, rather than special-casing Burn or the reported value 38.
Cleared statuses produce no help, expired timers produce no remaining-time
line, and the native infinite status does not acquire a countdown.

The existing snapshot test previously required the incorrect heading
`PARAM_BU 6UIIT_STT_GRADE`; it now requires a Burn heading and a separate
power detail. Runtime regression coverage also includes remote players,
pets, party cells, a viewer of the local player, and every named power/grade
mask branch. The browser fixture uses the production HUD, asset worker and
WebGPU renderer with controlled status snapshots; it is not a production
combat retest.

## Verification and release

Focused tests: `abnormal-tooltip`, `abnormal-snapshot`, `buff-viewer`,
`vitals`, and the server's abnormal-roll and snapshot publication tests.
The browser test is `tests/browser/abnormal-tooltip.test.mjs`; its screenshots
and report are written to `apps/client-next/temp/artifacts/abnormal-tooltip/`.

This is a client presentation repair. A deployed client containing the fix
is required before BUG-040 can be marked Ready to test. Player verification
and closure remain separate from implementation and automated tests.
