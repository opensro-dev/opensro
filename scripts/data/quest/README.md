# v1.150 compiled quest evidence

Ninety-one v1.150 quests are hand-written C++ classes in the v1.188
`SR_GameServer`, not Lua scripts, so the script-backed catalog
(`apps/server/internal/game/quest/catalog_generated.json`) never covered them.
`generate_compiled_quests.py` compiles these committed snapshots into
`apps/server/internal/game/quest/.generated/`; the server embeds the result.
`pnpm task check:compiled-quests` checks byte-for-byte regeneration.

## Inputs and reproduction

- `compiled-quests-source.json`: each class's initializer (vtable +0xDC),
  decoded from a Binary Ninja HLIL dump: the quest object's word writes and
  string lists, every mission object's field writes, and the vtable slots the
  class overrides beyond its destructor and initializer.
- `v150-text-source.json`: from the v1.150 `textquest.txt`, each mission's
  objective line (`SN_CON_*`) with the count it asks for, and each quest's
  advertised reward paragraph (`SN_PAYCON_*`).
- `sql-rewards-source.json`: the newer server's `refqusetreward` and
  `refquestrewarditems` rows for the same quests.

```text
python -B scripts/build/import_compiled_quest_evidence.py --hlil <dump> --methods <dump>
python -B scripts/build/import_quest_text_evidence.py --textdata <v1.150-textdata> --sql <SR_GameRefData>
python -B scripts/build/generate_compiled_quests.py
python -B scripts/build/generate_compiled_quests.py --check
```

## What each source decides

The class supplies the structure: NPCs, dialogue symbols, prerequisites, the
completion limit, and each mission's kind, NPC, item, monsters and drop chance.
v1.188 rebalanced counts and rewards, so the v1.150 text wins wherever it names
them: an objective line's count, and the popup's advertised EXP, skill EXP and
gold. The SQL rows supply the reward items, and the scalars of a quest whose
popup advertises none, as they did for the script-backed `QNO_RM_OLDWOMAN_1`.

A class that overrides vtable slots has custom behaviour (NPC talk at +0x58,
dialogue at +0x90, the capture escort's +0x80/+0x9C/+0xA8 timers and events).
The generator projects it only once that behaviour has a server owner, and a
quest waits while a prerequisite is unprojected. `compiled_quests_audit.json`
names every class still pending and why.

Mission field notes: +9 kind (1 gather from monster, 2 kill, 3 deliver, 4
gather from NPC, 6 dialog, 8 capture, 9 other quest cleared, 10 change item,
11 time check); +0xD objective symbol; +0x15 NPC. Gather: +0x19 monster count,
+0x1D monsters (stride 4), +0x14D drop percent (float, one per monster), +0x23D
count, +0x241 item, +0x249 losing the item regresses the quest
(QuestBase_RefreshMissionProgressAndCompletion 9259C0). Kill: +0x15 monster
count, +0x19 monsters, +0x149 count. Deliver: +0x19 item count, +0x42 items,
+0xC0 the hand-over line. Dialog: +0x1E the line.
