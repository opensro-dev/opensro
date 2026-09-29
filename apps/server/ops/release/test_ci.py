"""
===========================================================================

test_ci.py - the workflow commands send exact identities through the right role.

SSH transfer, freshness and the public candidate list are injected; each
test checks the request the host would receive and the key role it used.

===========================================================================
"""

import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import ci

SERVER = "1" * 64
CLIENT = "2" * 64
COMMIT = "3" * 40


# ================
# CiTests
# ================
class CiTests(unittest.TestCase):
	# ================
	# setUp
	#
	# Each command runs in its own working directory, as a workflow step does.
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		previous = os.getcwd()
		os.chdir(directory.name)
		self.addCleanup(os.chdir, previous)
		self.sent = []
		self.replies = []

	# ================
	# run_command
	#
	# Run one workflow command with the host and freshness checks injected.
	# ================
	def run_command(self, *arguments):
		# ================
		# fake_send
		# ================
		def fake_send(request, role):
			self.sent.append((role, request))
			return self.replies.pop(0) if self.replies else {"ok": True}

		with patch.object(sys, "argv", ["ci.py", *arguments]), patch.object(ci, "send", side_effect=fake_send), \
			patch.object(ci, "require_current") as current, patch("builtins.print"):
			ci.main()
		return current

	# ================
	# test_coordinate_checks_both_sources_and_hands_the_client_to_the_smoke
	# ================
	def test_coordinate_checks_both_sources_and_hands_the_client_to_the_smoke(self):
		client = {"candidate": CLIENT, "release": "4" * 64, "commit": COMMIT, "entrySha256": "5" * 64}
		self.replies.append({"phase": "verifying", "server": {}, "client": client})
		with patch.object(ci, "staged_client_commit", return_value="6" * 40) as staged:
			current = self.run_command("coordinate", SERVER, CLIENT, "--commit", COMMIT)
		staged.assert_called_once_with(CLIENT)
		self.assertEqual([call.args for call in current.call_args_list], [("server", COMMIT), ("client", "6" * 40)])
		self.assertEqual(self.sent, [("publish", {"operation": "publish-coordinated", "server": SERVER, "client": CLIENT})])
		self.assertEqual(json.loads(Path("candidate.json").read_text()), client)

	# ================
	# test_confirm_and_revert_use_the_publication_key_without_evidence
	#
	# Evidence goes through the staging key (evidence), never with confirm.
	# ================
	def test_confirm_and_revert_use_the_publication_key_without_evidence(self):
		self.run_command("confirm")
		self.run_command("revert", "--reason", "smoke failed")
		self.assertEqual(self.sent, [
			("publish", {"operation": "confirm-coordinated"}),
			("publish", {"operation": "revert-coordinated", "reason": "smoke failed"}),
		])

	# ================
	# test_evidence_keeps_the_staging_key
	# ================
	def test_evidence_keeps_the_staging_key(self):
		Path("report.json").write_text(json.dumps({"candidate": CLIENT}))
		self.run_command("evidence", "client", "report.json")
		self.assertEqual(self.sent, [("stage", {"operation": "client-smoke", "report": {"candidate": CLIENT}})])

	# ================
	# test_only_a_staged_coordinated_client_can_be_paired
	# ================
	def test_only_a_staged_coordinated_client_can_be_paired(self):
		rows = {"candidates": [
			{"candidate": CLIENT, "component": "client", "commit": COMMIT, "coordinated": False},
		]}

		# ================
		# FakeResponse
		#
		# The public candidate list, as urlopen returns it.
		# ================
		class FakeResponse:
			def __enter__(self):
				return self

			def __exit__(self, *_):
				return False

			def read(self):
				return json.dumps(rows).encode()

		with patch.dict(os.environ, {"RELEASE_ORIGIN": "https://example.test"}), \
			patch.object(ci.urllib.request, "urlopen", return_value=FakeResponse()):
			with self.assertRaisesRegex(ValueError, "coordinated candidate"):
				ci.staged_client_commit(CLIENT)
			rows["candidates"][0]["coordinated"] = True
			self.assertEqual(ci.staged_client_commit(CLIENT), COMMIT)


if __name__ == "__main__":
	unittest.main()
