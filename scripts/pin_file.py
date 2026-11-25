"""
===========================================================================

pin_file.py - read a checked-in archive pin file, or stop

Every importer that reads a later client's archives checks them against a
pin file committed beside it first (the supply chain is pinned, never
guessed): vsro_archive_pins.json, v1696_archive_pins.json, the avatar and
content archive pins. Seven importers read those files, each with its own
copy of the same checks. They read them here, and an importer that checks
one archive of a pinned client against its pin before reading it calls
require_pinned_archive rather than writing that check again.

A pin file is the opposite contract of a build-state file
(state_document.py): a cache that cannot be read counts as empty and costs
a rebuild, but a pin that cannot be read must stop the import, because
reading an unchecked archive is exactly what the pin exists to prevent.

===========================================================================
"""
from __future__ import annotations

import json
from pathlib import Path


# ================
# read_pin_field
#
# The `field` of a pin file in `format`, which must be an instance of
# `kind` (dict for the archive tables, str for a single sha256). Stops the
# import with a message naming the file when the file is missing,
# unreadable, of another format or without that field.
# ================
def read_pin_field(path, format: str, field: str, kind: type = dict):
	try:
		pin = json.loads(Path(path).read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{path} is unreadable: {error}")
	if not isinstance(pin, dict) or pin.get("format") != format or not isinstance(pin.get(field), kind):
		raise SystemExit(f"{path} is not a {format} pin file")
	return pin[field]


# ================
# require_pinned_archive
#
# The archive `name`.pk2 in the client folder `root`, checked against the
# `archives` table of the pin file before any of it is read: the pin
# must name it, the file must exist and its sha256 must match. `where`
# says what `root` must be (the variable or flag that names it) for the
# missing-file message. Returns the archive's path.
# ================
def require_pinned_archive(pin_path, format: str, root, name: str, where: str) -> Path:
	# Imported here: prepare_client_resources loads the pk2 reader and its
	# cipher, which a caller of read_pin_field alone must not need.
	from prepare_client_resources import file_digest
	archives = read_pin_field(pin_path, format, "archives")
	if name not in archives:
		raise SystemExit(f"{pin_path} pins no {name}.pk2")
	path = Path(root) / f"{name}.pk2"
	if not path.is_file():
		raise SystemExit(f"{path} not found; {where}")
	actual = file_digest(path)
	if actual != archives[name]:
		raise SystemExit(f"{path} does not match its pin in {Path(pin_path).name} (sha256 {actual})")
	return path
