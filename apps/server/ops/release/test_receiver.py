"""
===========================================================================

test_receiver.py - prove that staging credentials cannot publish releases.

Dispatch is tested with injected operation owners. Rejected requests must not
reach either live component, even when they otherwise have a valid identity.

===========================================================================
"""

import contextlib
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import receiver
from bundle import FILES, bundle
from test_bundle import fixture_file
from release_state import write_state
from test_release_state import candidate, production


# ================
# ReceiverTests
#
# Role separation is independent of artifact validity and SSH configuration.
# ================
class ReceiverTests(unittest.TestCase):
	# ================
	# test_server_retry_reuses_only_verified_retained_bytes
	# ================
	def test_server_retry_reuses_only_verified_retained_bytes(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			module = root / "module"
			for name, source in FILES.items():
				path = module / source
				path.parent.mkdir(parents=True, exist_ok=True)
				path.write_bytes(fixture_file(name, source, candidate("server")["commit"]))
			plan = candidate("server")
			plan["release"] = plan["commit"]
			archive = root / "server.tar"
			bundle(module, archive, plan["commit"], plan)
			config = {"production_state": str(root / "production.json"), "candidate_records": str(root / "records")}
			(root / "records").mkdir()
			write_state(config["production_state"], production())
			first = receiver.stage_server(config, archive, root / "first")
			self.assertEqual(receiver.stage_server(config, archive, root / "retry"), first)
			retained = root / "records" / first["candidate"] / "server.tar"
			retained.write_bytes(b"corrupted archive")
			with self.assertRaisesRegex(ValueError, "retained server archive changed"):
				receiver.stage_server(config, archive, root / "corrupt")

	# ================
	# test_staging_refuses_a_release_that_needs_a_stop_it_will_not_make
	#
	# The rehearsal names credential variables that need the fleet stopped.
	# Only a maintenance pair whose store upgrades stops before it validates
	# (deploy.py), so any other release with such a change is refused at
	# staging instead of failing inside its window.
	# ================
	def test_staging_refuses_a_release_that_needs_a_stop_it_will_not_make(self):
		stops = [("sro-gameworld-1", "nomad/jobs/sro-gameworld-1/gameworld/gameworld")]
		for needed, upgrades, refused in ((False, False, False), (True, False, True), (True, True, False)):
			with self.subTest(needed=needed, upgrades=upgrades), tempfile.TemporaryDirectory() as directory:
				root = Path(directory)
				module = root / "module"
				for name, source in FILES.items():
					path = module / source
					path.parent.mkdir(parents=True, exist_ok=True)
					path.write_bytes(fixture_file(name, source, candidate("server")["commit"]))
				plan = candidate("server")
				plan["release"] = plan["commit"]
				archive = root / "server.tar"
				bundle(module, archive, plan["commit"], plan)
				config = {"production_state": str(root / "production.json"), "candidate_records": str(root / "records")}
				(root / "records").mkdir()
				write_state(config["production_state"], production())
				with patch.object(receiver, "rehearse", return_value=stops if needed else []), \
					patch.object(receiver, "can_stop_for_credentials", return_value=upgrades):
					if refused:
						with self.assertRaisesRegex(ValueError, "would fail inside its window"):
							receiver.stage_server(config, archive, root / "scratch")
						self.assertEqual(list((root / "records").iterdir()), [])
					else:
						self.assertIn("candidate", receiver.stage_server(config, archive, root / "scratch"))

	# ================
	# test_every_maintenance_pair_may_stop_for_credentials
	#
	# With or without a store upgrade: the deploy validates first and stops
	# only for a reported credential. A plain release never may.
	# ================
	def test_every_maintenance_pair_may_stop_for_credentials(self):
		for plan, expected in (({}, False), ({"maintenance": False}, False), ({"maintenance": True}, True)):
			with self.subTest(plan=plan):
				self.assertEqual(receiver.can_stop_for_credentials(plan), expected)

	# ================
	# test_staging_cannot_dispatch_publication
	# ================
	def test_staging_cannot_dispatch_publication(self):
		with patch.object(receiver, "promote") as client, patch.object(receiver, "publish_server") as server, 			patch.object(receiver, "coordinated") as pair:
			for operation in ("publish-client", "publish-server", "rollback-client", "rollback-server",
				"publish-coordinated", "confirm-coordinated", "revert-coordinated",
				"resume-coordinated"):
				with self.subTest(operation=operation), self.assertRaisesRegex(ValueError, "not allowed"):
					receiver.request({}, "stage", {"operation": operation, "candidate": "a" * 64}, Path("unused"))
			client.assert_not_called()
			server.assert_not_called()
			self.assertEqual(pair.mock_calls, [])

	# ================
	# test_publication_cannot_write_its_own_test_evidence
	# ================
	def test_publication_cannot_write_its_own_test_evidence(self):
		with patch.object(receiver, "record_smoke") as evidence:
			with self.assertRaisesRegex(ValueError, "not allowed"):
				receiver.request({}, "publish", {"operation": "client-smoke", "report": {}}, Path("unused"))
			evidence.assert_not_called()

	# ================
	# test_authorized_dispatch_preserves_candidate_identity
	# ================
	def test_authorized_dispatch_preserves_candidate_identity(self):
		with patch.object(receiver, "promote", return_value={"published": True}) as client:
			result = receiver.request({}, "publish", {"operation": "publish-client", "candidate": "b" * 64}, Path("unused"))
			self.assertEqual(result, {"published": True})
			client.assert_called_once_with({}, "b" * 64)


	# ================
	# test_a_failed_request_leaves_its_reason_on_the_host
	#
	# The publisher may discard the receiver's stderr (2026-10-11): main()
	# records the reason in the host journal, masked and bounded, and still
	# raises so the exit status and the SSH output are unchanged.
	# ================
	def test_a_failed_request_leaves_its_reason_on_the_host(self):
		logged = []
		token = "f" * 64
		failure = RuntimeError("sro-nomad validate refused; token " + token + " " + "word " * 600)
		with patch.object(receiver, "serve_detached", side_effect=failure), \
			patch.object(receiver, "CONFIG", Path(__file__)), \
			patch.object(receiver.json, "loads", return_value={}), \
			patch.object(receiver.os, "geteuid", return_value=0, create=True), \
			patch.object(receiver.sys, "argv", ["receiver.py", "publish"]), \
			patch.object(receiver, "syslog", SimpleNamespace(LOG_ERR=3, openlog=lambda name: None,
				syslog=lambda level, text: logged.append((level, text)))):
			with self.assertRaises(RuntimeError):
				receiver.main()
		self.assertEqual(len(logged), 1)
		level, text = logged[0]
		self.assertEqual(level, 3)
		self.assertTrue(text.startswith("publish request failed: RuntimeError: sro-nomad validate refused"))
		self.assertNotIn(token, text)
		self.assertIn("<masked>", text)
		self.assertEqual(len(text), len("publish request failed: ") + receiver.MAX_FAILURE_LOG_CHARS)


# ================
# DetachedPublicationTests
#
# A publication request runs in its own unit (launch, injected), so the SSH
# session only follows it. The unit's body is the real run_job with the
# host lock and the dispatched operation injected.
# ================
class DetachedPublicationTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		self.root = Path(directory.name)
		# The host's config has no "requests" key (its controls were never
		# installed by install.main): the receiver's own default is used and
		# created on first use.
		self.config = {"scratch": str(self.root)}
		self.requests = self.root / "var-lib-requests"
		patcher = patch.object(receiver, "REQUESTS", self.requests)
		patcher.start()
		self.addCleanup(patcher.stop)
		self.jobs = []
		self.handled = []
		self.printed = []
		for name, replacement in (("request", self.fake_request), ("candidate_status", lambda config: None),
			("print", lambda *values, **options: self.printed.append(" ".join(map(str, values))))):
			patcher = patch.object(receiver, name, side_effect=replacement, create=name == "print")
			patcher.start()
			self.addCleanup(patcher.stop)

	# ================
	# fake_request
	# ================
	def fake_request(self, config, role, value, scratch):
		self.handled.append((role, value))
		if value.get("fail"):
			raise RuntimeError("Nomad refused; token " + "f" * 64)
		return {"phase": "verifying"}

	# ================
	# follow
	#
	# One SSH session sending body; unit is what the launched unit does.
	# ================
	def follow(self, body, unit=None, running=lambda job: False):
		def launch(job):
			self.jobs.append(job)
			if unit:
				unit(job)

		def upload(stream, path):
			Path(path).write_bytes(body)

		with patch.object(receiver, "receive", side_effect=upload):
			return receiver.serve_detached(self.config, launch, running, poll=0)

	# ================
	# run_unit
	# ================
	def run_unit(self, job):
		receiver.run_job(self.config, job, lock=contextlib.nullcontext)

	# ================
	# test_the_unit_does_the_work_and_the_follower_prints_its_result
	# ================
	def test_the_unit_does_the_work_and_the_follower_prints_its_result(self):
		request = {"operation": "confirm-coordinated", "nonce": "c" * 32}
		result = self.follow(json.dumps(request).encode(), self.run_unit)
		self.assertEqual(result, {"phase": "verifying"})
		self.assertEqual(self.handled, [("publish", request)])
		self.assertEqual(json.loads(self.printed[-1]), {"phase": "verifying"})
		job = self.jobs[0]
		self.assertRegex(job, "^[0-9a-f]{32}$")
		filed, recorded = receiver.job_paths(self.config, job)
		self.assertEqual(filed.parent, self.requests)
		# The finished job keeps what was asked, without the nonce.
		self.assertEqual(json.loads(filed.read_text()), {"operation": "confirm-coordinated"})
		self.assertEqual(json.loads(recorded.read_text()), {"ok": True, "result": {"phase": "verifying"}})

	# ================
	# test_losing_the_follower_never_stops_the_work
	#
	# The SSH session drops while the unit waits for the lock: the follower
	# dies, and the unit still runs the filed request and records its result.
	# ================
	def test_losing_the_follower_never_stops_the_work(self):
		def hang_up(job):
			raise KeyboardInterrupt("session closed")

		with self.assertRaises(KeyboardInterrupt):
			self.follow(json.dumps({"operation": "publish-server", "candidate": "b" * 64}).encode(), running=hang_up)
		self.assertEqual(self.handled, [])
		self.run_unit(self.jobs[0])
		self.assertEqual(self.handled, [("publish", {"operation": "publish-server", "candidate": "b" * 64})])
		_, recorded = receiver.job_paths(self.config, self.jobs[0])
		self.assertTrue(json.loads(recorded.read_text())["ok"])

	# ================
	# test_a_failed_job_reports_its_masked_reason
	# ================
	def test_a_failed_job_reports_its_masked_reason(self):
		def failing_unit(job):
			with self.assertRaisesRegex(RuntimeError, "Nomad refused"):
				self.run_unit(job)

		with self.assertRaisesRegex(RuntimeError, "Nomad refused; token <masked>"):
			self.follow(json.dumps({"operation": "publish-server", "fail": True}).encode(), failing_unit)
		_, recorded = receiver.job_paths(self.config, self.jobs[0])
		self.assertNotIn("f" * 64, recorded.read_text())

	# ================
	# test_the_follower_relays_a_unit_failure_without_logging_it_again
	#
	# The unit journaled the reason itself; the publish role's follower only
	# relays it to the SSH session, so the host journal holds it once.
	# ================
	def test_the_follower_relays_a_unit_failure_without_logging_it_again(self):
		logged = []
		with patch.object(receiver, "serve_detached", side_effect=receiver.UnitFailure("Nomad refused")), \
			patch.object(receiver, "CONFIG", Path(__file__)), \
			patch.object(receiver.json, "loads", return_value={}), \
			patch.object(receiver.os, "geteuid", return_value=0, create=True), \
			patch.object(receiver.sys, "argv", ["receiver.py", "publish"]), \
			patch.object(receiver, "syslog", SimpleNamespace(LOG_ERR=3, openlog=lambda name: None,
				syslog=lambda level, text: logged.append(text))):
			with self.assertRaisesRegex(RuntimeError, "Nomad refused"):
				receiver.main()
		self.assertEqual(logged, [])

	# ================
	# test_a_unit_that_ends_without_a_result_is_reported
	# ================
	def test_a_unit_that_ends_without_a_result_is_reported(self):
		with self.assertRaisesRegex(RuntimeError, "ended without a result"):
			self.follow(b'{"operation": "publish-client", "candidate": "' + b"b" * 64 + b'"}')

	# ================
	# test_the_publication_key_still_sends_only_bounded_requests
	# ================
	def test_the_publication_key_still_sends_only_bounded_requests(self):
		with self.assertRaisesRegex(ValueError, "only an approved candidate identity"):
			self.follow(b"\x1f\x8b archive bytes", self.run_unit)
		with self.assertRaisesRegex(ValueError, "exceeds limit"):
			self.follow(b"{" + b" " * receiver.MAX_REQUEST_BYTES + b"}", self.run_unit)
		self.assertEqual((self.jobs, self.handled), ([], []))

	# ================
	# test_a_job_is_named_only_by_the_receiver
	# ================
	def test_a_job_is_named_only_by_the_receiver(self):
		for job in ("../production", "A" * 32, "a" * 31, None):
			with self.subTest(job=job), self.assertRaisesRegex(ValueError, "invalid release job"):
				receiver.run_job(self.config, job, lock=contextlib.nullcontext)

	# ================
	# test_the_deadman_never_waits_for_a_release_step
	#
	# While a step holds the host lock, nothing is read or reverted. With the
	# lock free it expires the journal and prunes week-old filed jobs.
	# ================
	def test_the_deadman_never_waits_for_a_release_step(self):
		self.requests.mkdir()
		old = self.requests / ("a" * 32 + ".json")
		recent = self.requests / ("b" * 32 + ".result.json")
		for path in (old, recent):
			path.write_text("{}")
		now = 2e9
		os.utime(old, (now - receiver.JOB_RETENTION_SECONDS - 1,) * 2)
		os.utime(recent, (now - 60,) * 2)
		with patch.object(receiver.coordinated, "expire", return_value="reverted") as expire:
			self.assertIsNone(receiver.deadman(self.config, lock=lambda blocking: None, now=lambda: now))
			expire.assert_not_called()
			self.assertTrue(old.exists())
			outcome = receiver.deadman(self.config, lock=lambda blocking: contextlib.nullcontext(), now=lambda: now)
			self.assertEqual(outcome, "reverted")
			expire.assert_called_once_with(self.config, now)
		self.assertEqual((old.exists(), recent.exists()), (False, True))

	# ================
	# test_host_roles_are_dispatched_from_root
	#
	# run and deadman are named only by the unit and the timer; the SSH
	# forced commands name stage or publish.
	# ================
	def test_host_roles_are_dispatched_from_root(self):
		for argv, owner, arguments in ((["run", "c" * 32], "run_job", ("c" * 32,)), (["deadman"], "deadman", ()),
			(["publish"], "serve_detached", ()), (["stage"], "serve", ("stage",))):
			with self.subTest(argv=argv), patch.object(receiver, owner) as called, \
				patch.object(receiver, "CONFIG", Path(__file__)), \
				patch.object(receiver.json, "loads", return_value={}), \
				patch.object(receiver.os, "geteuid", return_value=0, create=True), \
				patch.object(receiver.sys, "argv", ["receiver.py", *argv]):
				receiver.main()
				called.assert_called_once_with({}, *arguments)


if __name__ == "__main__":
	unittest.main()
