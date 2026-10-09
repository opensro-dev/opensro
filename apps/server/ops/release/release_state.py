"""
===========================================================================

release_state.py - admission and durable state for approved component releases.

The receiver holds one deployment lock while reading, admitting and updating
this state. Candidates bind a component generation, not just a Git commit: an
intervening deploy followed by a rollback must still invalidate an old approval.
A release that changes the protocol moves both components in one
coordinated operation: each candidate is admitted alone for its own rules,
and the pair is admitted together immediately before publication. A
maintenance pair is a coordinated pair published inside a closed window: it
may also upgrade the persisted schema, because its revert restores the
database backups the upgrade journaled before the old pair returns.
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
COORDINATED = "release"
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
# release operation. A server may also declare storeUpgradeFrom, the oldest
# schema its offline upgrade converts (sro-authority-upgrade); releases
# recorded before that field existed upgrade nothing.
# ================
def compatibility(value, component):
	if not isinstance(value, dict):
		raise ValueError("missing compatibility declaration")
	optional = ()
	if component == "client":
		keys = ("protocol", "assetSchema")
	else:
		keys = ("protocolMin", "protocolMax", "storeReadMin", "storeReadMax", "storeWrite")
		optional = ("storeUpgradeFrom",)
	if not set(keys) <= set(value) <= set(keys + optional):
		raise ValueError("invalid compatibility fields")
	if any(type(value[key]) is not int or value[key] < 1 for key in value):
		raise ValueError("compatibility versions must be positive integers")
	if component == "server":
		if value["protocolMin"] > value["protocolMax"]:
			raise ValueError("invalid protocol range")
		if not value["storeReadMin"] <= value["storeWrite"] <= value["storeReadMax"]:
			raise ValueError("server cannot read its own persisted schema")
		if upgrade_from(value) > value["storeReadMin"]:
			raise ValueError("an offline upgrade must start at or below the readable schema")
	return value


# ================
# upgrade_from
#
# The oldest persisted schema a server can take over: through its offline
# upgrade when it declares one, else only what it reads directly.
# ================
def upgrade_from(value):
	return value.get("storeUpgradeFrom", value["storeReadMin"])


# ================
# store_upgrade_required
#
# True when the candidate cannot open the live database directly and must
# first run its offline upgrade (admission has checked that it can).
# ================
def store_upgrade_required(state, plan):
	return state["server"]["compatibility"]["storeWrite"] < plan["compatibility"]["storeReadMin"]


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
# admit_component
#
# The rules one candidate must meet whatever it is published with: an
# untouched journal, the generation it was built against, a forward or
# recorded rollback identity, and state it can read.
# ================
def admit_component(state, plan):
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
	if plan.get("coordinated", True) is not True:
		raise ValueError("invalid coordination declaration")
	if plan.get("maintenance", True) is not True or plan.get("maintenance") and not plan.get("coordinated"):
		raise ValueError("invalid maintenance declaration")
	mode = plan.get("mode")
	if mode == "forward":
		ancestors = plan.get("ancestors", [])
		if not isinstance(ancestors, list) or current["commit"] not in ancestors:
			raise ValueError("normal deployment cannot move backwards or across branches")
	elif mode == "rollback":
		if plan.get("coordinated"):
			raise ValueError("a rollback restores one recorded component")
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
	# An application release reuses the live data, so it must read the live
	# schema; a data release brings the data its schema describes.
	kind = plan.get("kind", "application")
	if kind not in ("application", "data") or (kind == "data" and component != "client"):
		raise ValueError("unknown release kind")
	if component == "client" and kind == "application" and candidate["assetSchema"] != current["compatibility"]["assetSchema"]:
		raise ValueError("application update requires the existing verified asset schema")
	if component == "server":
		live = state["server"]["compatibility"]
		if not upgrade_from(candidate) <= live["storeWrite"] <= candidate["storeReadMax"]:
			raise ValueError("candidate cannot read or upgrade the live database schema")
		if live["storeWrite"] < candidate["storeReadMin"] and plan["mode"] != "forward":
			raise ValueError("only a forward release may upgrade the persisted schema")
		if candidate["storeWrite"] < live["storeWrite"]:
			raise ValueError("deployment cannot downgrade persisted state")
	return component


# ================
# require_pair
# ================
def require_pair(client, server):
	if not server["protocolMin"] <= client["protocol"] <= server["protocolMax"]:
		raise ValueError("client and server protocol declarations are incompatible")


# ================
# admit
#
# Compare one candidate with production immediately before mutation. Normal
# releases move forward; rollback is a separate intent with a required reason.
# A failed or interrupted rollout requires reconciliation before another one.
# A coordinated candidate is checked here only for its own rules: its peer is
# the other candidate of the pair (admit_pair), not the live component.
# ================
def admit(state, plan):
	component = admit_component(state, plan)
	if plan.get("coordinated"):
		return component
	candidate = plan["compatibility"]
	client = candidate if component == "client" else state["client"]["compatibility"]
	server = candidate if component == "server" else state["server"]["compatibility"]
	require_pair(client, server)
	if component == "server":
		live = state["server"]["compatibility"]
		# Existing browser tabs may keep any protocol accepted by the live server.
		if server["protocolMin"] > live["protocolMin"] or server["protocolMax"] < live["protocolMax"]:
			raise ValueError("server must retain support for existing browser sessions")
	return component


# ================
# admit_pair
#
# A coordinated release replaces both components together, so the new pair
# must agree and the server may drop protocols: an open tab of the old client
# is refused with 426 and told to reload. The live server must be able to
# read whatever the new one writes, because a failed post-switch check
# reverts both components without restoring the database.
# ================
def admit_pair(state, server_plan, client_plan):
	if not server_plan.get("coordinated") or not client_plan.get("coordinated"):
		raise ValueError("both candidates must be built for a coordinated release")
	if admit_component(state, server_plan) != "server" or admit_component(state, client_plan) != "client":
		raise ValueError("a coordinated release pairs one server with one client")
	if server_plan["mode"] != "forward" or client_plan["mode"] != "forward":
		raise ValueError("a coordinated release moves forward")
	require_pair(client_plan["compatibility"], server_plan["compatibility"])
	live = state["server"]["compatibility"]
	written = server_plan["compatibility"]["storeWrite"]
	if not live["storeReadMin"] <= written <= live["storeReadMax"]:
		raise ValueError("the live server could not read the candidate's state after a revert")


# ================
# admit_maintenance
#
# A maintenance pair is published with every player kept out until it is
# confirmed (coordinated.py), so its revert can restore each shard's
# database from the backup the offline upgrade journaled and redeploy the
# old pair on it. It is therefore not bound by admit_pair's rule that the
# live server reads the candidate's state; the server must still be able to
# take over the live schema (admit_component). Both halves must declare it.
# ================
def admit_maintenance(state, server_plan, client_plan):
	if not server_plan.get("maintenance") or not client_plan.get("maintenance"):
		raise ValueError("both candidates must be built for the maintenance release")
	if admit_component(state, server_plan) != "server" or admit_component(state, client_plan) != "client":
		raise ValueError("a maintenance release pairs one server with one client")
	if server_plan["mode"] != "forward" or client_plan["mode"] != "forward":
		raise ValueError("a maintenance release moves forward")
	require_pair(client_plan["compatibility"], server_plan["compatibility"])


# ================
# begin
#
# Journal the intent before touching a running component. Interrupted work is
# visible to both operators and subsequent deployment attempts.
# ================
def begin(state, plan, now):
	if plan.get("coordinated"):
		raise ValueError("a coordinated candidate publishes only with its counterpart")
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
# begin_pair
#
# One journal entry names both candidates; the phase moves from deploying
# (server, then client) to verifying (post-switch browser evidence) and ends
# in confirmation or a revert of both. A maintenance pair also journals each
# shard's upgrade backup (authorities) as the upgrade takes it.
# ================
def begin_pair(state, server_plan, client_plan, now):
	maintenance = bool(server_plan.get("maintenance") or client_plan.get("maintenance"))
	if maintenance:
		admit_maintenance(state, server_plan, client_plan)
	else:
		admit_pair(state, server_plan, client_plan)
	result = json.loads(json.dumps(state))
	result["operation"] = {
		"component": COORDINATED,
		"server": {"release": server_plan["release"], "commit": server_plan["commit"]},
		"client": {"release": client_plan["release"], "commit": client_plan["commit"]},
		"mode": "forward",
		"phase": "deploying",
		"startedAt": now,
		"phaseStartedAt": now,
	}
	if maintenance:
		result["operation"].update(maintenance=True, authorities={})
	return result


# ================
# advance
#
# Record the replaced row in bounded history and install the new one.
# ================
def advance(result, plan, now):
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
	advance(result, plan, now)
	result["operation"] = None
	return result


# ================
# complete_pair
#
# Both generations advance together once the pair passed its live checks.
# ================
def complete_pair(state, server_plan, client_plan, now):
	operation = state.get("operation")
	if not operation or operation["component"] != COORDINATED or operation["phase"] != "verifying":
		raise ValueError("no coordinated release is awaiting confirmation")
	if operation["server"]["release"] != server_plan["release"] or operation["client"]["release"] != client_plan["release"]:
		raise ValueError("confirmation does not match the coordinated release in progress")
	result = json.loads(json.dumps(state))
	advance(result, server_plan, now)
	advance(result, client_plan, now)
	result["operation"] = None
	return result


# ================
# abandon_pair
#
# After a verified revert the old pair is live again. Both generations still
# advance, so no approval made for the reverted attempt can be replayed.
# ================
def abandon_pair(state, now, reason):
	operation = state.get("operation")
	if not operation or operation["component"] != COORDINATED:
		raise ValueError("no coordinated release is in progress")
	result = json.loads(json.dumps(state))
	for component in COMPONENTS:
		result[component]["generation"] += 1
	result["lastFailure"] = {"component": COORDINATED, "server": operation["server"], "client": operation["client"],
		"reason": reason, "at": now}
	result["operation"] = None
	return result
