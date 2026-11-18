"""
===========================================================================

pin_published_characters.py - pin the character ids a release publishes

A published monster id is a public name: the website catalogue links it and
the host's catalogue gate refuses a release that drops one (ops #171). The
variant's allocators keep identities stable only against the staging's own
last build, which is not what is live when a release is built in another
staging. This writes data/published-characters.json, codename -> id, from
the monsters.json of the catalogue live serves; the character allocators
(cap110_monsters.allocate_characters) keep every pinned id.

Refresh it from each published catalogue:

	py -3 scripts/pin_published_characters.py CATALOGUE/<release>/monsters.json

===========================================================================
"""
import argparse
import json
from pathlib import Path

PIN = Path(__file__).resolve().parent / "data" / "published-characters.json"


# ================
# pin_rows
#
# codename -> id for every monster row, refusing a codename that names two
# ids or an id two codenames share: either would make the pin ambiguous.
# ================
def pin_rows(rows):
	pinned, owners = {}, {}
	for row in rows:
		code, identity = row["codename"], row["id"]
		if type(identity) is not int or not code:
			raise ValueError(f"monster row without codename and integer id: {row.get('id')!r}")
		if pinned.get(code, identity) != identity or owners.get(identity, code) != code:
			raise ValueError(f"ambiguous published identity {code} {identity}")
		pinned[code], owners[identity] = identity, code
	return dict(sorted(pinned.items()))


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__.split("\n")[3])
	parser.add_argument("monsters", type=Path, help="the published catalogue's monsters.json")
	arguments = parser.parse_args()
	document = json.loads(arguments.monsters.read_text(encoding="utf-8"))
	pinned = pin_rows(document["monsters"])
	PIN.write_text(json.dumps({"release": document.get("release", ""), "characters": pinned}, indent="\t") + "\n",
		encoding="utf-8", newline="\n")
	print(f"pinned {len(pinned)} published characters to {PIN.name}")


if __name__ == "__main__":
	main()
