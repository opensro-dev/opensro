# Magic Cube (v1.696)

Port of the mall Magic Cube: an item that holds matching elixirs in linked
storage. Issue [#3906](https://github.com/opensro-dev/opensro/issues/3906).

v1.150 has none of it (no `UIIT_*MAGIC_CUBE*` strings, no item rows). The
v1.188 `SR_GameServer` already owns the storage op (`CGMagicCubeStorageOP`);
the v1.696 client owns the UI text and the mall rows. The browser client
needs both.

## Native evidence

| Claim | Evidence | Status |
| --- | --- | --- |
| Items are TID 3/2/3/* (`ITEM_PRE_MALL_MAGIC_CUBE_{WEAPON,ARMOR,ACCESSARY,SHIELD}` and `_10`/`_20`/`_30`) | v1.696 / OriDB `itemdata_*.txt` TypeID columns 9–12 | checked |
| Capacities 10 / 20 / 30 / 50; category filter Param (1/2/4/8) | same rows, Param block columns 134 / 136 | checked |
| CSOItem body is `[u32 RefObjID][u32 StoredItemCount]` | DaxterSoul CHARACTER_DATA MAGIC_CUBE Quantity; client band 0x40 group 0x180 | checked |
| Deposit / withdraw are inventory ops 39 / 40 (0x27 / 0x28) | vSRO `_STRG_MOVE_ITEM` (`SP_PUSH_ITEM_INTO_MAGIC_CUBE` / `SP_POP_ITEM_FROM_MAGIC_CUBE`) | checked |
| Contents live in `_InventoryForLinkedStorage` keyed by the cube's ItemID | same SP; GameServer `AttachLinkedStorageToItem`, `CInstanceInventoryForLinkedStorage` | checked |
| Sell refusal while non-empty | `UIIT_STT_MAGIC_CUBE_STORE_SELL_ERROR` → notice **01:8E** (v1.696 client jump table `0xbf42c0` index 127, base `0x180F`) | checked |
| Full refusal | `UIIT_STT_MAGIC_CUBE_INVENTORY_FULL` → notice **01:8A** (same table index 123) | checked |
| Consignment refusal | `UIIT_STT_OPEN_MARKET_SELL_IMPOSSIBLE_MAGIC_CUBE` (Media.pk2 text; not in the thin `sro_client.exe` string table) | checked (market admit `magic-cube`) |
| Use confirm | `UIIT_MSG_MAGIC_CUBE_USE` | checked (client-local confirm → window; no item-use send) |
| 0x706D body layout for 0x27 / 0x28 | v1.696 jump table `0x7f6e4c`: push `0x7f7be0` `[src][dst][u16 qty]`; pop `0x7f7b90` `[u32 RefObjID]` then the same three (struct+0x38; result apply `0xc9f631`) | checked |

Binaries used:

- v1.696 client `sro_client.exe` SHA-256 `8af7afc2a7c8c940133d0755b75a4b511cf95978536f97c57c2c348fdb95277a`
- v1.188 `SR_GameServer.exe` SHA-256 `bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`

## Switch

Later-client content, off by default, same shape as Nasrun / gears stones:

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_MAGIC_CUBE` | Build (`scripts/magic_cube_import.py`) and server (`game/magiccube`, `Runtime.MagicCube`) | Import the sixteen cube rows + icons; admit deposit/withdraw and the sell / consignment refusals |

## Work slices

1. **Wire body** - `IsMagicCube`, CSOItem encode/decode on server and client, so a cube in the bag does not desync inventory parse.
2. **Item import** - sixteen rows from `SRO_V1696_ROOT`, icons, names, shard `itemdata_190002.txt` behind the switch.
3. **Linked storage** - `InventoryRow.LinkedStorage` (+ clone / bag round-trip); capacity and filter helpers; sell refusal `0x8E` while `Data > 0`.
4. **Inventory ops 0x27 / 0x28** - pin the client request layout, then deposit/withdraw + full refusal `0x8A`. Done.
5. **Client window and notices** - notices `0x8A` / `0x8E` and consignment refusal done; use confirm + linked-storage window + B06D 0x27/0x28 apply done.

## Decisions recorded

- **Data word.** `InventoryRow.Data` / `ItemBody.Data` carries StoredItemCount for TID 3.2.3 (capsule and Spirit already share that word by type). The JSON key stays `transformRefObjId` so saved records keep decoding.
- **Move layout.** Push `0x27`: bag elixir slot, cube bag slot, quantity. Pop `0x28`: elixir `RefObjID`, cube bag slot, receive bag slot, quantity. No ActiveMagicCube identity on the wire — the cube is the bag slot in the body.
- **Notice codes.** Full `0x8A`, sell `0x8E` (low byte of `0x188A` / `0x188E` from the v1.696 category-01 table). Client routes them in `native-notice.ts` (absent from the v1.150-generated table).
