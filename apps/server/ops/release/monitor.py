#!/usr/bin/env python3
"""
===========================================================================

monitor.py - bounded availability checks and durable outage notifications.

A systemd timer checks the public game endpoint independently of the game
processes. An external Actions check watches this heartbeat and HTTPS edge so
a dead host cannot silently disable its own alarm. Notification acknowledgement
is recorded only after Discord accepts it; failed delivery is retried.

===========================================================================
"""

import argparse
import http.client
import json
import os
from pathlib import Path
import tempfile
import time
from urllib.parse import urlsplit

from deploy import announce
from release_state import write_state

HTTP_TIMEOUT_SECONDS = 15
MAX_RESPONSE_BYTES = 1 << 20
FAILURE_THRESHOLD = 2
RECOVERY_THRESHOLD = 2
MAX_MAINTENANCE_SECONDS = 600
MAX_HEARTBEAT_AGE_SECONDS = 180


# ================
# get_status
#
# A bounded public HTTPS request cannot follow a redirect to a private host or
# hold the monitoring timer indefinitely. HTTP errors are availability failures.
# ================
def get_status(url):
	endpoint = urlsplit(url)
	if endpoint.scheme != "https" or endpoint.username or endpoint.password or endpoint.fragment:
		raise ValueError("monitor requires a public HTTPS URL")
	connection = http.client.HTTPSConnection(endpoint.hostname, endpoint.port, timeout=HTTP_TIMEOUT_SECONDS)
	try:
		path = endpoint.path or "/"
		if endpoint.query:
			path += "?" + endpoint.query
		connection.request("GET", path, headers={"Cache-Control": "no-cache", "User-Agent": "OpenSRO-monitor/1"})
		response = connection.getresponse()
		if response.status != 200:
			raise RuntimeError("HTTP " + str(response.status))
		data = response.read(MAX_RESPONSE_BYTES + 1)
		if len(data) > MAX_RESPONSE_BYTES:
			raise RuntimeError("monitor response exceeds limit")
		content_type = response.getheader("Content-Type", "").split(";", 1)[0].strip().lower()
		if content_type == "application/json":
			return json.loads(data)
		return data.decode("utf-8").strip()
	finally:
		connection.close()


# ================
# check
#
# Fleet checks require both an operating lease and a responsive authority store.
# A reporter can renew its lease while a gameplay transaction is deadlocked.
# Edge checks require a recent local
# heartbeat; an old static status page must never be mistaken for a live check.
# ================
def check(config, now, request=get_status):
	try:
		value = request(config["url"])
		if config["kind"] == "fleet":
			if not isinstance(value, list):
				raise RuntimeError("server list is not an array")
			if not any(row.get("id") == config["shard"] and row.get("operating") is True for row in value):
				raise RuntimeError("public shard is not operating")
			if request(config["readiness_url"]) != "ready":
				raise RuntimeError("public shard is not ready")
		elif config["kind"] == "edge":
			age = now - value["checkedAt"]
			if age < -MAX_HEARTBEAT_AGE_SECONDS or age > MAX_HEARTBEAT_AGE_SECONDS:
				raise RuntimeError("host monitoring heartbeat is stale")
		else:
			raise ValueError("unknown monitor kind")
		return {"healthy": True, "detail": "available"}
	except Exception as error:
		return {"healthy": False, "detail": str(error)[:240]}


# ================
# transition
#
# Require consecutive observations to avoid one-packet alert noise. Planned
# maintenance suppresses delivery only for a bounded window, never forever.
# ================
def transition(previous, observation, now, maintenance_until=0):
	state = dict(previous)
	healthy = observation["healthy"]
	state["checkedAt"] = now
	state["detail"] = observation["detail"]
	state["successes"] = previous.get("successes", 0) + 1 if healthy else 0
	state["failures"] = previous.get("failures", 0) + 1 if not healthy else 0
	phase = previous.get("phase", "unknown")
	if state["failures"] >= FAILURE_THRESHOLD:
		phase = "down"
	elif state["successes"] >= RECOVERY_THRESHOLD:
		phase = "up"
	state["phase"] = phase
	if phase == "down" and previous.get("phase") != "down":
		state["incidentSince"] = now
	state["maintenance"] = now < maintenance_until
	if state["maintenance"]:
		state["maintenanceSeen"] = True
	if state["maintenance"] or phase == "unknown":
		return state, None
	acknowledged = previous.get("notified", "unknown")
	if phase == "down" and acknowledged != "down":
		# An outage whose alert was suppressed (maintenance, failed delivery)
		# can still be "down" on the first good check after the suppression
		# ends. Announcing it then says "unavailable" about a service that
		# answered; wait for the recovery threshold instead.
		if healthy:
			return state, None
		return state, "down"
	if phase == "up" and acknowledged == "down":
		return state, "up"
	if phase == "up" and previous.get("maintenanceSeen"):
		return state, "maintenance-complete"
	if phase == "up" and previous.get("deliveryPending") in ("down", "recovered-before-delivery"):
		return state, "recovered-before-delivery"
	return state, None


# ================
# maintenance_window
#
# The end of the announced restart the release journal describes, or 0. A
# coordinated release restarts the server while deploying and again while
# reverting; each phase opens its own bounded window. Verification with
# both new components live is not maintenance.
# ================
def maintenance_window(operation):
	if not operation or operation["component"] not in ("server", "release"):
		return 0
	if operation["phase"] not in ("deploying", "reverting"):
		return 0
	return operation.get("phaseStartedAt", operation["startedAt"]) + MAX_MAINTENANCE_SECONDS


# ================
# poll
#
# Persist heartbeat before notification delivery, and acknowledgement after it.
# A failed webhook remains a pending transition for the next timer invocation.
# ================
def poll(config, now, request=get_status, notify=announce):
	path = Path(config["state"])
	previous = json.loads(path.read_text()) if path.exists() else {}
	maintenance_until = 0
	if config.get("production_state"):
		production = json.loads(Path(config["production_state"]).read_text())
		maintenance_until = maintenance_window(production.get("operation"))
	state, event = transition(previous, check(config, now, request), now, maintenance_until)
	if event:
		state["deliveryPending"] = event
	write_state(path, state)
	if event:
		if event == "down":
			message = "OpenSRO " + config["label"] + " is unavailable. " + state["detail"]
		elif event == "up":
			message = "OpenSRO " + config["label"] + " has recovered."
		elif event == "maintenance-complete":
			message = "OpenSRO " + config["label"] + " is online after the maintenance check. You can connect now."
		else:
			message = "OpenSRO " + config["label"] + " recovered after an outage; notification delivery was delayed."
		notify(config["webhook"], message)
		state["notified"] = "down" if event == "down" else "up"
		state.pop("deliveryPending", None)
		if event != "down":
			state.pop("maintenanceSeen", None)
		state["notifiedAt"] = now
		write_state(path, state)
	return state


# ================
# observe_edge
#
# The Actions runner retains only observation state. Its webhook exists in a
# private temporary directory for this invocation and is never an artifact.
# ================
def observe_edge(root, origin):
	root = Path(root)
	(root / "edge").mkdir(exist_ok=True)
	with tempfile.TemporaryDirectory(prefix="monitor-secret-", dir=root) as directory:
		webhook = Path(directory) / "webhook"
		webhook.write_text(os.environ["MONITOR_WEBHOOK"], encoding="utf-8")
		webhook.chmod(0o600)
		config = {"kind": "edge", "url": origin.rstrip("/") + "/releases/health/fleet.json",
			"label": "HTTPS edge or host monitor", "state": str(root / "edge/state.json"), "webhook": str(webhook)}
		return poll(config, time.time())


# ================
# main
#
# Each invocation performs one observation. The scheduler owns cadence and
# serialization; the monitor never creates a hidden background loop.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("config")
	parser.add_argument("--edge-workflow", action="store_true")
	parser.add_argument("--origin", help="HTTPS origin owned by the operator")
	arguments = parser.parse_args()
	if arguments.edge_workflow:
		if not arguments.origin:
			parser.error("--edge-workflow requires --origin")
		state = observe_edge(arguments.config, arguments.origin)
	else:
		config = json.loads(Path(arguments.config).read_text())
		state = poll(config, time.time())
	print(json.dumps(state, sort_keys=True))


if __name__ == "__main__":
	main()
