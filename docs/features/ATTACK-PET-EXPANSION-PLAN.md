# Attack pet expansion — four pet skill trees and dropped gear with blues

Updated 2026-10-14. Owner-approved port-only feature, not original Silkroad rules.
Server flag `SRO_ATTACK_PET_EXPANSION=on`; deployment default is **off** (native).

## The design lives in one JSON source

[`apps/server/internal/game/companion/petdesign.json`](../../apps/server/internal/game/companion/petdesign.json)
is the one source of every number: pets, skills, rows, cooldowns, gear degrees, affixes (blues),
procs. Edit it, then run

```
py scripts/tools/pet_design_export.py
```

which validates it, rewrites it in its canonical layout and generates the client
module (`apps/client-next/src/engine/foundation/ui/pet-design-data.ts`). The server
embeds the file and the asset build reads it in place; `pnpm task check:pet-design`
fails when the client module no longer matches. Only rows whose
**Status** is `Implemented` can be learned or rolled; the engine notes column says
what each rank does in code. Rebuild game data after changing gear rows.

## Delivered

- **Four attack pets**: bear (defense, `COS_P_BEAR*`), wolf (attack, `COS_P_WOLF*`),
  penguin (magic/cold, `COS_P_PENGUIN*`), crow (support, `COS_P_RAVEN*`).
- **Five rows per pet**: two action rows and three passive rows. The owner uses an
  action (click it in the Skills tab, also in combat); the pet performs it on its
  next attack once the row is ready, and clicking again cancels. Actions in a row
  share one cooldown, which can be a timer, attacks made, hits taken, kills or
  spells. One passive per row is slotted (click to swap). Skills gain Rank II/III
  behaviour at the workbook's rank levels.
- **Points**: 1 at pet level 1, one more per `PointsPerPetLevels` levels; each skill
  level also needs a pet level. Reset is free.
- **Gear**: one collar/harness/charm family for all pets, degrees 1–9, monster drops
  only (chance by monster rarity, degree by monster level, uniques one higher).
  Normal / magic (1–2 blues) / rare (3–5 blues), one blue per group, tiers gated by
  degree: damage, crit, elemental hits, leech, crushing/deadly/pierce, attack speed,
  +skill levels, cooldown, find, and procs (chain lightning when hit, fire, mend).
  Blues travel as native-style magic options (param `0x8000 + affix code`).
- **Pet window**: Inventory tab shows the 3D pet between its three gear sockets
  (drag from bag to wear, right-click/drag to remove, a second piece swaps);
  Skills tab uses the character skill window's rows, bar and buttons.
- Effects use native abnormal states (fear, stun, bleed, frostbite, freeze, shock,
  myopia, decay, weaken) and the existing pet area-attack owner.

Wire: state push 0x3FB1; command = normal COS request 0x769E, tag F0,
`[petGID:u32le][F0][op][arg]` with ops 0 state, 1 learn, 2 use (queue an action), 3 wear bag
slot, 4 remove gear slot, 5 reset, 6 slot passive. Retail decoder unchanged.

## Rollout

1. `pnpm task build server-game-data`, then the full asset build (27 gear item
   rows, names and tooltips).
2. Maintenance release: store schema 28 (training + worn gear + affix rules are
   persisted fields); stop the shard, run `sro-authority-upgrade -commit` (keeps a backup).
3. Deploy GameWorld and client with `SRO_ATTACK_PET_EXPANSION=on`
   (Nomad variable `attack_pet_expansion`; deployment default remains `off`).
4. Rollback: redeploy with the flag off. Saved training and worn gear persist;
   no new gear drops; gear already in bags stays as a plain item.

## Verification status

- [x] companion tests: design coverage, points/levels/slots, shared row cooldowns
  and counters, gear rolls (rarity, blue count, groups, degree, slot), modifiers.
- [x] action tests: admission/flag/combat refusal, gear keeps blues (wear, swap,
  remove), wolf bleed + row cooldown, bear stun, Execute/Shatter damage, monster
  drops, gear item body carries options, receipt chat limit.
- [x] client tests: v2 state decoder, help text, affix tooltip lines, op bounds.
- [ ] Browser acceptance on the lab stack (all four pets, LabBlader).
- [ ] Balance measurement against the unmodified baseline.

Crow Mend's owner heal needs real owner combat stats, so it is checked only in the
running stack.

## Still to do

**Skills not live yet** (workbook Status `Needs engine` / `Planned`, 24 of 59):
- Wolf: Alpha's Lure (taunt), Pack Runner.
- Bear: Challenging Roar (taunt), Bear Hug, Guardian Bond (damage share),
  Fortitude, Thorns, Unshakable, Regeneration, Bodyguard (redirect), Steadfast Aura.
- Penguin: Blizzard, Ice Slide, Glacial Ward, Mana Spring, Mana Shield, Winter's Grace.
- Crow: Cleansing Caw, Battle Cry, Thieving Talons, Scout's Eye, Feather Ward,
  Fair Winds, Ill Omen, Spirit Link.
- "Needs engine" ones need new server mechanics first: monster taunt/aggro
  override, damage redirection owner <-> pet, owner buffs from the pet.
- Many live skills have Rank II/III marked planned (see the Engine notes column),
  e.g. Hamstring slow, Frenzy/Execute rank III, every stat passive's ranks.

**Blues not live yet** (Planned): pet HP, regen, resist, move speed, owner move
speed, hunger, pet EXP, +one skill, thorns, steady (no knockdown), phoenix
(revive), and procs nova/howl/poison/corpse.

**Engine/quality gaps**
- `wearPetGear` does not check `GrantStack`'s remainder when returning the
  previous piece to a full bag (refuse instead of losing it).
- Crow Mend's owner heal has no automated test (fixture lacks owner stats).
- Values per level can show fractions ("stuns up to 2.4 monsters"): round
  counts in the workbook or in `petEffect`.
- No new animation clips or effects: pets reuse their native attack clip, so
  specials are only visible through status icons and damage numbers.
- Actions can only be used from the Skills tab: no quickslot/hotkey for pet
  actions yet (drag to the skill bar would be the native-feeling way).
- PvP: effects are PvE only.
- Drop rates and numbers are unmeasured: balance pass against the native
  baseline (time-to-kill, pet survival, drop frequency per hour).
- Browser acceptance still open for penguin, crow and wolf combat, gear swap
  and relog persistence, and drops from real monsters.

**Rollout**
- Review the branch, then the maintenance release (schema 28 upgrade) and Nomad
  variable `attack_pet_expansion` (default `off`) as listed under Rollout.
- The lab-only LabBlader changes (level 40, pets, gear) are not part of the code
  and must not be applied to real shards.

## Source owners

- `apps/server/internal/game/companion/petdesign.json` + `scripts/tools/pet_design_export.py`: design and its client export.
- `apps/server/internal/game/companion/`: `design.go` (embedded design),
  `training.go` (points, rows, slots), `gear.go` (rolls, affix encoding, modifiers).
- `apps/server/internal/domain/character.go`: persisted `PetTraining`.
- `apps/server/internal/game/action/`: `pettraining.go` (commands, gear moves),
  `pettrained.go`, `petmanual.go`, `petshield.go`, `petspirit.go`, `petbond.go` (combat effects),
  `petgeardrop.go` (drops), `petcombat.go`, `monsterstrike.go`, `monsterloot.go` (existing owners hooked).
- `item/wire/pettraining.go`, `itemmove.go`: state frame and gear options on items.
- `scripts/build/shared/portItemRows.mjs`: gear item rows and text.
- Client: worker `pet-training.ts`, `foundation/ui/pet-training.ts`, `ui.ts`
  pet window, tooltips.

## Local deployment

Deploy the local Nomad stack from `apps/server` with `SRO_ATTACK_PET_EXPANSION=on`
in the local lab environment only (default `off`). Stop the local cluster before any
offline authority DB edit.
Browser game: [local client](http://127.0.0.1:5180/).
