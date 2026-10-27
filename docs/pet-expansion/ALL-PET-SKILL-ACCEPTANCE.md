# Pet skill acceptance matrix

All 60 books are tracked separately. "Prior live verified" means existing recorded gameplay evidence, not a new test in this sweep. Pending books are not accepted. Rank permutations and visual polish are separate from functional acceptance.

## Completed sweep — 2026-10-26

All four pets and all 60 books have recorded functional gameplay coverage. The table distinguishes tests performed in this sweep from earlier live results. Tests used authenticated native game commands against the running local server, tier-isolated level-90 test pets, real monsters and GM training dummies. Protocol evidence verifies health, status, mana, counters and reuse timers; it does not replace visual animation review.

- Wolf: Howl sharing, Rend/bleed, owner and pet Execute, seventh-hit sweep, Bloodscent and Blood Hunt splash.
- Bear: delayed Roar, shields, damage transfer, resurrection, Unbroken Spirit and incoming-hit stun counters.
- Penguin: Lance, four Blizzard pulses/final freeze, frozen-kill Shatter splash, resonance echoes and Hourglass recovery.
- Crow: Plume healing/cleanse, egg charges/hatch/clone expiry and wind burst, owner/party mana recovery, Tailwind and Guiding Feathers.

**Fixed:** periodic Blizzard damage omitted the pet codename and lost species/equipment scaling. A fixed-roll regression failed before the correction (312 initial versus 216 periodic damage) and passes afterward. Live pulses with the corrected build were 12,119 / 12,520 / 12,475 / 12,286, one second apart, with basic attacks stopped after the initial cast.

**Validation:** full Go server suite, action vet/lint, 40 focused client tests and the pet-design consistency check passed. Original character records, pets, equipment and GM configuration were restored after testing; temporary spawned actors were cleared by restart.

**Follow-up checks:** the full server suite and action vet/lint pass on the synchronized branch. All 82 pet client tests and 53 effect tests pass, including crow cue geometry, red/blue recovery effects and teardown. The clone wind-burst regression covers bare, D1, D9 and D9 Sun collars, with five-times scaling and exact yellow-damage/HP feedback. Native multi-impact attacks round each impact separately. The canonical pet-design export also passes consistency checking after the Hourglass text correction.

**Limits:** this is per-book functional coverage, not every rank, equipment combination or visual animation. Exact effect formulas also have regression tests; random live damage samples alone do not prove exact multipliers. Raw local captures and reversible fixtures are under `.state/all-books-20261025-*`; the table records their case labels.

| Book | Skill | Status | Evidence |
|---|---|---|---|
| 1 | Hunting Howl I | live verified | Live HowlI + early recast refusal; row5000ms at80%CDR; all-books-20261025-owner.jsonl labels wolf-I / wolf-I-full / wolf-I-weak-execute |
| 2 | Hunting Howl II | live verified | HowlII owner/wolf buff, native row5000ms and early-recast refusal; all-books-20261025-owner.jsonl labels wolf-II / wolf-II-owner / wolf-II-caps |
| 3 | Hunting Howl III | prior live verified | postmerge live: party HowlIII 500% and cooldown5000ms; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 4 | Savage Rend I | live verified | Native RendI and bleeding; BloodHunt consuming prey mark; all-books-20261025-owner.jsonl labels wolf-I / wolf-I-full / wolf-I-weak-execute |
| 5 | Savage Rend II | live verified | RendII native result hits3 grouped dummies; bleed native flags; all-books-20261025-owner.jsonl labels wolf-II / wolf-II-owner / wolf-II-caps |
| 6 | Savage Rend III | prior live verified | postmerge live: RendIII bleeding and mark consumption; splash recheck remains below; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 7 | Execute I | live verified | Live target467741 native ExecuteF0 and HP0 below threshold; all-books-20261025-owner.jsonl labels wolf-I / wolf-I-full / wolf-I-weak-execute |
| 8 | Execute II | live verified | wolf-II-owner-weak:13ownerbasiccasts, wolfidle; ExecuteF0 target467018 andHP0; no pet attacks during owner-only kill |
| 9 | Execute III | live verified | Party prepared nativeLion468067 to204/871HP without wolf/ownerhits; ownertargeteddummy467702; wolf-III-sweep-kill:ownerSweepF1 followedbyLionExecuteF0; selectedLionHP0; no direct owner/wolf attackonLion |
| 10 | Bloodscent I | live verified | Wolf Bloodscent cap5, ownercounter0 during real owner magic hit; all-books-20261025-owner.jsonl labels wolf-I / wolf-I-full / wolf-I-weak-execute |
| 11 | Bloodscent II | live verified | Separate live wolf and owner Bloodscent pools both reached8; all-books-20261025-owner.jsonl labels wolf-II / wolf-II-owner / wolf-II-caps |
| 12 | Bloodscent III | prior live verified | postmerge live: wolf/owner Bloodscent counters and stats at10 stacks; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 13 | Blood Hunt I | live verified | Four native hits prime mark; Rend consumes, HuntF2 feedback and HP debit; all-books-20261025-owner.jsonl labels wolf-I / wolf-I-full / wolf-I-weak-execute |
| 14 | Blood Hunt II | live verified | Owner basic attacks contribute to shared4hit prey mark; Rend consumes it with HuntF2 and realHPdebit; all-books-20261025-owner.jsonl labels wolf-II / wolf-II-owner / wolf-II-caps |
| 15 | Blood Hunt III | live verified | wolf-III: Huntmark4→RendIII→HuntF2, four bleeding dummy neighborsF3 and realHPrefresh; NativeprimaryRend5targets; survivingsecondaryHPseparatelyreduced bycreditedHuntsplash |
| 16 | Challenging Roar I | live verified | LiveRoarIaggression after1second release caused3nativelevel40RedScorpiongiants to attackbear; earlyrecast refusal; bear-I-body-reset / bear-I-native-combat native logs |
| 17 | Challenging Roar II | live verified | RoarII tauntedthree nativeRedScorpiongiants, followedbybear-targeted attack244/245; bear-II / bear-II-owner-party native logs |
| 18 | Challenging Roar III | prior live verified | postmerge live: RoarIII taunt, delay, early recast denial; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 19 | Iron Hide I | live verified | bear-I:shieldonlybear33554434andowner100002;no partyshield;5000ms nativeframe; Native4200ms rowCD at80%CDR, earlyrecast refused, expiry0withoutburst |
| 20 | Iron Hide II | live verified | IronHideII native5000ms shields bear+owner+nearbyjoinedparty100004; noexpiryburst; bear-II / bear-II-owner-party native logs |
| 21 | Iron Hide III | prior live verified | postmerge/Jangan live: bear+owner+party shields5000ms and expiry/explosion flags; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 22 | Unbroken Spirit I | live verified | ResolveI reaches10; maxHP258500to310200(+20%), newlygainedHPfilled; no stackspend onRoar; bear-I-body-reset / bear-I-native-combat native logs |
| 23 | Unbroken Spirit II | live verified | ResolveII cap10, maxHP258500to336050(+30%); noactiveconsumption; bear-II / bear-II-owner-party native logs |
| 24 | Unbroken Spirit III | prior live verified | postmerge live: Resolve10 and increased HP; empowered active consumption; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 25 | Guardian's Bond I | prior live verified | expiry-bond-cleanse live: rankI1 revives owner10%HP/10%EXP refund,600000ms cooldown; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 26 | Guardian's Bond II | live verified | LiveBearII ownerdamage routedtobear viaBondkind1; inheritedrankI10revival alsofired afterrealpoisondeath and restoredHP; bear-II-owner-party / bear-II-bond-counter native logs |
| 27 | Guardian's Bond III | prior live verified | expiry-bond-cleanse live: full owner/party HP+EXP restoration, shared300000ms cooldown, pet+10level boundary; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 28 | Crushing Paw I | live verified | Independentbear10hitcounter resets; attackers receive nativeStun16384; ownerhitscounter0; bear-I-body-reset / bear-I-native-combat native logs |
| 29 | Crushing Paw II | live verified | Ownercounterreached9thenreset; sourceScorpion469121receivednativeStun16384 twice; independentbearcounteralsoadvanced; bear-II-owner-party / bear-II-bond-counter native logs |
| 30 | Crushing Paw III | prior live verified | postmerge live: independent owner/bear10-hit stun counters, multiple targets/ripple; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 31 | Ice Lance I | live verified | NativeLanceI single-targetmagicprojectile; six casts in complete label; penguin-I-fixed / penguin-I-owner-excluded / penguin-I-complete |
| 32 | Ice Lance II | live verified | LanceII native result hits3aligneddummies; Frostbite2 applied; penguin-II / penguin-II-owner-stacks native logs |
| 33 | Ice Lance III | prior live verified | postmerge live: IceLanceIII cast and cold status; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 34 | Blizzard I | live verified | Fixedbuild131 isolatedBlizzardI initial12119+12520+12475+12286 damage exactly1s apart; nofreeze; penguin-I-fixed / penguin-I-owner-excluded / penguin-I-complete |
| 35 | Blizzard II | live verified | BlizzardII four pulses applyslow2withoutfinalfreeze1; normal4000msrow; penguin-II / penguin-II-owner-stacks native logs |
| 36 | Blizzard III | prior live verified | postmerge live: four isolated storm pulses, final freeze; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 37 | Shatter I | live verified | penguin-I-shatter-cold: nativeFrostbite2confirmed thenmultipleLance/BlizzardhitswhilechilledwithhighestpassiveI; Directdamagerose37123to41297/51514/54818withcold+Resonance; exactShatter20%isolatedinGo |
| 38 | Shatter II | live verified | OwnernativeLightninghitsagainstBlizzard-chilleddummy withShatterII active; exactbonuscoveredsharedformula regression; penguin-II / penguin-II-owner-stacks native logs |
| 39 | Shatter III | live verified | penguin-III-shatter-small: nativeScorp469357 frozenat25405HP, LanceIII fatal53179beforeGMcleanup; Two nearbydummies469355/469356 emittedD6 andreceivedseparateShardHPdebits |
| 40 | Arcane Resonance I | live verified | PenguinIResonance reached5; ownermagicdoesnotgainownerpool; penguin-I-fixed / penguin-I-owner-excluded / penguin-I-complete |
| 41 | Arcane Resonance II | live verified | Alternating1316/1350nativeownerhitsbuildindependentResonance5whilepetpool1; penguin-II / penguin-II-owner-stacks native logs |
| 42 | Arcane Resonance III | prior live verified | postmerge live: full5stacks original+echo distinct cast IDs; no recursive stacks; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 43 | Frozen Hourglass I | live verified | HourglassI reaches5, chargedBlizzardI row2000ms vs4000uncharged; penguin-I-fixed / penguin-I-owner-excluded / penguin-I-complete |
| 44 | Frozen Hourglass II | live verified | OwnerhitsbuildHourglass5; nextBlizzardII consumescharges androw4000to2000ms; penguin-II / penguin-II-owner-stacks native logs |
| 45 | Frozen Hourglass III | prior live verified | postmerge live: charged Blizzard cooldown4000to2000ms, ownercounter exercised; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 46 | Rejuvenating Plume I | live verified | Owner received immediate healing and five HoT pulses; the crow and nearby party did not heal.; all-books-20261025-owner.jsonl: crow-I-auto; party observer log |
| 47 | Rejuvenating Plume II | prior live verified | expiry-bond-cleanse live: native poison16to0 within3ms; owner+crow healing; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 48 | Rejuvenating Plume III | prior live verified | expiry-bond-cleanse live: burn8to0 within2ms; earlier partyHP+HoT live; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 49 | Stormbrood I | live verified | One charge spent, immediate second cast refused, one egg hatched after 30 seconds.; all-books-20261025-owner.jsonl: crow-I-auto; party observer log |
| 50 | Stormbrood II | live verified | Two stored-charge casts produced separate eggs and 30-second hatches; empty-charge recasts were refused and recharge became available.; all-books-20261025-owner.jsonl: crow-II-final; party observer log |
| 51 | Stormbrood III | prior live verified | wind-final live proof19780HPdamage,600191ms; prior ten-clone cap and charge limit; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 52 | Mana Spring I | live verified | Seven bird hits restored 10% actual damage as owner MP; party received only ordinary regeneration.; all-books-20261025-owner.jsonl: crow-I-auto; party observer log |
| 53 | Mana Spring II | live verified | Seven crow attacks restored 10% actual damage as MP to both owner and joined nearby party member.; all-books-20261025-owner.jsonl: crow-II-final; party observer log |
| 54 | Mana Spring III | prior live verified | postmerge live: owner/party MP and21party cooldown reductions; pet cooldown recovery; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 55 | Tailwind I | live verified | Reached 10 stacks / +50% movement; owner damage cleared all stacks without stunning.; all-books-20261025-owner.jsonl: crow-I-auto; party observer log |
| 56 | Tailwind II | live verified | Ten stacks granted +50% movement; owner hit cleared stacks and stunned the target (native status 16384).; all-books-20261025-owner.jsonl: crow-II-final; party observer log |
| 57 | Tailwind III | prior live verified | postmerge live: owner/party10stacks+50%speed, clears on damage; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
| 58 | Guiding Feathers I | live verified | Bird hits built five owner accuracy stacks; owner spell did not consume them; they expired normally.; all-books-20261025-owner.jsonl: crow-I-auto; party observer log |
| 59 | Guiding Feathers II | live verified | Crow attacks built five owner accuracy stacks; the next owner spell consumed them before expiry.; all-books-20261025-owner.jsonl: crow-II-final; party observer log |
| 60 | Guiding Feathers III | prior live verified | postmerge live: owner+party independent Precision pools reach5 and spend; .state/PET-ELEMENT-SEALS-HANDOFF.md and linked proof logs |
