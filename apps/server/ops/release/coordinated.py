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
	resume   - a revert that failed with the candidate server still live and
	           the retained client back returns to "verifying" with the
	           candidate client; fresh evidence must still follow (confirm)

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

The publisher names its operation with a random nonce; the journal keeps only
its sha256 (the journal is public). confirm and revert must echo the nonce, so
a publisher that wakes from sleep cannot end an operation it did not open: a
dead-man revert followed by a new publish leaves its stale nonce unmatched.
Recovery from a lost nonce is an explicit takeover with a reason, which is
journaled and alerted.

A host timer (expire) is the dead-man: a pair left in deploying, verifying,
failed or reverting past DEADMAN_SECONDS with nobody holding the host lock is
reverted on the host, so a publisher that went away never leaves players on
an unconfirmed pair or the journal closed to every other release.

===========================================================================
"""

import hashlib
import json
from pathlib import Path
import re
import time

from client_deploy import approved_client, check_entry, digest, install_client, live_directory, record_smoke, restore_client
import deploy
from release_state import (COORDINATED, abandon_pair, begin_pair, complete_pair, identity, read_state,
	store_upgrade_required, write_state)
from retention import directory, preserve

VERIFYING = "verifying"
MAX_REASON_CHARS = 512
NONCE_PATTERN = re.compile(r"[0-9a-f]{32}")
# A publisher that has not confirmed or reverted within this long is gone.
# The browser gate takes minutes; a maintenance upgrade that runs longer
# holds the host lock, and expire never acts while the lock is held.
DEADMAN_SECONDS = 30 * 60
# Phases a publisher or a revert left unfinished; revert-failed waits for an
# operator, as it always has.
EXPIRABLE_PHASES = ("deploying", VERIFYING, "failed", "reverting")


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
# nonce_digest
#
# The journal keeps the digest of the publisher's nonce, never the nonce.
# ================
def nonce_digest(value):
	if not isinstance(value, str) or not NONCE_PATTERN.fullmatch(value):
		raise ValueError("a coordinated release needs its publisher's 32-hex-digit nonce")
	return hashlib.sha256(value.encode()).hexdigest()


# ================
# bounded_reason
# ================
def bounded_reason(value, purpose):
	if not isinstance(value, str) or not value.strip() or len(value) > MAX_REASON_CHARS:
		raise ValueError(purpose + " requires a bounded reason")
	return value.strip()


# ================
# authorize
#
# confirm and revert act only for the publisher that opened the operation:
# the request echoes its nonce, or takes the operation over with a reason.
# A takeover is journaled on the operation and alerted before it acts.
# ================
def authorize(config, request, action):
	state = read_state(config["production_state"])
	operation = open_operation(state)
	nonce = request.get("nonce")
	if nonce is not None and operation.get("nonceSha256") == nonce_digest(nonce):
		return
	takeover = request.get("takeover")
	if takeover is None:
		raise ValueError("this coordinated release belongs to another publisher: echo its nonce, or take it over"
			" with a reason")
	reason = bounded_reason(takeover, "a takeover")
	operation.setdefault("takeovers", []).append({"action": action, "reason": reason, "at": time.time()})
	write_state(config["production_state"], state)
	deploy.alert_staff(config, "Coordinated release " + action + " by takeover: " + reason)


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
	nonce = nonce_digest(request.get("nonce"))
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
	pending["operation"]["nonceSha256"] = nonce
	pending["operation"]["server"]["candidate"] = server_id
	pending["operation"]["client"].update(candidate=client_id, entrySha256=entry)
	write_state(config["production_state"], pending)
	upgrade = maintenance and store_upgrade_required(state, manifest["plan"])
	try:
		if maintenance:
			deploy.shut_gate(config)
		# maintenance lets the deploy stop the fleet for a changed credential
		# even without a store upgrade (2026-10-11).
		warning = deploy.rollout(config, Path(scratch) / "server", manifest, upgrade=upgrade,
			on_upgrade=journal_upgrade(config, pending) if upgrade else None, maintenance=maintenance)
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
# the pair this operation published, and the request must be its publisher's
# (authorize).
# ================
def confirm(config, request):
	authorize(config, request, "confirm")
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
		deploy.lift_gate(config)
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
	reason = bounded_reason(reason, "revert")
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
		# Reverting a pair that added a credential replaces it again: a
		# maintenance revert may stop the fleet for that, as the publish did.
		deploy.rollout(config, retained_server, manifest, notice=False, restore=operation.get("authorities") or None,
			maintenance=bool(operation.get("maintenance")))
		if operation.get("maintenance"):
			deploy.lift_gate(config)
	except Exception:
		enter(config, state, "revert-failed")
		deploy.alert_staff(config, "Coordinated release revert failed. Production needs an operator: check Nomad and the client link.")
		raise
	result = abandon_pair(state, time.time(), reason)
	write_state(config["production_state"], result)
	deploy.alert_staff(config, "Coordinated release reverted: " + reason)
	return result


# ================
# revert_request
#
# The publication key's revert: only for the operation's publisher, or by
# takeover. publish's own revert (a client the edge does not serve) and the
# dead-man act on the host and call revert directly.
# ================
def revert_request(config, request, verify=check_entry):
	reason = bounded_reason(request.get("reason"), "revert")
	authorize(config, request, "revert")
	return revert(config, reason, verify)


# ================
# resume
#
# The publication key's forward way out of a failed revert. The failed
# revert restored the retained client but could not redeploy the retained
# server, so the candidate server is still the live one: reinstall the
# candidate client and return to verifying. The publisher then records
# fresh evidence and confirms (evidence from before the resume predates
# the phase, and confirm refuses it). Anything else - the retained
# server live again, a live client the revert did not restore, an open
# maintenance gate - is an operator's state, not a resume.
# ================
def resume(config, request, verify=check_entry):
	authorize(config, request, "resume")
	state = read_state(config["production_state"])
	operation = open_operation(state)
	if operation["phase"] != "revert-failed":
		raise ValueError("the coordinated release is not in a failed revert")
	client_id = operation["client"]["candidate"]
	if json.loads((Path(config["module"]) / "release.json").read_text())["commit"] != operation["server"]["commit"]:
		raise RuntimeError("live server is not the release being resumed")
	# approved_client refuses while the live client is anything but the
	# retained production client the failed revert restored.
	client = approved_client(config, client_id, state)
	entry = digest(client["new"] / "index.html")
	if entry != operation["client"].get("entrySha256"):
		raise ValueError("staged client entry differs from the journal")
	if operation.get("maintenance") and not (Path(config["module"]) / deploy.MAINTENANCE_GATE).is_file():
		raise ValueError("the maintenance gate is not closed")
	try:
		install_client(config, client)
		verify(config["origin"], entry)
	except Exception:
		restore_client(config, client, verify)
		raise
	operation.setdefault("resumes", []).append({"at": time.time()})
	enter(config, state, VERIFYING)
	deploy.alert_staff(config, "Coordinated release " + operation["server"]["commit"] +
		" resumed to verifying after a failed revert")
	return {"phase": VERIFYING, "server": operation["server"], "client": operation["client"],
		"maintenance": bool(operation.get("maintenance"))}


# ================
# expire
#
# The dead-man, run by a host timer with the host lock held (so no publish,
# confirm or revert is in flight). A pair unfinished past the deadline is
# reverted; any other stale operation (a single component whose receiver
# died, a failed revert) cannot be undone here and alerts staff once.
# Returns what it did, or None.
# ================
def expire(config, now, verify=check_entry):
	state = read_state(config["production_state"])
	operation = state.get("operation")
	if not operation:
		return None
	phase = operation["phase"]
	age = now - operation.get("phaseStartedAt", operation["startedAt"])
	if age < config.get("deadman_seconds", DEADMAN_SECONDS):
		return None
	if operation["component"] == COORDINATED and phase in EXPIRABLE_PHASES:
		revert(config, "dead-man: the release sat in " + phase + " for " + str(int(age // 60)) +
			" min with no publisher", verify)
		return "reverted"
	if operation.get("deadmanAlertedAt"):
		return None
	operation["deadmanAlertedAt"] = now
	write_state(config["production_state"], state)
	deploy.alert_staff(config, "Release operation " + operation["component"] + " has been in " + phase + " for " +
		str(int(age // 60)) + " min and needs an operator.")
	return "alerted"
