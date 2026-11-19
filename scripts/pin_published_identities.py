"""
===========================================================================

pin_published_identities.py - pin the ids a release publishes

A published id is what a player holds: a bag stores an item's reference
id, a skill list stores skill ids, and the website catalogue links every
monster by its id (the host's catalogue gate refuses a release that drops
one, ops #171). The variant build numbers its imports afresh on every
full build (#3815), so the allocators keep the ids the last release
shipped through this pin (published_ids.py). This writes
data/published-identities.json from the catalogue live serves:

	characters  monsters.json   codename -> id
	items       items.json      codename -> id
	skills      skills.json     codename -> id

Refresh it from each published catalogue, before the next release is
built:

	py -3 scripts/pin_published_identities.py CATALOGUE/<release>

A refresh never drops or moves a pinned entry: an id a player may hold
stays with its code even when the new catalogue leaves the code out (a
flag turned off since). Moving one refuses; drop it from the file by hand
only for a code no player can hold.

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


# ================
# build_pin
#
# The pin document for the catalogue folder, over the previous pin.
# ================
def build_pin(catalogue, previous):
	document = {"format": published_ids.PIN_FORMAT, "release": ""}
	for name, (file, key) in SOURCES.items():
		source = json.loads((Path(catalogue) / file).read_text(encoding="utf-8"))
		document["release"] = document["release"] or str(source.get("release") or "")
		document[name] = merge_pin(previous.get(name, {}), pin_rows(source[key]))
	return document


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__.split("\n")[3])
	parser.add_argument("catalogue", type=Path, help="the published catalogue's folder (monsters, items, skills)")
	arguments = parser.parse_args()
	previous = published_ids.read_pin(published_ids.PIN) if published_ids.PIN.exists() else {}
	document = build_pin(arguments.catalogue, previous)
	published_ids.PIN.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	counts = ", ".join(f"{len(document[name])} {name}" for name in SOURCES)
	print(f"pinned {counts} to {published_ids.PIN.name} (release {document['release']})")


if __name__ == "__main__":
	main()
