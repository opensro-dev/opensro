# The Roc range

The Roc summit is native to v1.150: its arena regions (`RN_RM_ROCAPEX`) and the entry (`RN_RM_ROCAPEXENTER`)
are in the shipped region table, and so are the `ITEM_QNO_RM_MEETROC_*` items. The kinds that fight there, the
gates and the wait room (25792) are not. They come from the v1.188 client and
are built as one native chain (#3399). The switch below stays off until the whole chain has landed.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_ROC` | Build and server | Build: stages the Roc range's nineteen kinds with their skills, names, skill effects and characterinfo, the four gates with their links, the wait room, the six MEETROC quests and the quest kinds' nests (`roc-monsters.txt`). Server: admits those nests (`monster/roc.go`) and runs worlds 40 and 41 (`action/roc.go`). Build and server must agree |

The build reads the VSRO client (`SRO_VSRO_ROOT`, the variant build's own source).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/roc_import.py` after every later layer. Behind the switch:

| Kind | Grade | Level |
| --- | --- | --- |
| `MOB_RM_ROC`, the Roc | 3 (unique) | 100 |
| `MOB_RM_DEVILSAYTAN`, Demon God Shaitan | 8 | 100 |
| `MOB_RM_TAHOMET_ROC`, the arena's Tahomet | 8 | 100 |
| `MOB_RM_SPIRITS` and its three clones | 8 | 100 |
| `MOB_RM_WINGTRIBEKNIGHT` | 8 | 100 |
| `MOB_RM_WINGTRIBEDUKE` | 0 | 100 |
| `MOB_RM_SEALSTONE` and `_2` to `_8`, the summit's seal stones | 0 | 100 |
| `MOB_QT_RM_FEATHERMASK_01`, a quest kind | 0 | 90 |
| `MOB_QT_RM_ROCKY_CLON_01`, a quest kind | 0 | 90 |

- **Where the rows go:** `characterdata_260000.txt` / `skilldata_260000.txt`.
- **Identities:** fresh, above every published row. `.opensro-roc.json` records them, so a rebuild never
  moves a kind.
- **Names:** the arena's Tahomet reuses the native `SN_MOB_RM_TAHOMET` name. The import records only the names
  it adds, so an off build leaves that native name in place.
- **Switching off:** an off build removes the shards, the skill manifest line, the added names, the skill
  effects and characterinfo rows, the wait room's files and region rows, and the marker.

### The gates (step 1b)

| Gate | Teleport | Building | Arrives in |
| --- | --- | --- | --- |
| `GATE_RC_MOUNTAIN_GATE` | 160 | `STORE_RC_TAHOMET_GATE` (24296) | world 1, region 23406 |
| `GATE_RC_TAHOMET_GATE` | 161 | none | world 41 (`INS_ROC_FRONT`), region 25789 |
| `GATE_RC_ROC_GATE` | 162 | none | world 40 (`INS_ROC_TOP`), region 26045 |
| `GATE_RC_ROC_FRONT_GATE` | 163 | `STORE_ROC_FRONT_GATE` (24297) | world 1, region 25792 |

- **Links:** the summit link, 160 to 162, carries the native conditions: the key item
  `ITEM_QNO_RM_MEETROC_6_01` held (kind 4) and spent (kind 5), level 80 and up (kind 1), and no job (kind 6).
  The server evaluates all four (#3765). The key item is renumbered by codename to the variant's id (24164 in
  v1.150). If a variant lacks it, the link stays out rather than opening without its key. 162 back to 160 is
  unconditioned.
- **Ids:** the gates and buildings keep the client's ids. A building id that the variant already uses for
  another object stops the build.
- **The wait room:** region 25792 (`RN_RM_ROCAPEX_WAIT`). v1.150 ships its zone names, but not its region
  row, sector 100/192 or navmesh. Those are merged the way the other later worlds are (`later_world_import`).

### The quest chain (steps 3b and 3c)

- **The quests:** the VSRO client's questdata ships exactly six MEETROC quests: `QNO_RM_MEETROC_1`, `_2`, `_6` and
  `_ECT_1` to `_ECT_3`. Their rows join `questdata.txt` under the source ids 91, 92, 96 and 374-376, which are free
  in v1.150. A row the client lacks, or an id or codename the table already holds, stops the build. v1.150 already
  ships their text and their items, and the contents rows of `_1`, `_2` and `_6`; the three ECT contents rows come
  from the client, cut to v1.150's thirteen columns. The compiled classes are #3796; step 3b ports the custom slots
  of the other five, so all six project.
- **The loop (step 3b):** giving `_2` up reopens `_1`; giving `_6` up or paying it reopens `_2`. Seven guardian kills
  with a Soul Crystal held exchange every crystal for one Soul Fragment, and `_6` pays on holding the fragment. The
  ECT quests repeat while their token is held, take 7, 10 or 15 tokens per run, and keep them at give-up.
- **The Soul Crystal has no source.** No server code grants `ITEM_QNO_RM_MEETROC_6_03`: the quests that fuse it
  (`MEETROC_3` to `_5`, "Expeditions 2-4" in v1.150's text) never shipped in any server or client questdata. `_6`
  completes only for a character who already holds one.
- **The quest kinds' nests:** six v1.188 nests in one hive (545) on the Roc mountain, regions 24690, 24691, 24693,
  24947 and 24948, sharing a cap of 20, and one nest for each of `_6`'s seven guardians (`MOB_QT_01..07_ROCKY_CLON2`,
  which v1.150 ships but never places), one guardian each, in no hive. `scripts/build/import_roc_quest_nests.py` reads them once from the
  v1.188 shard backup the population evidence pins, and commits them to `scripts/data/roc/quest-nests.json`.
  The build writes them as `roc-monsters.txt`. ISRO-R holds the same six nests at the same anchors with other
  tuning; the v1.188 rows win, as for every nest.
- **Switching off:** the six rows leave `questdata.txt` and the three ECT rows leave `questcontentsdata.txt` by
  codename (both are the native tables again, byte for byte) and `roc-monsters.txt` is removed.

### The summit and Tahomet's room (step 4)

The server half follows the v1.188 reference server (addresses in `action/roc.go`).

- **When:** the iSRO-R shard's `SCHEDULE_DEF_MEET_ROC` row, every Saturday 22:00 for four hours, shard local time
  (`world/roc/schedule.go`).
- **Open:** at the start, world 40 (`INS_ROC_TOP`) opens on layer 1, where `GATE_RC_ROC_GATE` arrives. The shard is
  told `UIIT_MSG_QUEST_ROC_APPEAR`, and the eight seal stones stand.
- **The summit's steps:**
  1. When every stone has fallen, the layer hears `UIIT_MSG_QUEST_ROC_BREAK_STONE`.
  2. Ten minutes later, entry closes and the shard is told `UIIT_MSG_QUEST_ROC_DISAPPEAR`.
  3. 42 seconds later, the Roc appears at region 26045.
  4. When the Roc falls, an exit gate rises where it fell and leads to `GATE_RC_MOUNTAIN_GATE`'s spawn.
  5. Thirty minutes after the kill, the layer closes.
- **Entry:** the summit refuses a character riding or with a summoned transport (0x1C30), and anyone once entry
  has closed. Job mode is admitted.
- **End:** the window's end closes entry. The layer closes once an hour has passed since it opened, sending
  everyone inside to the mountain gate.
- **Tahomet's room (world 41):** opens with the summit, also on layer 1, with Tahomet at region 25789. When
  Tahomet falls, a gate rises to `GATE_RC_ROC_FRONT_GATE`'s spawn, the room refuses new entries, and whoever
  stays is sent there after sixty seconds. The room closes with the summit.
- **Inferences** (recorded in `action/roc.go`):
  - the Front opens beside the summit, since its native opener was not found;
  - the notices use the 0x36BF text-key notice;
  - the Roc's timed skills (`MSKILL_RM_ROC_ATTACK10` at the hour, the spirits' five-minute respawn) are not
    ported yet.

### The Roc's countdowns

The v1.188 reference server counts the living Roc down twice. The v1.696 client shows both (0x300C kinds
0x0C14 and 0x0C15). The port sends them on 0x3058 as kinds 0x14 and 0x15, the low bytes, as kinds 5 and 6 carry
0x0C05 and 0x0C06. Only the summit's residents hear them, and a fallen Roc stops both.

- **Berserk** (the summit tick's step 3): 30 minutes after the Roc appears, "berserk in 30 minutes", then 15,
  10, 5, 3 and 1 minutes, `UIIT_MSG_QUEST_ROC_BERSERKER_START` at the hour, and `_AGAIN` every 30 minutes,
  four times. v1.150 lacks the `_AGAIN` line; the Roc import adds the VSRO client's row to `textuisystem.txt`
  and takes it out again on an off build.
- **Departure** (the Roc's six one-shot timers): "leaves in 30 minutes" at 3:00:01 after it appears, then 15, 10,
  5, 3 and 1. At 3:30 every resident is sent to `GATE_RC_MOUNTAIN_GATE`'s spawn; the layer stays open.

### The Roc's notices (step 5a)

- **Appearance:** when the Roc spawns on the summit, the whole shard is told (0x3058 kind 5, the client's unique
  notice), as the v1.188 reference server's in-layer spawner announces a unique.
- **Kill:** when it falls, the shard is told who killed it (kind 6).
- **Which layers speak:** only worlds 40 and 41 send their layers' unique notices, because both Roc world classes
  leave the notices uncontrolled. Every other instance world keeps today's behaviour: natively they are
  data-driven worlds with a per-world switch the port does not read.
- **Off:** with `SRO_ROC` off no world is registered, and nothing changes.

### The Roc's drops (step 5b)

- **Source:** every enabled reward rule the pinned vSRO backup authors for the Roc kinds, committed as
  `scripts/data/roc/rewards-source.json` (`scripts/build/import_roc_rewards.py`). The v1.150 client has none.
- **What ISRO-R adds:** its random rows for the Roc are disabled (service 0), but its fixed table, which has no
  service column, gives the Roc kinds eight trophies at every kill:
  - the Roc: `ITEM_QNO_RM_ARCHEMY_1`, `_2` and `_3`, one or two each;
  - the Demon God Shaitan: `_4`, one or two;
  - the four spirits: `_5` to `_8`, none or one.

  The trophies are native v1.150 items (`itemdata_25000`). The quests that take them, `QNO_RM_ARCHEMY_1` to
  `_8`, are not: v1.150 ships only their text. So the trophies wait for those quests (step 5d) rather than drop
  with nothing to spend them on, and the build summary says so.
- **What the Roc drops there:**
  - three distinct Roc set pieces (group 1);
  - two or three degree-11 pieces (group 12);
  - a 1-in-100,000 degree-11 rare (group 13);
  - five or six of each of the four E051123 scrolls.
- **The bosses, spirits and wing knight:** one or two degree-11 pieces (group 12), plus the same rare chance.
- **Joined to the variant:** `scripts/roc_rewards.py` keeps each group's members that the variant installs, at
  their authored weights, and writes `roc-rewards.txt`. The server joins it after the cap110 overlay
  (`loot/roc.go`).
  - **The scrolls:** the four E051123 scrolls are native v1.150 items (`itemdata_5000`), so the Roc drops
    them on every Roc build.
  - **Group 12:** the degree-11 pieces come from `SRO_CAP110_ITEMS`.
  - **Group 1:** the Roc sets join the equipment import when `SRO_ROC` and `SRO_CAP110_ITEMS` are both on (step
    5c, below). Without them the rule waits and the build summary lists it. A rule that draws distinct pieces
    also waits while the variant installs fewer pieces than it draws.
  - **Not installed:** the rares are in no client the port reads.
  - **Nothing installed:** no file is written, and the Roc kinds roll only the native passes.
- **The scrolls' option bytes:** all 26 rows of the backup that carry option bytes (these four, plus the God
  pillars' teleport holes) carry the same block, and all of them drop ETC items, which have no magic options.
  The importer reads that block as an insert artefact (an inference) and refuses any other.
- **Off:** with `SRO_ROC` off the file is removed and nothing joins.

### The Roc's set armour (step 5c)

- **What joins:** the 72 Roc set pieces (`ITEM_ROC_*_SET`: class 30, level 98, set groups 1-12). They are the
  Red Lightning, Red Thunder and Red Storm sets, for CH and EU, male and female. They join the degree-10/11
  equipment import (`SRO_CAP110_ITEMS`), but only when `SRO_ROC` is on too, because they drop only from the
  Roc.
- **What comes with them:** their own worn models (212 files), the later client's names (the client spells one
  "Red Lightening Set") and their set groups, which the import already writes.
- **Identities:**
  - the pieces number after every other imported piece, so a fresh build numbers the rest the same either way;
  - a build with `SRO_ROC` off keeps their identities reserved (`retiredIds` in the import marker), so no later
    item takes an id a player's Roc piece holds.
  - turned on over a variant built without them, they number above everything the other importers (rewards,
    honor, Nasrun, pets, mall) already published there, so no published identity moves.
- **Off:** with `SRO_ROC` off they aren't imported, and the rest of the import is unchanged.

## What is not here yet

- **Content still to come**, each its own step of #3399:
  - the trophies (`ITEM_QNO_RM_ARCHEMY_1` to `_8`) and their exchange quests (step 5d).
