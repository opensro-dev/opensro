"""
===========================================================================
import_teleport_transport.py - which teleport links refuse a summoned transport

Reads _RefTeleport and _RefTeleLink from research SQL backups. A link whose
Restrict columns hold kind 2 is refused while the player's first summoned
COS is a transport (CRefTeleportLink_CheckState 513A70 -> 0x1C10). The
v1.150 client's teleportlink.txt carries the kind on the wrong links (the
ferries rather than the town gates); the server reads its database, so the
database decides. The first backup is the authority (the Eternity vSRO
pack built for the v1.188 SR_GameServer); a later backup only confirms,
and each link it disagrees on is listed in the table. Writes
<source-data>/server/action/portal-transport.json, which the bundle build
copies into the projection's derived data (action.PortalTransportFile); it
never restores or modifies a database.
===========================================================================
"""

import argparse
import hashlib
import json
import pathlib
import struct
import sys

from loot_sql_pages import Backup, variable_fields

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
import sro_paths  # noqa: E402

OUTPUT = sro_paths.SOURCE_DATA_ROOT / "server" / "action" / "portal-transport.json"
FORMAT = "sro-portal-transport-v1"

# Restrict kind 2: refused while a transport is summoned (513B1A).
TRANSPORT_RESTRICT = 2
# Five (Restrict, Data_1, Data_2) int triples follow the three method bytes.
RESTRICT_SLOTS = 5


# ================
# read_names
# ================
def read_names(backup):
	names = {}
	for _, at, page in backup.named_rows("_RefTeleport"):
		ident = struct.unpack_from("<i", page, at + 4)[0]
		fields = variable_fields(page, at)
		if not fields:
			raise ValueError("_RefTeleport row without a codename")
		names[ident] = fields[0].decode("ascii").strip()
	return names


# ================
# read_links
#
# vSRO rows (83 fixed bytes) open Service, Owner, Target; ISRO-R rows (87)
# open Owner, Service, Target and append a column. The Fee, method bytes
# and Restrict triples sit at the same offsets in both.
# ================
def read_links(path):
	backup = Backup(path)
	links = {}
	try:
		names = read_names(backup)
		for _, at, page in backup.named_rows("_RefTeleLink"):
			fixed = struct.unpack_from("<H", page, at + 2)[0]
			if fixed not in (83, 87):
				raise ValueError("Unsupported _RefTeleLink row layout %d" % fixed)
			raw = page[at + 4: at + fixed]
			service, owner, target = struct.unpack_from("<iii", raw, 0)
			if fixed == 87:
				owner, service = service, owner
			if service != 1 or owner not in names or target not in names:
				continue
			kinds = [struct.unpack_from("<i", raw, 19 + 12 * i)[0] for i in range(RESTRICT_SLOTS)]
			key = (names[owner], names[target])
			refuses = TRANSPORT_RESTRICT in kinds
			if key in links and links[key] != refuses:
				raise ValueError("Conflicting transport restriction for %s" % (key,))
			links[key] = refuses
	finally:
		backup.close()
	if not links:
		raise ValueError("Empty teleport link catalog")
	return links


# ================
# document
#
# The table's bytes: compact JSON, one trailing newline, so a rerun over the
# same backups reproduces them exactly (--check). The first backup is the
# authority; a later one only records the links it disagrees on.
# ================
def document(backups):
	merged, sources, disagreements = {}, [], []
	for index, path in enumerate(backups):
		links = read_links(path)
		for key, refuses in sorted(links.items()):
			if index == 0:
				merged[key] = refuses
			elif key in merged and merged[key] != refuses:
				disagreements.append(path.name + " " + "->".join(key))
		with path.open("rb") as source:
			sources.append({"name": path.name, "sha256": hashlib.file_digest(source, "sha256").hexdigest()})
	body = {"format": FORMAT, "sources": sources, "disagreements": disagreements,
		"links": [[owner, target, refuses] for (owner, target), refuses in sorted(merged.items())]}
	return (json.dumps(body, separators=(",", ":")) + "\n").encode("utf-8"), merged


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("backups", type=pathlib.Path, nargs="+")
	parser.add_argument("--output", type=pathlib.Path, default=OUTPUT)
	parser.add_argument("--check", action="store_true", help="refuse unless output already holds these bytes")
	args = parser.parse_args()
	data, merged = document(args.backups)
	if args.check:
		if not args.output.is_file() or args.output.read_bytes() != data:
			raise SystemExit(f"{args.output} differs from the backups' transport table")
		print(f"{args.output} matches ({len(merged)} teleport links)")
		return
	args.output.parent.mkdir(parents=True, exist_ok=True)
	args.output.write_bytes(data)
	print(f"Wrote {len(merged)} teleport links, {sum(merged.values())} refuse a transport, to {args.output}")


if __name__ == "__main__":
	main()
