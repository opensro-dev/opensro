# Retired documents

Dated work reports: what one batch of fixes changed, on which branch, with
which PR open. They described the tree at the time and stopped being true
as it moved on; the decisions they record live in the code comments and
commit messages, and long native evidence belongs in `../research`. They
remain in git history: restore any of them with

```
git checkout 2371a6a -- <path>
```

| Path | Report |
| --- | --- |
| `docs/ABNORMAL_MECHANICS_AUDIT_2026-10-01.md` | Abnormal mechanics audit — 2026-10-01 |
| `docs/ABNORMAL_TOOLTIP_2026-10-01.md` | Abnormal status power, grade and tooltip ownership |
| `docs/ACTION_MOVEMENT_2026-10-01.md` | Moving out of auto-attack — 2026-10-01 |
| `docs/BERSERK_HAIR_2026-09-30.md` | BUG-046: berserk hair and independent compound attachments |
| `docs/BETA_BUG_BATCH_2026-09-28.md` | Beta bug batch, 2026-09-28 |
| `docs/FORTRESS_EVENT_COS_2026-10-01.md` | Fortress, event and COS extension — 2026-10-01 |
| `docs/GROUND_LOOT_LIFECYCLE_2026-09-30.md` | Ground loot lifecycle — BUG-047 |
| `docs/NPC_RESELECTION_2026-09-30.md` | BUG-042: reopening the same NPC |
| `docs/PROJECTILE_SOCKET_FALLBACK_2026-10-01.md` | Projectile socket fallback — 2026-10-01 |
| `docs/RECOVERY_GOLD_PARTY_2026-09-29.md` | Recovery consumables, merchant blessing and party selection |
| `docs/RESTORATION_TRAPS_2026-09-29.md` | BUG-037: restoration and remaining quest traps |
| `docs/ROGUE_SKILLS_2026-10-02.md` | Scorn and Poison Weapons — 2026-10-02 |
| `docs/SCROLL_CHAT_PICKUP_2026-09-29.md` | Scrolls, chat continuity and repeated pickup |
| `docs/SMALL_BUG_PASS_2026-09-29.md` | Discord audit and small UI/text repairs |
| `docs/TRACE_BUG_BATCH_2026-09-29.md` | Trace and shared native geometry, 2026-09-29 |
| `docs/WIZARD_SKILL_REVIEW_2026-10-02.md` | Wizard skill PR integration — 2026-10-02 |

Retired later; restore these from `8a456bb` the same way.

| Path | Report |
| --- | --- |
| `docs/sox-visual-parity.md` | SOX presentation: BUG-057 |
| `docs/native-third-party-review.md` | Third-party native review, captured revisions |
| `docs/COMPANION_PROTOCOL_RELEASE.md` | Companion protocol and authority release order |
| `docs/CAP110_UPSTREAM_HANDOFF.md` | Cap-110 upstream review handoff |
| `docs/CAP110_SKILL_ASSET_AUDIT.md` | Mastery 101-110 skill asset audit |
| `docs/releases/2026-10-13-f7.md` | F7 changes merged without a player note |
| `docs/GRAPHICS_UPGRADE_2026-10-05.md` | Folded into `docs/features/EXPERIMENTAL_GRAPHICS.md` |
| `docs/GRAPHICS_UPGRADE_2026-10-06.md` | Folded into `docs/features/EXPERIMENTAL_GRAPHICS.md` |
| `docs/GRAPHICS_UPGRADE_2026-10-08.md` | Folded into `docs/features/EXPERIMENTAL_GRAPHICS.md` |

## Second cut, 2026-11-01

Restore these with `git checkout d27317ea55 -- <path>`. Paths are as of that commit.

| Path | Now |
| --- | --- |
| `docs/features/CAP110_PROGRESS.md` | Rewritten as `docs/features/CAP110.md` |
| `docs/features/ATTACK-PET-EXPANSION-PLAN.md` | Current parts in `docs/pet-expansion/ATTACK-PET-SYSTEM.md` |
| `docs/pet-expansion/PET-GEAR-DEGREES.md`, `PET-GEAR-PROCS.md`, `PET-GEAR-SEALS.md` | Folded into `ATTACK-PET-SYSTEM.md` |
| `docs/pet-expansion/ASSET-PROVENANCE.md`, `PET-ITEM-ICONS.md` | Folded into `docs/pet-expansion/assets/README.md` |
| `docs/features/GRAB_PETS.md`, `PET_NAMEPLATES.md`, `TRAINING-DUMMY.md`, `BETA_MASTERY.md` | Folded into `docs/features/SMALL_FEATURES.md` |
| `docs/features/ALEXANDRIA_NPC_FIXES.md` | Finished bug report |
| `docs/pet-expansion/ALL-PET-SKILL-ACCEPTANCE.md` | Dated acceptance matrix |
| `docs/pet-expansion/CROW-REWORK.md`, `PENGUIN-REWORK.md`, `WOLF-REWORK.md` | Numbers live in `petdesign.json` |
| `docs/pet-expansion/PET-SKILL-UI.md`, `PET-LOOT-MODEL.md`, `PET-SURVIVABILITY-BALANCE.md` | Finished work notes |
| `docs/pet-expansion/REVIEW-2026-10-14.md`, `REVIEW-VALIDATION.md` | Dated reviews |
| Per-feature native evidence, and `docs/evidence/` | Moved to `../research/docs/port-native` |
