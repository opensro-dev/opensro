# Public pet buffs and Guardian Bond: live acceptance

Experimental attack-pet expansion enabled; independent observer LabPetTest,
source BondOwner and party recipient BondParty. This records the November 2
live captures reviewed and cleaned up on November 4, 2026.

| Check | Observed result |
| --- | --- |
| Bond shared damage | A 991-damage hit split into 793 owner damage and 198 bear damage; the observer saw a yellow 198 above the bear. |
| Tailwind | Selecting another player showed its stack icon under the target panel, including the full-stack ready label. |
| Guiding Feathers | Normal crow attacks on a training dummy built Precision to 5/5; the observer saw it beside Tailwind. After attacks stopped, Guidance expired independently. |
| Hunting Howl III | The observer selected party recipient BondParty before the cast. The icon appeared during the eight-second buff and disappeared without retargeting. |

Howl was absent in capture frames 0–9, present in frames 10–25 (7.3 seconds
between sampled endpoints), and absent in frames 26–43. No icon or expiry
failure reproduced. The caster's own selected-player view was not separately
captured. Attack-speed mechanics were outside this visual check.

Fixed the garbled separator in Tailwind's ready label. Automated regressions
cover recipient isolation, expiry, clearing, and yellow paid-damage feedback
without a second HP debit.

All five character records were restored byte-for-byte from the pre-test
backup; database integrity passed. Temporary fixtures and captures remain
local and are not included in Git. No test GM access remains.

Protocol 12 requires a coordinated client/server release: older strict pet
state and Bond decoders cannot accept the expanded payloads. Existing
protocol-11 servers must be updated with the matching client.
