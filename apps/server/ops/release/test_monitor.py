"""
===========================================================================

test_monitor.py - outage, recovery, maintenance and failed-delivery drills.

Inject observations and notification delivery so no test stops a production
service or sends a false public outage announcement.

===========================================================================
"""

import json
from pathlib import Path
import tempfile
import unittest

from monitor import MAX_MAINTENANCE_SECONDS, check, maintenance_window, poll, transition


# ================
# MonitorTests
#
# Drive the observation clock directly. Alert timing never depends on sleeping.
# ================
class MonitorTests(unittest.TestCase):
	# ================
	# test_one_failure_does_not_alert_and_recovery_is_reported_once
	# ================
	def test_one_failure_does_not_alert_and_recovery_is_reported_once(self):
		bad = {"healthy": False, "detail": "HTTP 503"}
		good = {"healthy": True, "detail": "available"}
		state, event = transition({}, good, 0)
		self.assertIsNone(event)
		state, event = transition(state, bad, 30)
		self.assertIsNone(event)
		state, event = transition(state, good, 60)
		self.assertIsNone(event)
		state, _ = transition(state, bad, 90)
		state, event = transition(state, bad, 120)
		self.assertEqual(event, "down")
		state["notified"] = event
		state, event = transition(state, bad, 150)
		self.assertIsNone(event)
		state, event = transition(state, good, 180)
		self.assertIsNone(event)
		state, event = transition(state, good, 210)
		self.assertEqual(event, "up")
		state["notified"] = event
		_, event = transition(state, good, 240)
		self.assertIsNone(event)

	# ================
	# test_maintenance_suppression_expires_during_a_real_outage
	# ================
	def test_maintenance_suppression_expires_during_a_real_outage(self):
		bad = {"healthy": False, "detail": "restart"}
		state, _ = transition({}, bad, 0, 120)
		state, event = transition(state, bad, 30, 120)
		self.assertIsNone(event)
		_, event = transition(state, bad, 120, 120)
		self.assertEqual(event, "down")

	# ================
	# test_restart_outage_that_ends_with_maintenance_is_not_announced
	#
	# The 2026-09-29 server publish: the restart failed two checks inside the
	# window, and the window closed on the first good check. That once posted
	# "unavailable. available" and then "recovered".
	# ================
	def test_restart_outage_that_ends_with_maintenance_is_not_announced(self):
		bad = {"healthy": False, "detail": "HTTP 502"}
		good = {"healthy": True, "detail": "available"}
		state = {"phase": "up", "notified": "up"}
		state, _ = transition(state, bad, 30, 100)
		state, event = transition(state, bad, 60, 100)
		self.assertIsNone(event)
		state, event = transition(state, good, 120)
		self.assertIsNone(event)
		_, event = transition(state, good, 150)
		self.assertEqual(event, "maintenance-complete")

	# ================
	# test_each_restart_of_a_coordinated_release_opens_its_own_window
	# ================
	def test_each_restart_of_a_coordinated_release_opens_its_own_window(self):
		operation = {"component": "release", "phase": "deploying", "startedAt": 0, "phaseStartedAt": 0}
		self.assertEqual(maintenance_window(operation), MAX_MAINTENANCE_SECONDS)
		operation.update(phase="verifying", phaseStartedAt=300)
		self.assertEqual(maintenance_window(operation), 0)
		operation.update(phase="reverting", phaseStartedAt=900)
		self.assertEqual(maintenance_window(operation), 900 + MAX_MAINTENANCE_SECONDS)
		self.assertEqual(maintenance_window({"component": "client", "phase": "deploying", "startedAt": 0}), 0)
		self.assertEqual(maintenance_window(None), 0)

	# ================
	# test_failed_delivery_is_retried_without_losing_heartbeat
	# ================
	def test_failed_delivery_is_retried_without_losing_heartbeat(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "monitor.json"
			config = {"state": str(path), "url": "https://example.test/api/title/servers",
				"kind": "fleet", "shard": "global", "label": "game service", "webhook": "fixture"}
			messages = []

			# ================
			# failed_delivery
			# A network failure cannot acknowledge an undelivered alert.
			# ================
			def failed_delivery(_path, _message):
				raise RuntimeError("notification transport failed")

			poll(config, 0, request=lambda _url: [], notify=failed_delivery)
			with self.assertRaisesRegex(RuntimeError, "transport failed"):
				poll(config, 30, request=lambda _url: [], notify=failed_delivery)
			self.assertEqual(json.loads(path.read_text())["checkedAt"], 30)
			self.assertNotEqual(json.loads(path.read_text()).get("notified"), "down")
			poll(config, 60, request=lambda _url: [], notify=lambda _path, message: messages.append(message))
			self.assertEqual(len(messages), 1)
			self.assertEqual(json.loads(path.read_text())["notified"], "down")

	# ================
	# test_external_check_rejects_stale_heartbeat_and_inactive_shard
	# ================
	def test_external_check_rejects_stale_heartbeat_and_inactive_shard(self):
		edge = {"kind": "edge", "url": "https://example.test/releases/monitor.json"}
		self.assertTrue(check(edge, 100, lambda _url: {"checkedAt": 90})["healthy"])
		self.assertFalse(check(edge, 400, lambda _url: {"checkedAt": 90})["healthy"])
		fleet = {"kind": "fleet", "url": "https://example.test/api/title/servers", "shard": "global"}
		self.assertFalse(check(fleet, 0, lambda _url: [{"id": "global", "operating": False}])["healthy"])
		fleet["readiness_url"] = "https://example.test/shards/global/transport/readyz"
		self.assertTrue(check(fleet, 0, lambda url: "ready" if url == fleet["readiness_url"] else
			[{"id": "global", "operating": True}])["healthy"])

	# ================
	# test_operating_lease_cannot_hide_blocked_gameplay_readiness
	#
	# Reproduce the incident's split: the reporter renews its lease while the
	# authority lock stops readiness. Delivery must follow the failed check.
	# ================
	def test_operating_lease_cannot_hide_blocked_gameplay_readiness(self):
		with tempfile.TemporaryDirectory() as directory:
			config = {"state": str(Path(directory) / "monitor.json"),
				"url": "https://example.test/api/title/servers", "kind": "fleet", "shard": "global",
				"readiness_url": "https://example.test/shards/global/transport/readyz",
				"label": "game service", "webhook": "fixture"}
			messages = []
			blocked = True

			# ================
			# request
			# A bounded transport timeout represents the blocked authority lock.
			# ================
			def request(url):
				if url == config["url"]:
					return [{"id": "global", "operating": True}]
				self.assertEqual(url, config["readiness_url"])
				if blocked:
					raise TimeoutError("gameplay readiness timed out")
				return "ready"

			notify = lambda _path, message: messages.append(message)
			poll(config, 0, request=request, notify=notify)
			state = poll(config, 30, request=request, notify=notify)
			self.assertEqual(state["phase"], "down")
			self.assertEqual(len(messages), 1)
			self.assertIn("readiness timed out", messages[0])
			blocked = False
			poll(config, 60, request=request, notify=notify)
			state = poll(config, 90, request=request, notify=notify)
			self.assertEqual(state["phase"], "up")
			self.assertEqual(len(messages), 2)
			self.assertIn("has recovered", messages[1])

	# ================
	# test_recovery_does_not_erase_an_outage_with_failed_notification
	# ================
	def test_recovery_does_not_erase_an_outage_with_failed_notification(self):
		state = {"phase": "down", "failures": 2, "deliveryPending": "down"}
		good = {"healthy": True, "detail": "available"}
		state, _ = transition(state, good, 30)
		_, event = transition(state, good, 60)
		self.assertEqual(event, "recovered-before-delivery")

	# ================
	# test_maintenance_completion_is_delivered_once_and_retried_on_failure
	# ================
	def test_maintenance_completion_is_delivered_once_and_retried_on_failure(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			journal = root / "production.json"
			journal.write_text(json.dumps({"operation": {"component": "server", "phase": "deploying", "startedAt": 0}}))
			config = {"state": str(root / "monitor.json"), "url": "https://example.test/api/title/servers",
				"readiness_url": "https://example.test/shards/global/transport/readyz",
				"kind": "fleet", "shard": "global", "label": "game service", "webhook": "fixture",
				"production_state": str(journal)}
			messages = []

			# ================
			# notify
			# Preserve a durable pending event when the first delivery fails.
			# ================
			def notify(_path, message):
				messages.append(message)
				if len(messages) == 1:
					raise RuntimeError("webhook unavailable")

			request = lambda url: "ready" if url == config["readiness_url"] else [{"id": "global", "operating": True}]
			poll(config, 30, request=request, notify=notify)
			poll(config, 60, request=request, notify=notify)
			journal.write_text(json.dumps({"operation": None}))
			with self.assertRaisesRegex(RuntimeError, "webhook unavailable"):
				poll(config, 90, request=request, notify=notify)
			poll(config, 120, request=request, notify=notify)
			poll(config, 150, request=request, notify=notify)
			self.assertEqual(len(messages), 2)
			self.assertEqual(messages[0], messages[1])
			self.assertIn("online after the maintenance check", messages[1])


if __name__ == "__main__":
	unittest.main()
