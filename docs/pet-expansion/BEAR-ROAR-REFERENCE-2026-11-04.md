# Bear roar reference pass — 2026-11-04

Port-only Challenging Roar, all three books. The existing bear mesh remains in use.
Reference: the owner's six-panel Bear Roar Animation Concept storyboard.

- Crouch at 0.10 s; rear up by 0.25 s; open-mouth release and positional sound at 0.50 s.
- Three narrow warm-gold ground rings expand to the skill radius; primary reaches full extent at 1.00 s.
- Soft tan dust and sparse olive leaves travel outward without growing the individual particles.
- Muzzle ripples follow the retail head socket's local X axis. Recovery ends at 1.40 s.
- Removed the enclosing cocoon, oversized blast walls, bear enlargement and camera shake.
- Server taunt delay (1 s), range calculation, cooldown and victim selection were not changed.
- Iron Hide still uses its existing blast geometry; retaliation keeps its existing ripple.

Verification: 43 focused bear/character-effect tests pass; client typecheck, ownership,
capabilities, execution-flow, test-type and changed-file formatting checks pass.
Both retail bear rigs keep their hind paws planted and return to neutral.
Live casts observed from another player outside Jangan: rear-up, muzzle rings,
expanding rings, transparent dust and recovery. Fixed rectangular dust cards found
in the first live pass and grounded/thinned the rings in subsequent passes.

The full client check is not green: missing extracted retail resinfo/textdata/BSR
inputs and unrelated catalogue assertions prevent the complete suite passing here.
No numerical visual-similarity score is claimed; final appearance remains subjective.

All five original character records and ground items were compared exactly with
pre-test backups after restoration. Original Nomad jobs restored; no GM granted.
No archive files, gameplay stats or other pets' clips changed.

Focused verification:
`node --test apps/client-next/tests/runtime/bear-roar.test.mjs apps/client-next/tests/runtime/character-effects.test.mjs`
