"""
===========================================================================

shard_switch.py - a port-only shard switch's deployed posture, and its
server overlays, for the Python build

Port-only server switches (SRO_QINSHI_TOMB, SRO_MUHAN) append committed
overlay rows behind a flag that can default on in the shard registry. A
Python import that must know whether those rows stand cannot read the
bare build env: a release runs each step under the shard profile, which
leaves a switch it does not name unset, while the deployed shard takes
the registry's Default. Reading unset as off is the #2024 failure
(scripts/build/shared/shardSwitch.mjs): in r203, HUNTING2 staged five
MOB_TQ_* kinds the on-by-default Qin-Shi overlay already defines, and
the bake refused the duplicate codenames.

This mirrors shardSwitch.mjs for Python: the same profile, the same
registry row, the same overlay framing. The server's shardsettings.go
owns the defaults; neither reader keeps its own copy.

===========================================================================
"""
import json
import os
from pathlib import Path
import re

from sro_paths import REPO_ROOT

SERVER_ROOT = REPO_ROOT / "apps" / "server"
# The deployed profile and the registry holding every switch's Default
# (internal/config/shardsettings/shardsettings.go, the server's one owner).
SHARD_PROFILE = Path("config") / "shard-settings.json"
SHARD_REGISTRY = Path("internal") / "config" / "shardsettings" / "shardsettings.go"


# ================
# switch_posture
#
# The switch's value as the deployment sets it when the build env does
# not: the profile's entry, else the registry Default. A registry row
# this cannot read raises: a posture the build cannot see must stop it,
# never read as off.
# ================
def switch_posture(name, server_root=SERVER_ROOT):
	profile = json.loads((server_root / SHARD_PROFILE).read_text(encoding="utf-8"))
	if isinstance(profile.get(name), str):
		return profile[name]
	registry = (server_root / SHARD_REGISTRY).read_text(encoding="utf-8")
	row = re.search(r'\{\s*Env:\s*"' + re.escape(name) + r'"[^}]*?Default:\s*"([^"]*)"', registry)
	if not row:
		raise ValueError(f"{name}: no Default in shardsettings.go; the build cannot see the shard's posture")
	return row.group(1)


# ================
# switch_enabled
#
# An explicit env value wins (any non-blank value, "off" included); unset
# or blank takes the deployed posture. on_words are the words the switch's
# Go reader accepts once trimmed and folded to lower case.
# ================
def switch_enabled(name, on_words, env=os.environ, server_root=SERVER_ROOT):
	value = env.get(name, "")
	word = value if value.strip() else switch_posture(name, server_root)
	return word.strip().lower() in on_words


# ================
# overlay_rows
#
# One committed server overlay as column rows: UTF-8, tab-separated, in
# the v1.150 table layout, with blank lines and `#` or `//` comment lines
# skipped (the server's SplitTabbedText framing).
# ================
def overlay_rows(relative_path, server_root=SERVER_ROOT):
	text = (server_root / relative_path).read_text(encoding="utf-8").lstrip("﻿")
	rows = []
	for line in text.splitlines():
		if not line.strip():
			continue
		cols = line.split("\t")
		first = cols[0].strip()
		if first.startswith("#") or first.startswith("//"):
			continue
		rows.append(cols)
	return rows
