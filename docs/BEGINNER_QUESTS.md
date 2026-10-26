# Beginner quests through level 64

Port-only, not native; owner-approved in the beginner quest design discussion.
Set `SRO_BEGINNER_QUESTS=on` on the GameWorld and restart to enable the track.
The Nomad setting is `beginner_quests`; its default is `off`.
Build and publish the resource assets with the matching server change: the
quest-data asset supplies the journal, objectives and NPC dialogue strings.

This release requires authority schema 32, even when the switch is off.
With the GameWorld stopped, use the existing offline
`sro-authority-upgrade -authority-dir PATH` validation, then repeat with
`-commit` to retain a backup and upgrade before starting the new binary.
The schema 31 step preserves existing records and treats missing binding
flags as false. No live database is upgraded by server startup.

Every character has thirteen once-only milestones. Chinese and European
characters have separate hunting routes, with the same rewards. A weapon
quest offers all five Chinese and nine European weapon families, regardless
of the character's country. Shields are not weapon rewards.

## Milestones

| Level | Kills | Chinese route | European route | Item reward |
| --- | --- | --- | --- | --- |
| 8 | 30 | Water Ghost | Barus Wolf | One chosen degree 2 Moon weapon |
| 10 | 20 | Yeoha | Barpolle | 50 HP and 50 MP potions |
| 16 | 40 | Bandit | Simos Ladon | One chosen degree 3 Moon weapon |
| 20 | 30 | Chakji | Seiren | 75 HP and 75 MP potions |
| 24 | 50 | Hyungno Ghost soldier | Crab | One chosen degree 4 Moon weapon |
| 30 | 40 | Hyeongcheon | Punisher | 100 HP and 100 MP potions |
| 32 | 60 | Scorpion | Periton | One chosen degree 5 Moon weapon |
| 40 | 50 | Red Scorpion | Rock Golem | 125 HP and 125 MP potions |
| 42 | 70 | Red-eye Ghost | Red-eye Ghost | One chosen degree 6 Moon weapon |
| 50 | 60 | Mujigi | Mujigi | 150 HP and 150 MP potions |
| 52 | 80 | Ishade | Ishade | One chosen degree 7 Moon weapon |
| 60 | 70 | Yeti | Yeti | 200 HP and 200 MP potions |
| 64 | 100 | Edimmu | Edimmu | One chosen degree 8 Moon weapon |

The potion merchant both offers and completes each quest. Levels 8-20 use
Jangan or Constantinople; levels 24-40 use Donwhang or Samarkand; levels
42-64 use Hotan. Both the regular and field-clone monster identities count.
Native fatal-hit quest attribution supplies progress; these quests add no
new party-credit rule. An unrelated kill never counts.

Each milestone requires its listed level. There are no earlier-quest
prerequisites, and higher-level characters can catch up missed milestones.
The native tutorials are unchanged. The native quest guide is unchanged;
custom quests are discovered at the potion NPC and use the normal journal
and tracker after acceptance.

## Rewards and binding

All milestones also grant the EXP and skill EXP listed in the
[content manifest](../apps/server/internal/game/quest/beginner_quests.json).
These values and the hunt counts are initial custom balance choices.
Support rewards use progressively larger native HP/MP potion grades.
There are no gold rewards. Weapons are fresh, unenhanced native Moon items;
their item references, degree and seal are validated at definition loading.

Every item reward is permanently character-bound. The persisted
`characterBound` inventory field applies to the individual item or stack,
not to every item sharing the reference. Binding survives save/load,
equipment moves, reinforcement and stack splits. Bound consumables merge
only with bound stock; ordinary stock remains ordinary. Dragging between
bound and ordinary stacks of the same potion is refused, so the native
client never infers a merge that the server did not perform. Weapon reward
confirmation names the exact degree, seal and weapon family selected.

Bound rewards cannot be traded, listed or sold through stalls, sold to NPCs,
dropped manually or on PK death, or transferred to account/guild storage or
companion containers. A bound weapon cannot be dissolved into transferable
materials. Normal item use and equipment upgrades remain available.

Reward inventory, EXP/SP and quest completion use the existing character
transaction. A full bag pays nothing and leaves the quest ready to report.
An invalid choice, wrong NPC, incomplete objective or replay pays nothing.

Turning the feature off removes its offers and executable definitions.
Already earned rewards retain their binding, and completed IDs remain saved
so re-enabling the track does not reset claims. The client retains display
metadata for saved custom journal entries while the feature is off.

## Ownership

- [Content manifest](../apps/server/internal/game/quest/beginner_quests.json):
  levels, targets, reward amounts and weapon families.
- [Server registration](../apps/server/internal/game/quest/beginner.go):
  optional identities and mechanics, using the standard quest loader.
- [Asset projection](../scripts/build/shared/beginnerQuests.mjs): additive
  journal and dialogue text from that same manifest.
- [Inventory persistence](../apps/server/internal/domain/character.go) and
  [row bridge](../apps/server/internal/game/action/characterrows.go): durable
  item-instance binding.

The quest IDs are reserved at 60000 + level for Chinese routes and
60100 + level for European routes. Never change an existing milestone's ID:
persisted active and completed records use it as their identity.
