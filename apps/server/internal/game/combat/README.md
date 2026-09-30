# Combat damage and equipment parameters

The combat projection is shared by login, equipment changes, the character
pane, incoming attacks, outgoing attacks and effect refreshes. `stats.go`
constructs one ParamKeeper graph from the current authoritative snapshot.
Equipment coefficients belong in that graph, not in a weapon-specific damage
formula or UI correction.

## Hit magnitude and HP debit

A committed hit has two distinct quantities:

- `MonsterDamageResult.Damage`: the full accepted hit used by skill feedback.
- `MonsterDamageResult.Applied`: the bounded HP debit used for bookkeeping.

A 1000-damage hit against 55 HP reports 1000 and leaves zero HP. The fatal flag
belongs to the one transition from positive HP to zero. The population owner
retains the corpse until its explicit lifecycle removal. Subsequent sequence
impacts stop at the fatal hit; area packets preserve their authored skipped rows.

`committedSkillImpact` is shared by immediate, area, multi-impact, projectile
release and persistent player attacks. Incoming monster attacks preserve the
same distinction inside the character mutation door. COS hit feedback already
retains full hit magnitude. Wall absorption remains its own result kind. Periodic
abnormal notifications and authoritative HP refreshes retain their separate
contracts. Hostility ignores fatal sequences, so surviving hit debit equals its
hit magnitude. Contribution credit already receives the original accepted hit.

The browser decodes full damage, bounds local HP independently and forwards the
original impact to animation feedback. It needs no compensating display change.
The packet layout and protocol remain unchanged.

Native server instruction evidence:

- `0x585664..0x58567e`: load hit-record `+0x20`, shift it eight bits, combine the
  result flags and write the packed value. No HP clamp appears here.
- `0x590590..0x5905a4`: accumulate hit magnitude in the target result, compare
  against current HP, and set the independent fatal flag.
- `0x593800`: apply the target result through the victim's hit dispatcher. Its
  fatal path can debit current HP without rewriting the serialized hit record.

This supersedes older notes that describe the wire damage as `Applied`.

## Equipment reinforcement

`itemrefs.go` admits the full v1.150 reinforcement columns: armor/shield 82–85,
weapon physical 105–108, and weapon magical 109–112. Their permille values are
parsed into float32 fractions. Invalid cells invalidate the combat reference.
The v1.150 table has no reinforcement-per-plus columns; later-version columns
are not invented.

`equipmentreinforcement.go` routes the coefficients into the existing graph:

| Equipment | Keeper parameters | Variance lanes |
| --- | --- | --- |
| Weapon physical minimum/maximum | 34, 36 | 1 |
| Weapon magical minimum/maximum | 35, 37 | 2 |
| Armor physical/magical, part 1–6 | 38–43, 44–49 | 1, 2 |
| Shield physical/magical | 58, 59 | 1, 2 |

These replace source-zero base coefficients. Independent item stats and magic
options retain their existing source identities. The existing keeper owns
zero-write admission and propagation. Broken or unequipped items contribute
nothing when a new snapshot is built, restoring the base graph.

Native server `0x497830` owns equipment insertion; the weapon instructions at
`0x497b84..0x497c1b` show float32 percentage conversion and source-zero writes.
`0x495d10` spills the normalized variance to float32, and `0x4961b9..0x496325`
shows reinforcement interpolation spills. The armor and shield insertion
branches use their own parameter ranges. **Port inference:** normalize v1.150
fractional armor references to the percentage units of the initialized keeper
graph, matching its existing unarmored coefficients. This adaptation is stated
in the implementation rather than presented as a captured v1.150 server result.

## Regression coverage

- `action/overkill_test.go`: reproduces 1000 becoming 55 and an incoming 16
  becoming 1 before the correction; checks committed feedback afterward.
- Existing action tests retain fatal lifecycle, area falloff, multi-impact and
  critical-result assertions with HP debit checked separately.
- `item/wire/actionresultfixture_test.go`: generates the shared nonfatal and
  overkill packets from the simulation owner.
- Browser `combat-overkill.test.mjs`: consumes those packets through decoding,
  HP and animation-feedback owners and checks that feedback is not duplicated.
- `equipmentreinforcement_test.go`: all weapon subclasses through 16, Chinese
  and European armor parts, both shield subtypes, independent variance lanes,
  equipment lifetime, complete-set composition and independent effect writes.
- The reported `ITEM_CH_SPEAR_06_C` is checked through the production text
  reader. At level 50, STR 69, INT 216, +30 and zero variance, its projected
  attacks are 668.304–729.999 physical and 1361.184–1529.360 magical. These are
  attack parameters, not promised final damage against every target.

The two unnamed exception helpers encountered while tracing result serialization
were labeled `EH_std_list_invalid_argument_SecurityCookieHandler` (`0xa53348`)
and `EH_std_list_out_of_range_SecurityCookieHandler` (`0xa53378`). Both labels
were saved and read back from database snapshot 269.
