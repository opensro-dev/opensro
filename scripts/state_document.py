"""
===========================================================================

state_document.py - the build's small JSON state files

The Python build keeps a few reuse records: the image discovery cache
(convert_images.py), the archives stage 1 already hashed
(import_vsro_alexandria.py), the variant's settle ledger
(variant_settle.py). Each is one JSON object with a "format" tag, an
optional "version" and one dict body. Each used to read and write it its
own way. They share these two functions, so every one of them treats a
foreign or unreadable file the same way (as empty: the worst case is redoing
the work) and is written whole, and only when its bytes change, which
leaves a no-op run's stat fingerprints alone.

===========================================================================
"""
from __future__ import annotations

import json
from pathlib import Path

from prepare_client_resources import write_atomically


# ================
# read_document
#
# The document's body field when the file holds this format (and version,
# when one is given) and the field is an object; None otherwise.
# ================
def read_document(path: Path, format: str, field: str, version: int | None = None) -> dict | None:
	try:
		document = json.loads(Path(path).read_text(encoding="utf-8"))
	except (OSError, ValueError):
		return None
	if not isinstance(document, dict) or document.get("format") != format:
		return None
	if version is not None and document.get("version") != version:
		return None
	body = document.get(field)
	return body if isinstance(body, dict) else None


# ================
# write_document
#
# Writes {"format", "version"?, field: body} with sorted keys, minified,
# unless the file already holds exactly these bytes. Returns whether it
# wrote.
# ================
def write_document(path: Path, format: str, field: str, body: dict, version: int | None = None) -> bool:
	document = {"format": format, field: body}
	if version is not None:
		document["version"] = version
	data = json.dumps(document, separators=(",", ":"), sort_keys=True).encode("utf-8")
	path = Path(path)
	try:
		if path.read_bytes() == data:
			return False
	except OSError:
		pass
	write_atomically(path, data)
	return True
