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

The HLIL and methods dumps the class snapshot is imported from are kept
outside the product, in `research/investigations/quest/compiled-dumps/`
(`compiled_hlil.json`, sha256 `1a23832e...`, the snapshot's `dumpSHA256`, and
`compiled_methods.json`, with `SHA256SUMS`). Re-importing them must reproduce
`compiled-quests-source.json` byte for byte.

```text
python -B scripts/build/import_compiled_quest_evidence.py --hlil <dump> --methods <dump>
python -B scripts/build/import_quest_text_evidence.py --textdata <v1.150-textdata> --sql <SR_GameRefData>
python -B scripts/build/import_quest_text_evidence.py --textdata <v1.150-textdata>   # the text snapshot alone
python -B scripts/build/generate_compiled_quests.py
python -B scripts/build/generate_compiled_quests.py --check
```

## What each source decides

The class supplies the structure: NPCs, dialogue symbols, prerequisites, the
completion limit, and each mission's kind, NPC, item, monsters and drop chance.
v1.188 rebalanced counts and rewards, so the v1.150 text wins wherever it names
them: an objective line's count, and the popup's advertised EXP, skill EXP,
gold and inventory slots. The SQL rows supply the reward items, and the scalars
of a quest whose popup advertises none, as they did for the script-backed
`QNO_RM_OLDWOMAN_1`. A popup body can span several lines of `textquest.txt`;
the importer joins them into one record.

Lists: 0xF3 completion NPCs, 0xF7 quest NPCs, 0x102 every quest completed,
0x10C quests active, 0x114 any one quest completed, 0x108 quests ended and
never completed (`CBasicQuest_MeetsPrerequisites` 9262A0; the server's
`EndedQuestIds`). 0x110 is not yet read.
A 0x102 entry must be completed as many times as its byte at +0x418 says
(one byte per entry, 1 by default). The importer records such byte and word
writes as `"0x106.b"`, keeping a value read at run time as null. The generator
projects the first entry's count as `RequiredQuestCompletions`, and refuses
any other field that differs from the constructor's default (91E200), such
as 0x15B/0x15C, which tie the quest to an instance world.
The condition table (0xC2) gates the offer through its flag word, which the
initializer sets through a pointer copy (`*eax |= 1`); the importer records
it as the table's `flags`. Flag 1 admits from +0x4, projected as `MinLevel`
(+0x23 is the questdata level, which only picks the marker); flag 2 checks
the repeat limit and the lists; 0x100 is the country. The generator refuses
any other flag, a condition the port does not check. Flag 4 requires held
items: every one of the vector the initializer pushes at +0x28 (begin/end
+0x2C) and, when listed, one of +0x38 (+0x3C), projected as
`RequiredHeldItems` / `RequiredAnyHeldItems`. The importer records those pushes
as the table's `"0x28"` / `"0x38"` lists, whether the initializer names the
table `arg3[0xc2]` or `*(arg1 + 0x308)`.

A class that overrides vtable slots has custom behaviour (NPC talk at +0x58,
dialogue at +0x90, the capture escort's +0x80/+0x9C/+0xA8 timers and events).
The generator projects it only once `CLASS_BEHAVIOUR` states that override in
QuestSpec fields (`QSP_KT_EXINVENTORY_3`'s +0x58 is a 10,000 gold turn-in fee),
and a quest waits while a prerequisite is unprojected. `compiled_quests_audit.json`
names every class still pending and why.

Mission field notes: +9 kind (1 gather from monster, 2 kill, 3 deliver, 4
gather from NPC, 6 dialog, 8 capture, 9 other quest cleared, 10 change item,
11 time check); +0xD objective symbol; +0x15 NPC. Gather: +0x19 monster count,
+0x1D monsters (stride 4), +0x14D drop percent (float, one per monster), +0x23D
count, +0x241 item, +0x249 losing the item regresses the quest
(QuestBase_RefreshMissionProgressAndCompletion 9259C0). Kill: +0x15 monster
count, +0x19 monsters, +0x149 count. Deliver: +0x19 (byte) the number of
items, +0x42 their codenames and +0x1A their quantities (stride 4, the pairs
9208D0 grants), +0xC0 the hand-over line, +0xC4 the not-yet-delivered line.
Dialog: +0x1E the line.
