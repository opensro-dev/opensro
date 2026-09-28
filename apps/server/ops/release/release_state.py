"""
===========================================================================

release_state.py - admission and durable state for approved component releases.

The receiver holds one deployment lock while reading, admitting and updating
this state. Candidates bind a component generation, not just a Git commit: an
intervening deploy followed by a rollback must still invalidate an old approval.
This module never changes binaries, web roots, databases or remote services.

===========================================================================
"""

import json
import os
from pathlib import Path
import re

STATE_FORMAT = "opensro-production-v1"
PLAN_FORMAT = "opensro-deployment-v1"
COMPONENTS = ("client", "server")
IDENTITY_PATTERN = re.compile(r"[0-9a-f]{40}(?:[0-9a-f]{24})?")
MAX_HISTORY = 32


# ================
# identity
#
# Accept Git commits and content digests, never paths supplied by a candidate.
# ================
def identity(value):
	if not isinstance(value, str) or not IDENTITY_PATTERN.fullmatch(value):
		raise ValueError("invalid release identity")
	return value


# ================
# compatibility
#
# Validate the declaration before using it for admission. A server rollback
# must read the schema already on disk; restoring an old database is not a
# release operation.
# ================
def compatibility(value, component):
	if not isinstance(value, dict):
		raise ValueError("missing compatibility declaration")
	if component == "client":
		keys = ("protocol", "assetSchema")
	else:
		keys = ("protocolMin", "protocolMax", "storeReadMin", "storeReadMax", "storeWrite")
	if set(value) != set(keys):
		raise ValueError("invalid compatibility fields")
	if any(type(value[key]) is not int or value[key] < 1 for key in keys):
		raise ValueError("compatibility versions must be positive integers")
	if component == "server":
		if value["protocolMin"] > value["protocolMax"]:
			raise ValueError("invalid protocol range")
		if not value["storeReadMin"] <= value["storeWrite"] <= value["storeReadMax"]:
			raise ValueError("server cannot read its own persisted schema")
	return value


# ================
# read_state
#
# A missing or malformed ledger fails closed. Initial installation imports
# inspected live releases explicitly instead of inventing an empty production.
# ================
def read_state(path):
	state = json.loads(Path(path).read_text(encoding="utf-8"))
	if state.get("format") != STATE_FORMAT:
		raise ValueError("invalid production state format")
	for component in COMPONENTS:
		row = state[component]
		identity(row["release"])
		identity(row["commit"])
		if type(row["generation"]) is not int or row["generation"] < 1:
			raise ValueError("invalid component generation")
		compatibility(row["compatibility"], component)
	return state


# ================
# write_state
#
# Flush the complete replacement before rename so interruption cannot leave a
# partially written approval ledger. The caller owns the deployment lock.
# ================
def write_state(path, state):
	path = Path(path)
	temporary = path.with_name(path.name + ".incoming")
	with temporary.open("x", encoding="utf-8", newline="\n") as output:
		# Release manifests hash their ordered JSON projection across Python and JS.
		json.dump(state, output, indent=2, ensure_ascii=False)
		output.write("\n")
		output.flush()
		os.fsync(output.fileno())
	temporary.chmod(0o644)
	os.replace(temporary, path)


# ================
# admit
#
# Compare the candidate with production immediately before mutation. Normal
# releases move forward; rollback is a separate intent with a required reason.
# A failed or interrupted rollout requires reconciliation before another one.
# ================
def admit(state, plan):
	if plan.get("format") != PLAN_FORMAT or plan.get("component") not in COMPONENTS:
		raise ValueError("invalid deployment plan")
	component = plan["component"]
	current = state[component]
	if state.get("operation"):
		raise ValueError("previous deployment requires reconciliation")
	if plan.get("baseRelease") != current["release"] or plan.get("baseGeneration") != current["generation"]:
		raise ValueError("superseded candidate: production changed after this build")
	identity(plan.get("release"))
	identity(plan.get("commit"))
	if plan["release"] == current["release"]:
		raise ValueError("release is already live")
	mode = plan.get("mode")
	if mode == "forward":
		ancestors = plan.get("ancestors", [])
		if not isinstance(ancestors, list) or current["commit"] not in ancestors:
			raise ValueError("normal deployment cannot move backwards or across branches")
	elif mode == "rollback":
		if not isinstance(plan.get("reason"), str) or not plan["reason"].strip():
			raise ValueError("rollback requires an operator reason")
		known = state.get("history", [])
		target = next((row for row in known if row["component"] == component and row["release"] == plan["release"]), None)
		if target is None:
			raise ValueError("rollback target is not a recorded production release")
		if target["commit"] != plan["commit"] or target["compatibility"] != plan.get("compatibility"):
			raise ValueError("rollback declaration differs from its recorded release")
	else:
		raise ValueError("unknown deployment mode")

	candidate = compatibility(plan.get("compatibility"), component)
	if component == "client" and candidate["assetSchema"] != current["compatibility"]["assetSchema"]:
		raise ValueError("application update requires the existing verified asset schema")
	client = candidate if component == "client" else state["client"]["compatibility"]
	server = candidate if component == "server" else state["server"]["compatibility"]
	if not server["protocolMin"] <= client["protocol"] <= server["protocolMax"]:
		raise ValueError("client and server protocol declarations are incompatible")
	if component == "server":
		live = state["server"]["compatibility"]
		if not server["storeReadMin"] <= live["storeWrite"] <= server["storeReadMax"]:
			raise ValueError("candidate cannot read the live database schema")
		if server["storeWrite"] < live["storeWrite"]:
			raise ValueError("deployment cannot downgrade persisted state")
		# Existing browser tabs may keep any protocol accepted by the live server.
		if server["protocolMin"] > live["protocolMin"] or server["protocolMax"] < live["protocolMax"]:
			raise ValueError("server must retain support for existing browser sessions")
	return component


# ================
# begin
#
# Journal the intent before touching a running component. Interrupted work is
# visible to both operators and subsequent deployment attempts.
# ================
def begin(state, plan, now):
	component = admit(state, plan)
	result = json.loads(json.dumps(state))
	result["operation"] = {
		"component": component,
		"release": plan["release"],
		"commit": plan["commit"],
		"mode": plan["mode"],
		"phase": "deploying",
		"startedAt": now,
	}
	return result


# ================
# complete
#
# Advance the generation only after the caller verifies the actual live
# identity and health. Retain bounded rollback history without saving secrets.
# ================
def complete(state, plan, now):
	operation = state.get("operation")
	if not operation or operation["release"] != plan["release"] or operation["component"] != plan["component"]:
		raise ValueError("completion does not match the deployment in progress")
	result = json.loads(json.dumps(state))
	component = plan["component"]
	previous = dict(result[component], component=component)
	result["history"] = (result.get("history", []) + [previous])[-MAX_HISTORY:]
	result[component] = {
		"release": plan["release"],
		"commit": plan["commit"],
		"generation": previous["generation"] + 1,
		"compatibility": plan["compatibility"],
		"deployedAt": now,
	}
	result["operation"] = None
	return result
