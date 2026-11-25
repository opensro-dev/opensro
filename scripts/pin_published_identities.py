"""
===========================================================================

pin_published_identities.py - pin the ids a release publishes

A published id is what a player holds: a bag stores an item's reference
id, a skill list stores skill ids, and the website catalogue links every
monster by its id (the host's catalogue gate refuses a release that drops
one, ops #171). The variant build numbers its imports afresh on every
full build (#3815), so the allocators keep the ids the last release
shipped through this pin (published_ids.py). This writes
data/published-identities.json from what live serves:

	characters  monsters.json, and every characterdata and
	            teleportbuilding row of the served textdata
	items       items.json, and every itemdata row of the served textdata
	skills      skills.json     codename -> id

The catalogue lists monsters and the items it shows; a player's records
name more objects than that by id: a stored pet, a ride, an appointed
rebirth gate, a fortress structure, a premium clock's item. Pinned from
the catalogue alone, the fox pets and the desert gates were numbered
afresh by the next full build (r203r moved 316 objects) and a summoned
fox locked its owner out (#4019). The served textdata is the release's
own server data, so every object it served keeps its id.

Refresh it from each published release, before the next one is built:

	py -3 scripts/pin_published_identities.py CATALOGUE/<release> --textdata SERVER_TEXTDATA

A refresh never drops or moves a pinned entry: an id a player may hold
stays with its code even when the new catalogue leaves the code out (a
flag turned off since). Moving one refuses; drop it from the file by hand
only for a code no player can hold. The one move a refresh makes is the
id owner's rule for a released id a character and an item both hold
(published_ids.settle_shared): the item keeps it, and the character's old
id is kept in "former", which the catalogue publishes as formerIds.

===========================================================================
"""
import argparse
import json
from pathlib import Path

import published_ids

# The catalogue file and list key of each pin map.
SOURCES = {
	"characters": ("monsters.json", "monsters"),
	"items": ("items.json", "items"),
	"skills": ("skills.json", "skills"),
}


# ================
# pin_rows
#
# codename -> id for every row, refusing a codename that names two ids or
# an id two codenames share: either would make the pin ambiguous.
# ================
def pin_rows(rows):
	pinned, owners = {}, {}
	for row in rows:
		code, identity = row["codename"], row["id"]
		if type(identity) is not int or not code:
			raise ValueError(f"row without codename and integer id: {row.get('id')!r}")
		if pinned.get(code, identity) != identity or owners.get(identity, code) != code:
			raise ValueError(f"ambiguous published identity {code} {identity}")
		pinned[code], owners[identity] = identity, code
	return dict(sorted(pinned.items()))


# ================
# merge_pin
#
# The previous map with the new catalogue's rows added: an entry the
# catalogue no longer lists is kept (its id stays reserved), and one it
# lists at another id refuses rather than move what a player holds.
# ================
def merge_pin(previous, current):
	merged = dict(previous)
	for code, identity in current.items():
		if merged.get(code, identity) != identity:
			raise ValueError(f"{code} moved from {merged[code]} to {identity}; a published id never moves")
		merged[code] = identity
	pin_rows([{"codename": code, "id": identity} for code, identity in merged.items()])
	return dict(sorted(merged.items()))


# The object map each served table joins, by the table-name prefixes
# published_ids.object_rows reads (one _RefObjCommon id space).
SERVED_TABLES = {
	"characters": ("characterdata_", "teleportbuilding"),
	"items": ("itemdata_",),
}


# ================
# served_objects
#
# map -> codename -> id for every object row of the served textdata, a
# disabled row too (it still holds its id). The rows come from the one
# reader of the object tables (published_ids.object_rows).
# ================
def served_objects(textdata):
	rows = {name: [] for name in SERVED_TABLES}
	for table, row in published_ids.object_rows(textdata):
		if len(row) < 3 or not row[2]:
			continue
		for name, prefixes in SERVED_TABLES.items():
			if table.startswith(prefixes):
				rows[name].append({"codename": row[2], "id": int(row[1])})
	if not all(rows.values()):
		raise ValueError(f"{textdata} holds no character or no item table; pass the release's server textdata")
	return {name: pin_rows(named) for name, named in rows.items()}


# ================
# build_pin
#
# The pin document for the catalogue folder and the served textdata,
# over the previous pin. The catalogue and the textdata are one release:
# a code they number differently refuses (merge_pin).
# ================
def build_pin(catalogue, previous, textdata):
	document = {"format": published_ids.PIN_FORMAT, "release": ""}
	if previous.get(published_ids.FORMER):
		document[published_ids.FORMER] = previous[published_ids.FORMER]
	served = served_objects(textdata)
	for name, (file, key) in SOURCES.items():
		source = json.loads((Path(catalogue) / file).read_text(encoding="utf-8"))
		document["release"] = document["release"] or str(source.get("release") or "")
		current = merge_pin(served.get(name, {}), pin_rows(source[key]))
		document[name] = merge_pin(previous.get(name, {}), current)
	return published_ids.settle_shared(document)


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__.split("\n")[3])
	parser.add_argument("catalogue", type=Path, help="the published catalogue's folder (monsters, items, skills)")
	parser.add_argument("--textdata", type=Path, required=True,
		help="the same release's server textdata (every object it served)")
	arguments = parser.parse_args()
	previous = published_ids.read_pin(published_ids.PIN) if published_ids.PIN.exists() else {}
	document = build_pin(arguments.catalogue, previous, arguments.textdata)
	published_ids.PIN.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	counts = ", ".join(f"{len(document[name])} {name}" for name in SOURCES)
	print(f"pinned {counts} to {published_ids.PIN.name} (release {document['release']})")


if __name__ == "__main__":
	main()
