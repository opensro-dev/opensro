# Magic Cube (v1.696)

Port of the mall Magic Cube: an item that holds matching elixirs in linked
storage. Issue [#3906](https://github.com/opensro-dev/opensro/issues/3906).

v1.150 has none of it (no `UIIT_*MAGIC_CUBE*` strings, no item rows). The
v1.188 reference server already owns the storage op; the v1.696 client owns
the UI text and the mall rows. The browser client needs both.

## Native evidence

| Claim | Evidence | Status |
| --- | --- | --- |
| Items are TID 3/2/3/* (`ITEM_PRE_MALL_MAGIC_CUBE_{WEAPON,ARMOR,ACCESSARY,SHIELD}` and `_10`/`_20`/`_30`) | v1.696 / OriDB `itemdata_*.txt` TypeID columns 9–12 | checked |
| Capacities 10 / 20 / 30 / 50; category filter Param (1/2/4/8) | same rows, Param block columns 134 / 136 | checked |
| CSOItem body is `[u32 RefObjID][u32 StoredItemCount]` | DaxterSoul CHARACTER_DATA MAGIC_CUBE Quantity; client band 0x40 group 0x180 | checked |
| Deposit / withdraw are inventory ops 39 / 40 (0x27 / 0x28) | the v1.188 reference server's storage op (push / pop into linked storage) | checked |
| Contents live in linked storage keyed by the cube's ItemID | the same storage op; the reference server attaches linked storage to the item | checked |
| Sell refusal while non-empty | `UIIT_STT_MAGIC_CUBE_STORE_SELL_ERROR` → notice **01:8E** (v1.696 client jump table `0xbf42c0` index 127, base `0x180F`) | checked |
| Full refusal | `UIIT_STT_MAGIC_CUBE_INVENTORY_FULL` → notice **01:8A** (same table index 123) | checked |
| Consignment refusal | `UIIT_STT_OPEN_MARKET_SELL_IMPOSSIBLE_MAGIC_CUBE` (Media.pk2 text; not in the thin `sro_client.exe` string table) | checked (market admit `magic-cube`) |
| Use confirm | `UIIT_MSG_MAGIC_CUBE_USE` | checked (client-local confirm → window; no item-use send) |
| 0x706D body layout for 0x27 / 0x28 | v1.696 jump table `0x7f6e4c`: push `0x7f7be0` `[src][dst][u16 qty]`; pop `0x7f7b90` `[u32 RefObjID]` then the same three (struct+0x38; result apply `0xc9f631`) | checked |
| Package silk 6 / 12 / 18 / 30 by capacity | later `refpricepolicyofitem` for `PACKAGE_ITEM_MALL_MAGIC_CUBE_*` | checked |

Binaries used:

- v1.696 client `sro_client.exe` SHA-256 `8af7afc2a7c8c940133d0755b75a4b511cf95978536f97c57c2c348fdb95277a`
- v1.188 reference server SHA-256 `bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`

## Switch

Later-client content, off by default, same shape as Nasrun / gears stones:

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_MAGIC_CUBE` | Build (`scripts/magic_cube_import.py`) and server (`game/magiccube`, `Runtime.MagicCube`) | Import the sixteen cube rows + icons; list them on the normal Silk Mall; admit deposit/withdraw and the sell / consignment refusals |

## Work slices

1. **Wire body** - `IsMagicCube`, CSOItem encode/decode on server and client, so a cube in the bag does not desync inventory parse.
2. **Item import** - sixteen rows from `SRO_V1696_ROOT`, icons, names, shard `itemdata_190002.txt` behind the switch.
3. **Linked storage** - `InventoryRow.LinkedStorage` (+ clone / bag round-trip); capacity and filter helpers; sell refusal `0x8E` while `Data > 0`.
4. **Inventory ops 0x27 / 0x28** - pin the client request layout, then deposit/withdraw + full refusal `0x8A`. Done.
5. **Client window and notices** - notices `0x8A` / `0x8E` and consignment refusal done; use confirm + linked-storage window + B06D 0x27/0x28 apply done.
6. **Silk Mall** - sixteen authored packages on `MALL_CONSUME_ETC` at the later package silk. Done.
7. **Drop / trade / stall** - linked rows travel with the cube (drop→ground→pickup, exchange, stall); sell and consignment still refuse while non-empty. Done.

## Decisions recorded

- **Data word.** `InventoryRow.Data` / `ItemBody.Data` carries StoredItemCount for TID 3.2.3 (capsule and Spirit already share that word by type). The JSON key stays `transformRefObjId` so saved records keep decoding.
- **Move layout.** Push `0x27`: bag elixir slot, cube bag slot, quantity. Pop `0x28`: elixir `RefObjID`, cube bag slot, receive bag slot, quantity. No ActiveMagicCube identity on the wire — the cube is the bag slot in the body.
- **Notice codes.** Full `0x8A`, sell `0x8E` (low byte of `0x188A` / `0x188E` from the v1.696 category-01 table). Client routes them in `native-notice.ts` (absent from the v1.150-generated table).
- **Store schema.** `InventoryRow.LinkedStorage` stays in memory (`json:"-"`) and persists at each owning record's top level (`cubeStorages` on character and account warehouse, `storageCubeStorages` on guild, `linkedStorage` on ground items), so schema **61** stays additive inside main's schema-60 read window (#3090): write 61 / read 61–63, rollback-compatible with schema 60.
- **Drop / trade / stall.** Contents stay with the cube (same as a pet scroll's summon). Merchant sale and consignment still refuse while `Data > 0`.
