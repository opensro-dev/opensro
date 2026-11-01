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
		staged.assert_called_once_with(CLIENT, False, SERVER)
		self.assertEqual([call.args for call in current.call_args_list], [("server", COMMIT), ("client", "6" * 40)])
		nonce = json.loads(Path("operation.json").read_text())["nonce"]
		self.assertRegex(nonce, "^[0-9a-f]{32}$")
		self.assertEqual(self.sent, [("publish", {"operation": "publish-coordinated", "server": SERVER, "client": CLIENT,
			"nonce": nonce})])
		self.assertEqual(json.loads(Path("candidate.json").read_text()), client)

	# ================
	# test_the_nonce_is_kept_before_the_request_is_sent
	#
	# A publisher whose connection drops after the host opened the operation
	# still holds the nonce that confirms or reverts it.
	# ================
	def test_the_nonce_is_kept_before_the_request_is_sent(self):
		def lost(request, role):
			self.sent.append(json.loads(Path("operation.json").read_text())["nonce"])
			raise RuntimeError("connection dropped")

		with patch.object(sys, "argv", ["ci.py", "coordinate", SERVER, CLIENT, "--commit", COMMIT]), \
			patch.object(ci, "send", side_effect=lost), patch.object(ci, "require_current"), \
			patch.object(ci, "staged_client_commit", return_value=COMMIT):
			with self.assertRaisesRegex(RuntimeError, "dropped"):
				ci.main()
		self.assertEqual(self.sent, [json.loads(Path("operation.json").read_text())["nonce"]])

	# ================
	# test_confirm_revert_and_resume_use_the_publication_key_without_evidence
	#
	# Evidence goes through the staging key (evidence), never with confirm.
	# All three echo the operation's nonce, from operation.json or --nonce.
	# ================
	def test_confirm_revert_and_resume_use_the_publication_key_without_evidence(self):
		Path("operation.json").write_text(json.dumps({"nonce": "a" * 32}))
		self.run_command("confirm")
		self.run_command("revert", "--reason", "smoke failed", "--nonce", "b" * 32)
		self.run_command("resume")
		self.assertEqual(self.sent, [
			("publish", {"operation": "confirm-coordinated", "nonce": "a" * 32}),
			("publish", {"operation": "revert-coordinated", "reason": "smoke failed", "nonce": "b" * 32}),
			("publish", {"operation": "resume-coordinated", "nonce": "a" * 32}),
		])

	# ================
	# test_a_lost_nonce_needs_an_explicit_takeover
	# ================
	def test_a_lost_nonce_needs_an_explicit_takeover(self):
		with self.assertRaisesRegex(ValueError, "--takeover"):
			self.run_command("confirm")
		self.run_command("revert", "--reason", "smoke failed", "--takeover", "publisher PC lost its nonce")
		self.assertEqual(self.sent, [
			("publish", {"operation": "revert-coordinated", "reason": "smoke failed",
				"takeover": "publisher PC lost its nonce"}),
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


	# ================
	# test_a_maintenance_pair_needs_two_maintenance_candidates
	#
	# --maintenance only asserts what both staged plans declare.
	# ================
	def test_a_maintenance_pair_needs_two_maintenance_candidates(self):
		client = {"candidate": CLIENT, "component": "client", "commit": COMMIT, "coordinated": True, "maintenance": True}
		server = {"candidate": SERVER, "component": "server", "commit": COMMIT, "coordinated": True, "maintenance": False}
		with self.assertRaisesRegex(ValueError, "maintenance candidates"):
			ci.staged_client_commit(CLIENT, True, SERVER, [client, server])
		server["maintenance"] = True
		self.assertEqual(ci.staged_client_commit(CLIENT, True, SERVER, [client, server]), COMMIT)
		with self.assertRaisesRegex(ValueError, "maintenance candidates"):
			ci.staged_client_commit(CLIENT, True, SERVER, [client])
		client["maintenance"] = False
		with self.assertRaisesRegex(ValueError, "maintenance candidates"):
			ci.staged_client_commit(CLIENT, True, SERVER, [client, server])
		self.assertEqual(ci.staged_client_commit(CLIENT, False, SERVER, [client, server]), COMMIT)



# ================
# TransportTests
#
# The same transport from a workflow (secrets as text) and from the
# operator's machine (key files and a local proxy); ssh itself is injected.
# ================
class TransportTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		self.root = Path(directory.name)
		self.request = self.root / "request.json"
		self.request.write_text("{}")
		for name in ("RELEASE_KEY", "RELEASE_KEY_FILE", "RELEASE_KNOWN_HOSTS", "RELEASE_KNOWN_HOSTS_FILE",
			"RELEASE_SSH_PROXY_COMMAND", "GITHUB_OUTPUT"):
			patcher = patch.dict(os.environ, {name: ""})
			patcher.start()
			self.addCleanup(patcher.stop)
		patcher = patch.dict(os.environ, {"RELEASE_HOST": "release.example"})
		patcher.start()
		self.addCleanup(patcher.stop)

	# ================
	# transfer
	#
	# Run one transfer and return the ssh argv with what each named file held
	# while ssh ran (the temporary ones are gone afterwards).
	# ================
	def transfer(self, role="stage"):
		seen = {}

		# ================
		# fake_run
		# ================
		def fake_run(argv, **options):
			seen["argv"] = argv
			seen["key"] = Path(argv[argv.index("-i") + 1]).read_text()
			known = next(value for value in argv if value.startswith("UserKnownHostsFile="))
			seen["known"] = Path(known.split("=", 1)[1].strip('"')).read_text()
			return ci.subprocess.CompletedProcess(argv, 0, stdout='noise\n{"candidate": "x"}\n')

		with patch.object(ci.subprocess, "run", side_effect=fake_run):
			self.assertEqual(ci.transfer(self.request, role), {"candidate": "x"})
		return seen

	# ================
	# test_workflow_secrets_are_written_privately
	# ================
	def test_workflow_secrets_are_written_privately(self):
		with patch.dict(os.environ, {"RELEASE_KEY": "key text", "RELEASE_KNOWN_HOSTS": "host key"}):
			seen = self.transfer("publish")
		self.assertEqual((seen["key"], seen["known"]), ("key text\n", "host key\n"))
		self.assertEqual(seen["argv"][-1], "sro-release@release.example")
		self.assertIn("StrictHostKeyChecking=yes", seen["argv"])
		self.assertFalse(any(value.startswith("ProxyCommand=") for value in seen["argv"]))

	# ================
	# test_operator_files_and_proxy
	#
	# The operator's own key file is used in place, never copied, and the
	# proxy is one ProxyCommand option with the host key still pinned.
	# ================
	def test_operator_files_and_proxy(self):
		key, known = self.root / "stage_key", self.root / "known_hosts"
		key.write_text("operator key\n")
		known.write_text("pinned host\n")
		proxy = "python relay.py 127.0.0.1 7890 %h %p"
		with patch.dict(os.environ, {"RELEASE_KEY_FILE": str(key), "RELEASE_KNOWN_HOSTS_FILE": str(known),
			"RELEASE_SSH_PROXY_COMMAND": proxy}):
			seen = self.transfer()
		argv = seen["argv"]
		self.assertEqual(argv[argv.index("-i") + 1], str(key))
		self.assertIn('UserKnownHostsFile="' + str(known) + '"', argv)
		self.assertIn("StrictHostKeyChecking=yes", argv)
		self.assertEqual(argv[argv.index("ProxyCommand=" + proxy) - 1], "-o")
		self.assertEqual(argv[-1], "sro-stage@release.example")
		# The operator's ssh config, agent and other known_hosts stay out.
		self.assertEqual(argv[:3], ["ssh", "-F", "none"])
		for option in ("IdentitiesOnly=yes", "IdentityAgent=none", "ForwardAgent=no", "ClearAllForwardings=yes",
			"GlobalKnownHostsFile=none", "UpdateHostKeys=no"):
			self.assertEqual(argv[argv.index(option) - 1], "-o")

	# ================
	# test_missing_or_ambiguous_credentials_are_refused
	# ================
	def test_missing_or_ambiguous_credentials_are_refused(self):
		key = self.root / "key"
		key.write_text("operator key\n")
		cases = (
			({}, "RELEASE_KEY or RELEASE_KEY_FILE"),
			({"RELEASE_KEY": "text", "RELEASE_KEY_FILE": str(key), "RELEASE_KNOWN_HOSTS": "host"}, "both set"),
			({"RELEASE_KEY_FILE": str(self.root / "absent"), "RELEASE_KNOWN_HOSTS": "host"}, "names no file"),
			({"RELEASE_KEY_FILE": str(key)}, "RELEASE_KNOWN_HOSTS or RELEASE_KNOWN_HOSTS_FILE"),
		)
		for values, message in cases:
			with self.subTest(message=message), patch.dict(os.environ, values), \
				patch.object(ci.subprocess, "run", side_effect=AssertionError("ssh ran")):
				with self.assertRaisesRegex(ValueError, message):
					ci.transfer(self.request, "stage")

	# ================
	# test_paths_ssh_would_reinterpret_are_refused
	# ================
	def test_paths_ssh_would_reinterpret_are_refused(self):
		for key, known in (("key%h", "known"), ("key", 'known"x'), ("key", "known%d")):
			with self.subTest(key=key, known=known), self.assertRaises(ValueError):
				ci.ssh_options(key, known)

	# ================
	# test_stage_by_hand_needs_no_job_output
	#
	# Outside a workflow there is no GITHUB_OUTPUT; candidate.json carries
	# the identity. Inside one the job output is still written.
	# ================
	def test_stage_by_hand_needs_no_job_output(self):
		previous = os.getcwd()
		os.chdir(self.root)
		self.addCleanup(os.chdir, previous)
		staged = {"candidate": CLIENT, "release": "4" * 64}
		with patch.object(ci, "transfer", return_value=staged):
			self.assertEqual(ci.stage("client", "candidate.tar"), staged)
			self.assertEqual(json.loads(Path("candidate.json").read_text()), staged)
			output = self.root / "output"
			with patch.dict(os.environ, {"GITHUB_OUTPUT": str(output)}):
				ci.stage("client", "candidate.tar")
			self.assertEqual(output.read_text(), "candidate=" + CLIENT + "\n")


if __name__ == "__main__":
	unittest.main()
