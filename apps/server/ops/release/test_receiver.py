"""
===========================================================================

test_receiver.py - prove that staging credentials cannot publish releases.

Dispatch is tested with injected operation owners. Rejected requests must not
reach either live component, even when they otherwise have a valid identity.

===========================================================================
"""

from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import receiver
from bundle import FILES, bundle
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
				path.write_bytes(("fixture: " + name).encode())
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
	# test_staging_cannot_dispatch_publication
	# ================
	def test_staging_cannot_dispatch_publication(self):
		with patch.object(receiver, "promote") as client, patch.object(receiver, "publish_server") as server, 			patch.object(receiver, "coordinated") as pair:
			for operation in ("publish-client", "publish-server", "rollback-client", "rollback-server",
				"publish-coordinated", "confirm-coordinated", "revert-coordinated"):
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
		failure = RuntimeError("sro-nomad validate refused; token " + token + " " + "x" * 2000)
		with patch.object(receiver, "serve", side_effect=failure), 			patch.object(receiver, "CONFIG", Path(__file__)), 			patch.object(receiver.json, "loads", return_value={}), 			patch.object(receiver.os, "geteuid", return_value=0, create=True), 			patch.object(receiver.sys, "argv", ["receiver.py", "publish"]), 			patch.object(receiver, "syslog", SimpleNamespace(LOG_ERR=3, openlog=lambda name: None,
				syslog=lambda level, text: logged.append((level, text)))):
			with self.assertRaises(RuntimeError):
				receiver.main()
		self.assertEqual(len(logged), 1)
		level, text = logged[0]
		self.assertEqual(level, 3)
		self.assertTrue(text.startswith("publish request failed: RuntimeError: sro-nomad validate refused"))
		self.assertNotIn(token, text)
		self.assertIn("<masked>", text)
		self.assertLessEqual(len(text), len("publish request failed: ") + receiver.MAX_FAILURE_LOG_CHARS)


if __name__ == "__main__":
	unittest.main()
