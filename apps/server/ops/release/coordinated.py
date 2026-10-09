"""
===========================================================================

coordinated.py - publish a server and its client as one release.

A release that changes the browser protocol cannot move one component at a
time: the new client cannot talk to the old server, and the new server
refuses the old client (HTTP 426, which tells an open tab to reload). Both
candidates are built with coordinated plans, staged and verified on their
own, and published here in one journaled operation:

	publish  - server first (maintenance notice, backup, Nomad health), then
	           the client switch and its HTTPS entry check; the journal is
	           left in "verifying"
	confirm  - the browser smoke that ran against the live pair passed and the
	           stage key recorded its evidence; both generations advance
	revert   - restore the retained client, then redeploy the retained server

A maintenance pair (both candidates built with --maintenance) runs the same
three steps inside a closed window: publish first closes the gate, so the
GameWorlds admit only the host's maintenance_accounts (the release probe),
then runs the server's offline store upgrade and journals each shard's
backup before the new server deploys; confirm opens the gate; revert
restores every journaled backup with the fleet stopped before the old pair
returns, and only then opens the gate. Nobody but the probe has played on
the new state, so restoring it loses nothing.

A client candidate cannot record browser evidence before its server is
live, so the evidence for this release is taken after the switch, and a
failure reverts both. The evidence is recorded with the staging key, as for
every candidate: the publication key cannot write its own test receipt. The
receiver holds the host lock around each step; between steps the open
journal entry blocks every other publication.

===========================================================================
"""

import json
from pathlib import Path
import time

from client_deploy import approved_client, check_entry, digest, install_client, live_directory, record_smoke, restore_client
import deploy
from release_state import (COORDINATED, abandon_pair, begin_pair, complete_pair, identity, read_state,
	store_upgrade_required, write_state)
from retention import directory, preserve

VERIFYING = "verifying"
MAX_REASON_CHARS = 512


# ================
# plan_of
#
# The approved plan of a staged candidate, from its private record.
# ================
def plan_of(config, candidate_id):
	record = Path(config["candidate_records"]) / identity(candidate_id)
	return json.loads((record / "candidate.json").read_text())["plan"]


# ================
# open_operation
#
# The coordinated journal entry, or a refusal when none is in progress.
# ================
def open_operation(state):
	operation = state.get("operation")
	if not operation or operation["component"] != COORDINATED:
		raise ValueError("no coordinated release is in progress")
	return operation


# ================
# enter
#
# Record a phase change durably before acting on it.
# ================
def enter(config, state, phase):
	operation = state["operation"]
	operation["phase"] = phase
	operation["phaseStartedAt"] = time.time()
	write_state(config["production_state"], state)


# ================
# publish
#
# Admit the pair, retain both live releases, then replace the server and the
# client. A server that fails its own health checks leaves the journal
# "failed" with the client untouched; revert restores the retained server.
# A client that fails its entry check reverts both at once. A maintenance
# pair closes the gate before anything stops and journals each shard's
# upgrade as it happens (journal_upgrade).
# ================
def publish(config, request, scratch, verify=check_entry):
	server_id = identity(request.get("server"))
	client_id = identity(request.get("client"))
	state = read_state(config["production_state"])
	manifest = deploy.verified_server(config, server_id, scratch)
	client = approved_client(config, client_id, state)
	if manifest["plan"].get("maintenance") or client["plan"].get("maintenance"):
		deploy.gate_accounts(config)
	pending = begin_pair(state, manifest["plan"], client["plan"], time.time())
	maintenance = bool(pending["operation"].get("maintenance"))
	deploy.retain_server(config, state)
	preserve(config, "client", state["client"], client["old"], client["manifest"])
	entry = digest(client["new"] / "index.html")
	pending["operation"]["server"]["candidate"] = server_id
	pending["operation"]["client"].update(candidate=client_id, entrySha256=entry)
	write_state(config["production_state"], pending)
	upgrade = maintenance and store_upgrade_required(state, manifest["plan"])
	try:
		if maintenance:
			deploy.open_gate(config)
		warning = deploy.rollout(config, Path(scratch) / "server", manifest, upgrade=upgrade,
			on_upgrade=journal_upgrade(config, pending) if upgrade else None)
	except Exception:
		enter(config, pending, "failed")
		deploy.alert_staff(config, "Coordinated release " + manifest["commit"] + " failed in the server rollout. Run the revert workflow.")
		raise
	if warning:
		pending["operation"]["warning"] = warning
	try:
		install_client(config, client)
		verify(config["origin"], entry)
	except Exception:
		revert(config, "client publication failed its HTTPS entry check", verify)
		raise
	enter(config, pending, VERIFYING)
	return {"phase": VERIFYING, "server": pending["operation"]["server"], "client": pending["operation"]["client"],
		"maintenance": maintenance}


# ================
# journal_upgrade
#
# Record each shard of a maintenance upgrade durably: None once its commit
# starts, then the backup it kept, before the next shard or the deploy.
# ================
def journal_upgrade(config, pending):
	def record(shard, backup):
		pending["operation"]["authorities"][shard] = backup
		write_state(config["production_state"], pending)
	return record


# ================
# confirm
#
# The browser evidence the staging key recorded for the live client, taken
# after the pair went live, completes the pair. The live bytes must still be
# the pair this operation published.
# ================
def confirm(config):
	state = read_state(config["production_state"])
	operation = open_operation(state)
	if operation["phase"] != VERIFYING:
		raise ValueError("the coordinated release is not awaiting confirmation")
	client_id = operation["client"]["candidate"]
	link = Path(config["client_link"]).resolve()
	if link != (Path(config["client_candidates"]) / client_id).resolve():
		raise RuntimeError("live client changed during verification")
	if json.loads((Path(config["module"]) / "release.json").read_text())["commit"] != operation["server"]["commit"]:
		raise RuntimeError("live server changed during verification")
	evidence = Path(config["candidate_records"]) / client_id / "smoke.json"
	if not evidence.is_file() or evidence.stat().st_mtime < operation["phaseStartedAt"]:
		raise ValueError("no browser evidence was recorded for the live pair")
	report = json.loads(evidence.read_text())
	record_smoke(config, report)
	server_plan = plan_of(config, operation["server"]["candidate"])
	client_plan = plan_of(config, client_id)
	result = complete_pair(state, server_plan, client_plan, time.time())
	result["client"]["candidate"] = client_id
	result["client"]["entrySha256"] = report["entrySha256"]
	if operation.get("warning"):
		result["lastWarning"] = operation["warning"]
	write_state(config["production_state"], result)
	# The release is recorded before anyone is let in.
	if operation.get("maintenance"):
		deploy.close_gate(config)
	return result


# ================
# revert
#
# Restore the pair the journal still records as production: the client from
# its retained manifest, then the server from its retained inputs, without a
# restart notice. Any phase can be reverted, including an interrupted publish
# and a server rollout that failed its health checks.
# Both generations advance afterwards, so the reverted approval cannot be
# replayed. A failed revert stays in the journal for an operator. A
# maintenance revert also restores every journaled database backup before the
# retained server deploys (deploy.restore_authorities), and keeps the gate
# closed until the old pair is back.
# ================
def revert(config, reason, verify=check_entry):
	if not isinstance(reason, str) or not reason.strip() or len(reason) > MAX_REASON_CHARS:
		raise ValueError("revert requires a bounded reason")
	state = read_state(config["production_state"])
	operation = open_operation(state)
	enter(config, state, "reverting")
	try:
		retained_client = directory(config, "client", state["client"]["release"])
		restore_client(config, {
			"old": live_directory(config, state["client"]),
			"manifest": (retained_client / "release.json").read_bytes(),
		}, verify)
		retained_server = directory(config, "server", state["server"]["release"])
		# Always redeploy: a rollout that failed after copying its inputs
		# still shows the old release record, so the record cannot prove
		# which jobs Nomad is running.
		manifest = json.loads((retained_server / "release.json").read_text())
		deploy.rollout(config, retained_server, manifest, notice=False, restore=operation.get("authorities") or None)
		if operation.get("maintenance"):
			deploy.close_gate(config)
	except Exception:
		enter(config, state, "revert-failed")
		deploy.alert_staff(config, "Coordinated release revert failed. Production needs an operator: check Nomad and the client link.")
		raise
	result = abandon_pair(state, time.time(), reason.strip())
	write_state(config["production_state"], result)
	deploy.alert_staff(config, "Coordinated release reverted: " + reason.strip())
	return result
