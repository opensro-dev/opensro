"""
===========================================================================

shop_layout.py - a later client's shop tables in v1.150's column layout

The VSRO (v1.188) and v1.696 clients ship the twelve shop tables with
the columns of the VSRO database, which v1.150's text tables mostly
share. Two do not:

- refpricepolicyofitem carries PreviousCost (the struck-through "was"
  price) between PaymentDevice and Cost. Neither v1.150 binary has a
  place for it: the v1.188 server's price-row reader skips the
  column, and
  the v1.150 client reads 13 columns (CRefPricePolicyOfItem_ParseTextRow
  0x813910), so a 14-column row would put PreviousCost where it reads
  Cost (#4704).
- refshoptab's text export has a 7th column the database does not hold
  (_RefShopTab has 6): "xxx", empty, or a designer's note in v1.696.
  Neither binary reads it (v1.150 CRefShopTabData_ParseTextRow 0x816c50,
  nor the v1.188 server's shop-tab reader).

native_row is the one conversion: every column of every table is named
here (LAYOUTS), each extra column has a stated fate, and a row of any
other width, or an extra column holding a value v1.150 would lose,
raises instead of being cut. client_payment_device says which
PaymentDevice values the v1.150 client can hold at all.

===========================================================================
"""

PARAMS = ("Param1", "Param1_Desc128", "Param2", "Param2_Desc128", "Param3", "Param3_Desc128", "Param4", "Param4_Desc128")
HEAD = ("Service", "Country")

# Every shop table's v1.150 columns, in file order (the VSRO database
# schema, SRO_VT_SHARD; the v1.150 files have these widths).
LAYOUTS = {
	"refshopgroup.txt": HEAD + ("ID", "CodeName128", "RefNPCCodeName") + PARAMS,
	"refmappingshopgroup.txt": HEAD + ("RefShopGroupCodeName", "RefShopCodeName"),
	"refshop.txt": HEAD + ("ID", "CodeName128") + PARAMS,
	"refmappingshopwithtab.txt": HEAD + ("RefShopCodeName", "RefTabGroupCodeName"),
	"refshoptabgroup.txt": HEAD + ("ID", "CodeName128", "StrID128_Group"),
	"refshoptab.txt": HEAD + ("ID", "CodeName128", "RefTabGroupCodeName", "StrID128_Tab"),
	"refshopgoods.txt": HEAD + ("RefTabCodeName", "RefPackageItemCodeName", "SlotIndex") + PARAMS,
	"refpackageitem.txt": HEAD + ("ID", "CodeName128", "SaleTag", "ExpandTerm", "NameStrID", "DescStrID", "AssocFileIcon") + PARAMS,
	"refscrapofpackageitem.txt": HEAD + ("RefPackageItemCodeName", "RefItemCodeName", "OptLevel", "Variance", "Data", "MagParamNum")
		+ tuple(f"MagParam{n}" for n in range(1, 13)) + PARAMS + ("Index",),
	"refpricepolicyofitem.txt": HEAD + ("RefPackageItemCodeName", "PaymentDevice", "Cost") + PARAMS,
	"refconditiontosellpackageitem.txt": HEAD + ("RefPackageItemCodeName", "AcceptOrReject", "FourCC") + PARAMS,
	"refrewardpolicytosellpackageitem.txt": HEAD + ("RefPackageItemCodeName", "AcceptOrReject", "FourCC") + PARAMS,
}

# The columns a later file has beyond LAYOUTS: (index in the later row,
# name, the values v1.150 can drop without losing anything).
LATER_EXTRA = {
	# The v1.188 server skips it and v1.150 has no column: only the
	# "no previous price" value carries no meaning v1.150 would lose
	# (every VSRO and v1.696 row, census 2026-11-28).
	"refpricepolicyofitem.txt": ((4, "PreviousCost", {"0"}),),
	# A text-export note no binary reads; any text is droppable.
	"refshoptab.txt": ((6, "Note", None),),
}

# The PaymentDevice bits the v1.150 client keeps a price slot for
# (CRefPackageItemData_AddPrice 0x812990: bits 0..5, class table
# {1,2,2,3,2,3}): gold, the three silks, guild points and honor. A later
# client's coin devices (64, 128, 256, 512: Copper/Iron/Silver/Gold Coin
# items; 1024: Arena Coins) and v1.696's 2048 to 131072 have no slot, so
# the v1.150 shop window can neither show nor request them.
CLIENT_PAYMENT_DEVICES = frozenset({"1", "2", "4", "8", "16", "32"})


# ================
# native_row
#
# One later row of table name in v1.150's layout, every column
# accounted for: a row already in that layout is returned as is (a
# copy), a later-layout row loses its LATER_EXTRA columns after each is
# checked to hold a droppable value. Any other width, or an extra
# column with a value v1.150 would lose, raises ValueError naming the
# table, the row and the column.
# ================
def native_row(name, row):
	width = len(LAYOUTS[name])
	if len(row) == width:
		return list(row)
	extra = LATER_EXTRA.get(name, ())
	if len(row) != width + len(extra):
		raise ValueError(f"{name}: a {len(row)}-column row, want {width} (v1.150) or {width + len(extra)} (later): {row[:4]}")
	for index, column, droppable in extra:
		if droppable is not None and row[index] not in droppable:
			raise ValueError(f"{name}: {column} {row[index]!r} has no v1.150 column: {row[:4]}")
	dropped = {index for index, _, _ in extra}
	return [cell for index, cell in enumerate(row) if index not in dropped]


# ================
# client_payment_device
#
# Whether a v1.150-layout price row names a PaymentDevice the v1.150
# client holds a price for (CLIENT_PAYMENT_DEVICES).
# ================
def client_payment_device(price):
	return price[3] in CLIENT_PAYMENT_DEVICES
