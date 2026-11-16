"""
===========================================================================

pin_file.py - read a checked-in archive pin file, or stop

Every importer that reads a later client's archives checks them against a
pin file committed beside it first (the supply chain is pinned, never
guessed): vsro_archive_pins.json, v1696_archive_pins.json, the avatar and
content archive pins. Seven importers read those files, each with its own
copy of the same checks. They read them here.

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
