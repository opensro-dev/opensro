"""
===========================================================================

test_coordinated.py - a protocol change publishes server and client together.

Admission lets a server drop protocols only as half of a pair, and only when
the old server could be restored; a maintenance pair may also upgrade the
store, because its revert restores the journaled backups. The flow tests
drive the real journal, retention, staging and symlink switch, with Nomad and
the HTTPS edge injected: a pair is confirmed by post-switch evidence or
reverted as a pair, and a maintenance pair keeps the gate closed until then.

===========================================================================
"""

import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from bundle import FILES, bundle
from test_bundle import go_binary
import coordinated
import deploy
from release_state import admit, admit_maintenance, admit_pair, begin, begin_pair, read_state, write_state
import receiver
from test_client_data import DATA_SCHEMA, LiveDataFixture
from test_release_state import NEW_COMMIT, candidate, production

NEXT_PROTOCOL = 3
NEXT_SERVER = {"protocolMin": NEXT_PROTOCOL, "protocolMax": NEXT_PROTOCOL,
	"storeReadMin": 13, "storeReadMax": 13, "storeWrite": 13}
# A server that only opens an upgraded store (the live one writes 13).
UPGRADING_SERVER = dict(NEXT_SERVER, storeReadMin=14, storeReadMax=14, storeWrite=14, storeUpgradeFrom=13)
PROBE = "release-probe"
NONCE = "c" * 32


# ================
# server_plan
# ================
def server_plan(**changes):
	plan = candidate("server")
	plan.update(release=NEW_COMMIT, compatibility=dict(NEXT_SERVER), coordinated=True)
	plan.update(changes)
	return plan


# ================
# client_plan
# ================
def client_plan(**changes):
	plan = candidate()
	plan.update(compatibility={"protocol": NEXT_PROTOCOL, "assetSchema": DATA_SCHEMA}, kind="data", coordinated=True)
	plan.update(changes)
	return plan


# ================
# PairAdmissionTests
# ================
class PairAdmissionTests(unittest.TestCase):
	# ================
	# test_a_protocol_change_is_admitted_only_as_a_pair
	# ================
	def test_a_protocol_change_is_admitted_only_as_a_pair(self):
		alone = server_plan()
		del alone["coordinated"]
		with self.assertRaisesRegex(ValueError, "incompatible"):
			admit(production(), alone)
		# Staging admits each half for its own rules only.
		self.assertEqual(admit(production(), server_plan()), "server")
		self.assertEqual(admit(production(), client_plan()), "client")
		with self.assertRaisesRegex(ValueError, "counterpart"):
			begin(production(), server_plan(), "start")
		admit_pair(production(), server_plan(), client_plan())
		with self.assertRaisesRegex(ValueError, "incompatible"):
			admit_pair(production(), server_plan(), client_plan(compatibility={"protocol": 2, "assetSchema": DATA_SCHEMA}))

	# ================
	# test_both_halves_must_be_built_for_the_pair
	# ================
	def test_both_halves_must_be_built_for_the_pair(self):
		client = client_plan()
		del client["coordinated"]
		with self.assertRaisesRegex(ValueError, "both candidates"):
			admit_pair(production(), server_plan(), client)
		with self.assertRaisesRegex(ValueError, "pairs one server"):
			admit_pair(production(), client_plan(), client_plan())
		with self.assertRaisesRegex(ValueError, "coordination"):
			admit(production(), server_plan(coordinated=False))

	# ================
	# test_the_old_server_must_read_what_the_new_one_writes
	# ================
	def test_the_old_server_must_read_what_the_new_one_writes(self):
		upgraded = dict(NEXT_SERVER, storeReadMax=14, storeWrite=14)
		with self.assertRaisesRegex(ValueError, "revert"):
			admit_pair(production(), server_plan(compatibility=upgraded), client_plan())

	# ================
	# test_a_rollback_is_never_coordinated
	# ================
	def test_a_rollback_is_never_coordinated(self):
		with self.assertRaisesRegex(ValueError, "rollback restores one"):
			admit(production(), server_plan(mode="rollback", reason="test"))


# ================
# MaintenanceAdmissionTests
# ================
class MaintenanceAdmissionTests(unittest.TestCase):
	# ================
	# test_a_maintenance_pair_may_upgrade_the_store
	#
	# The plain pair is refused (its revert cannot restore a database); the
	# maintenance pair is admitted and journals the shards it will upgrade.
	# ================
	def test_a_maintenance_pair_may_upgrade_the_store(self):
		server = server_plan(compatibility=dict(UPGRADING_SERVER), maintenance=True)
		client = client_plan(maintenance=True)
		with self.assertRaisesRegex(ValueError, "revert"):
			admit_pair(production(), server_plan(compatibility=dict(UPGRADING_SERVER)), client_plan())
		admit_maintenance(production(), server, client)
		pending = begin_pair(production(), server, client, 1.0)
		self.assertEqual((pending["operation"]["maintenance"], pending["operation"]["authorities"]), (True, {}))
		plain = begin_pair(production(), server_plan(), client_plan(), 1.0)
		self.assertNotIn("maintenance", plain["operation"])

	# ================
	# test_both_halves_must_declare_maintenance
	# ================
	def test_both_halves_must_declare_maintenance(self):
		server = server_plan(compatibility=dict(UPGRADING_SERVER), maintenance=True)
		for client in (client_plan(), client_plan(maintenance=False)):
			with self.assertRaisesRegex(ValueError, "both candidates must be built for the maintenance"):
				begin_pair(production(), server, client, 1.0)
		alone = server_plan(maintenance=True)
		del alone["coordinated"]
		with self.assertRaisesRegex(ValueError, "maintenance declaration"):
			admit(production(), alone)
		with self.assertRaisesRegex(ValueError, "maintenance declaration"):
			admit(production(), server_plan(maintenance="yes"))
		with self.assertRaisesRegex(ValueError, "pairs one server"):
			admit_maintenance(production(), client_plan(maintenance=True), client_plan(maintenance=True))

	# ================
	# test_maintenance_never_downgrades_or_skips_an_upgrade
	#
	# The server must still take the live schema over: no downgrade, and an
	# offline upgrade must reach back to the live schema.
	# ================
	def test_maintenance_never_downgrades_or_skips_an_upgrade(self):
		client = client_plan(maintenance=True)
		unreachable = dict(UPGRADING_SERVER, storeUpgradeFrom=14)
		with self.assertRaisesRegex(ValueError, "cannot read or upgrade"):
			admit_maintenance(production(), server_plan(compatibility=unreachable, maintenance=True), client)
		older = dict(NEXT_SERVER, storeReadMin=12, storeReadMax=13, storeWrite=12)
		with self.assertRaisesRegex(ValueError, "downgrade"):
			admit_maintenance(production(), server_plan(compatibility=older, maintenance=True), client)
		with self.assertRaisesRegex(ValueError, "incompatible"):
			admit_maintenance(production(), server_plan(compatibility=dict(UPGRADING_SERVER), maintenance=True),
				client_plan(maintenance=True, compatibility={"protocol": 2, "assetSchema": DATA_SCHEMA}))


# ================
# PairFixture
#
# A live client and server, a staged coordinated data client and a staged,
# Linux-verified coordinated server.
# ================
class PairFixture(LiveDataFixture):
	CONTRACT = {"protocol": NEXT_PROTOCOL, "assetSchema": DATA_SCHEMA}
	INTENT = {"coordinated": True}
	SERVER = {}

	# ================
	# setUp
	# ================
	def setUp(self):
		super().setUp()
		self.module = self.root / "module"
		live = {"format": "opensro-server-v1", "commit": self.state["server"]["commit"], "files": {}}
		build = self.root / "build"
		for name, source in FILES.items():
			(self.module / name).parent.mkdir(parents=True, exist_ok=True)
			(self.module / name).write_bytes(b"live server " + name.encode())
			live["files"][name] = hashlib.sha256((self.module / name).read_bytes()).hexdigest()
			(build / source).parent.mkdir(parents=True, exist_ok=True)
			(build / source).write_bytes(go_binary(name, NEW_COMMIT) if source.startswith("bin/") else b"next server " + name.encode())
		write_state(self.module / "release.json", live)
		self.config.update(module=str(self.module), staff_webhook="unused")
		archive = self.root / "server.tar"
		bundle(build, archive, NEW_COMMIT, server_plan(**self.SERVER))
		staged = receiver.stage_server(self.config, archive, self.root / "stage-scratch")
		self.server = staged["candidate"]
		write_state(self.root / "records" / self.server / "smoke.json",
			{**staged, "linuxTests": True, "verdict": "PASS"})
		self.client = self.staged["candidate"]
		self.rollouts = []
		self.deployments = []
		self.journaled = []
		self.shards = ("global-official", "second")
		self.failing_entries = set()
		for target, replacement in ((deploy, "deploy"), (deploy, "alert_staff")):
			patcher = patch.object(target, replacement, side_effect=getattr(self, "fake_" + replacement))
			patcher.start()
			self.addCleanup(patcher.stop)

	# ================
	# fake_deploy
	#
	# Nomad: record what was rolled out and write the release record, as a
	# healthy rollout does.
	# ================
	def fake_deploy(self, _config, staging, manifest, notice=True, upgrade=False, on_upgrade=None, restore=None,
		maintenance=False):
		self.rollouts.append((manifest["commit"], notice, (Path(staging) / "agent").read_bytes()))
		self.deployments.append({"upgrade": upgrade, "restore": restore, "maintenance": maintenance})
		if upgrade and on_upgrade:
			for shard in self.shards:
				on_upgrade(shard, None)
				self.journaled.append(read_state(self.config["production_state"])["operation"]["authorities"][shard])
				on_upgrade(shard, {"backup": "state.before-upgrade-1.db", "sha256": "f" * 64})
		write_state(self.module / "release.json", manifest)

	# ================
	# fake_alert_staff
	# ================
	def fake_alert_staff(self, _config, _message):
		pass

	# ================
	# edge
	# ================
	def edge(self, _origin, entry):
		if entry in self.failing_entries:
			raise RuntimeError("edge served a different entry")

	# ================
	# publish
	#
	# Each receiver invocation has its own scratch directory.
	# ================
	def publish(self):
		request = {"server": self.server, "client": self.client, "nonce": NONCE}
		return coordinated.publish(self.config, request, tempfile.mkdtemp(dir=self.root), self.edge)

	# ================
	# confirm
	#
	# The publisher's confirm, echoing its nonce.
	# ================
	def confirm(self, **claim):
		return coordinated.confirm(self.config, claim or {"nonce": NONCE})

	# ================
	# report
	# ================
	def report(self):
		return {**self.staged, "verdict": "PASS", "errors": [],
			"phases": {name: "PASS" for name in ("title", "login", "roster", "world", "gameplay", "resume")}}

	# ================
	# record_evidence
	#
	# The smoke job records its report with the staging key.
	# ================
	def record_evidence(self, report):
		request = {"operation": "client-smoke", "report": report}
		return receiver.request(self.config, "stage", request, self.root / "unused")

	# ================
	# live_client
	# ================
	def live_client(self):
		return Path(self.config["client_link"]).resolve()


# ================
# CoordinatedFlowTests
# ================
@unittest.skipIf(os.name == "nt", "publication swaps POSIX directory symlinks")
class CoordinatedFlowTests(PairFixture, unittest.TestCase):

	# ================
	# test_the_pair_goes_live_and_confirms_together
	# ================
	def test_the_pair_goes_live_and_confirms_together(self):
		result = self.publish()
		self.assertEqual(result["phase"], "verifying")
		self.assertEqual(self.rollouts, [(NEW_COMMIT, True, go_binary("agent", NEW_COMMIT))])
		self.assertEqual(self.deployments, [{"upgrade": False, "restore": None, "maintenance": False}])
		self.assertEqual(self.live_client(), self.tree.resolve())
		pending = read_state(self.config["production_state"])
		self.assertEqual(pending["operation"]["phase"], "verifying")
		self.assertEqual(pending["server"], self.state["server"])
		with self.assertRaisesRegex(ValueError, "reconciliation"):
			admit(pending, client_plan(release=self.next["releaseId"], baseRelease=self.base["releaseId"]))
		self.record_evidence(self.report())
		state = self.confirm()
		self.assertIsNone(state["operation"])
		self.assertEqual((state["server"]["release"], state["server"]["generation"]), (NEW_COMMIT, 2))
		self.assertEqual((state["client"]["release"], state["client"]["generation"]), (self.next["releaseId"], 2))
		self.assertEqual(state["client"]["candidate"], self.client)
		self.assertEqual(state["server"]["compatibility"]["protocolMin"], NEXT_PROTOCOL)
		self.assertEqual({row["component"] for row in state["history"]}, {"client", "server"})

	# ================
	# test_failed_evidence_reverts_both_and_voids_the_approval
	# ================
	def test_failed_evidence_reverts_both_and_voids_the_approval(self):
		self.publish()
		state = coordinated.revert(self.config, "browser smoke failed", self.edge)
		self.assertEqual(self.live_client(), self.live.resolve())
		self.assertEqual(json.loads(Path(self.config["client_manifest"]).read_text()), self.base)
		self.assertEqual(self.rollouts[-1], (self.state["server"]["commit"], False, b"live server agent"))
		self.assertEqual(state["server"]["release"], self.state["server"]["release"])
		self.assertEqual(state["client"]["release"], self.base["releaseId"])
		self.assertEqual((state["server"]["generation"], state["client"]["generation"]), (2, 2))
		self.assertEqual(state["lastFailure"]["reason"], "browser smoke failed")
		with self.assertRaisesRegex(ValueError, "superseded"):
			self.publish()

	# ================
	# test_a_client_the_edge_does_not_serve_reverts_at_once
	# ================
	def test_a_client_the_edge_does_not_serve_reverts_at_once(self):
		self.failing_entries.add(self.staged["entrySha256"])
		with self.assertRaisesRegex(RuntimeError, "different entry"):
			self.publish()
		state = read_state(self.config["production_state"])
		self.assertIsNone(state["operation"])
		self.assertEqual(self.live_client(), self.live.resolve())
		self.assertEqual([notice for _, notice, _ in self.rollouts], [True, False])

	# ================
	# test_a_failed_server_rollout_waits_for_an_explicit_revert
	# ================
	def test_a_failed_server_rollout_waits_for_an_explicit_revert(self):
		deploy.deploy.side_effect = RuntimeError("Nomad health failed")
		with self.assertRaisesRegex(RuntimeError, "health failed"):
			self.publish()
		state = read_state(self.config["production_state"])
		self.assertEqual(state["operation"]["phase"], "failed")
		self.assertEqual(self.live_client(), self.live.resolve())
		deploy.deploy.side_effect = self.fake_deploy
		coordinated.revert(self.config, "server rollout failed", self.edge)
		self.assertEqual(self.rollouts, [(self.state["server"]["commit"], False, b"live server agent")])
		self.assertIsNone(read_state(self.config["production_state"])["operation"])

	# ================
	# test_the_public_list_shows_the_pair_ready_with_the_client_entry
	#
	# Workflows find a staged client and its entry digest in this list.
	# ================
	def test_the_public_list_shows_the_pair_ready_with_the_client_entry(self):
		receiver.candidate_status(self.config)
		listing = json.loads((self.root / "candidates.json").read_text())
		rows = {row["candidate"]: row for row in listing["candidates"]}
		self.assertEqual(rows[self.client]["phase"], "ready-for-approval")
		self.assertTrue(rows[self.client]["coordinated"])
		self.assertEqual(rows[self.client]["entrySha256"], self.staged["entrySha256"])
		self.assertEqual(rows[self.server]["phase"], "ready-for-approval")
		self.assertTrue(rows[self.server]["restartRequired"])
		self.assertNotIn("entrySha256", rows[self.server])

	# ================
	# test_confirmation_needs_evidence_recorded_for_the_live_pair
	# ================
	def test_confirmation_needs_evidence_recorded_for_the_live_pair(self):
		with self.assertRaisesRegex(ValueError, "no coordinated release"):
			self.confirm()
		self.publish()
		with self.assertRaisesRegex(ValueError, "no browser evidence"):
			self.confirm()
		failing = self.report()
		failing["phases"]["world"] = "FAIL"
		with self.assertRaisesRegex(ValueError, "missing phase"):
			self.record_evidence(failing)
		# Evidence older than the switch cannot describe the live pair.
		self.record_evidence(self.report())
		evidence = self.root / "records" / self.client / "smoke.json"
		started = read_state(self.config["production_state"])["operation"]["phaseStartedAt"]
		os.utime(evidence, (started - 60, started - 60))
		with self.assertRaisesRegex(ValueError, "no browser evidence"):
			self.confirm()
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "verifying")
		with self.assertRaisesRegex(ValueError, "not allowed"):
			receiver.request(self.config, "stage", {"operation": "confirm-coordinated"}, self.root / "unused")

	# ================
	# fail_the_revert
	#
	# The revert restores the client but the retained server refuses to
	# redeploy: the journal waits in revert-failed with the candidate
	# server still live.
	# ================
	def fail_the_revert(self):
		original = deploy.deploy.side_effect
		deploy.deploy.side_effect = RuntimeError("Nomad refuses the retained server")
		with self.assertRaisesRegex(RuntimeError, "refuses the retained"):
			coordinated.revert(self.config, "browser smoke failed", self.edge)
		deploy.deploy.side_effect = original
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "revert-failed")
		self.assertEqual(self.live_client(), self.live.resolve())

	# ================
	# test_a_failed_revert_resumes_forward
	#
	# Resume reinstalls the candidate client and returns to verifying;
	# only evidence recorded after the resume confirms the pair (the
	# earlier evidence predates the phase and confirm refuses it).
	# ================
	def test_a_failed_revert_resumes_forward(self):
		self.publish()
		self.record_evidence(self.report())
		self.fail_the_revert()
		result = coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.assertEqual(result["phase"], "verifying")
		self.assertEqual(self.live_client(), self.tree.resolve())
		pending = read_state(self.config["production_state"])
		self.assertEqual(pending["operation"]["phase"], "verifying")
		self.assertEqual(len(pending["operation"]["resumes"]), 1)
		with self.assertRaisesRegex(ValueError, "no browser evidence"):
			self.confirm()
		self.record_evidence(self.report())
		self.assertIsNone(self.confirm()["operation"])

	# ================
	# test_resume_refuses_without_a_failed_revert
	# ================
	def test_resume_refuses_without_a_failed_revert(self):
		with self.assertRaisesRegex(ValueError, "no coordinated release"):
			coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.publish()
		with self.assertRaisesRegex(ValueError, "not in a failed revert"):
			coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "verifying")
		self.assertEqual(self.live_client(), self.tree.resolve())

	# ================
	# test_resume_refuses_when_the_retained_server_is_live
	#
	# Forward recovery exists only while the candidate server is the
	# live one; a retained server back in place is an operator's state.
	# ================
	def test_resume_refuses_when_the_retained_server_is_live(self):
		self.publish()
		self.fail_the_revert()
		retained = json.loads((self.module / "release.json").read_text())
		retained["commit"] = self.state["server"]["commit"]
		write_state(self.module / "release.json", retained)
		with self.assertRaisesRegex(RuntimeError, "live server is not the release being resumed"):
			coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "revert-failed")
		self.assertEqual(self.live_client(), self.live.resolve())

	# ================
	# test_resume_refuses_a_changed_staged_client
	# ================
	def test_resume_refuses_a_changed_staged_client(self):
		self.publish()
		self.fail_the_revert()
		(self.tree / "index.html").write_bytes(b"tampered")
		with self.assertRaisesRegex(ValueError, "changed after validation"):
			coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "revert-failed")
		self.assertEqual(self.live_client(), self.live.resolve())

	# ================
	# test_resume_is_a_publication_key_operation
	#
	# The staging key cannot resume; the publication key reaches the
	# resume (and its own refusal while no revert has failed).
	# ================
	def test_resume_is_a_publication_key_operation(self):
		self.publish()
		with self.assertRaisesRegex(ValueError, "not allowed"):
			receiver.request(self.config, "stage", {"operation": "resume-coordinated", "nonce": NONCE},
				self.root / "unused")
		with self.assertRaisesRegex(ValueError, "not in a failed revert"):
			receiver.request(self.config, "publish", {"operation": "resume-coordinated", "nonce": NONCE},
				self.root / "unused")


# ================
# OwnershipTests
#
# Only the publisher that opened an operation ends it, unless an operator
# takes it over with a reason; the public journal never holds the nonce.
# ================
@unittest.skipIf(os.name == "nt", "publication swaps POSIX directory symlinks")
class OwnershipTests(PairFixture, unittest.TestCase):

	# ================
	# setUp
	# ================
	def setUp(self):
		super().setUp()
		self.alerts = []
		deploy.alert_staff.side_effect = lambda _config, message: self.alerts.append(message)

	# ================
	# test_the_journal_keeps_only_the_nonce_digest
	# ================
	def test_the_journal_keeps_only_the_nonce_digest(self):
		self.publish()
		journal = Path(self.config["production_state"]).read_text()
		self.assertNotIn(NONCE, journal)
		self.assertEqual(read_state(self.config["production_state"])["operation"]["nonceSha256"],
			hashlib.sha256(NONCE.encode()).hexdigest())

	# ================
	# test_a_publish_without_a_nonce_changes_nothing
	# ================
	def test_a_publish_without_a_nonce_changes_nothing(self):
		for nonce in (None, "C" * 32, "c" * 31, 7):
			with self.subTest(nonce=nonce):
				request = {"server": self.server, "client": self.client, "nonce": nonce}
				with self.assertRaisesRegex(ValueError, "nonce"):
					coordinated.publish(self.config, request, tempfile.mkdtemp(dir=self.root), self.edge)
		self.assertIsNone(read_state(self.config["production_state"])["operation"])
		self.assertEqual(self.rollouts, [])

	# ================
	# test_another_publisher_cannot_confirm_or_revert
	#
	# A stale or missing nonce leaves the operation exactly as it was.
	# ================
	def test_another_publisher_cannot_confirm_or_revert(self):
		self.publish()
		self.record_evidence(self.report())
		before = read_state(self.config["production_state"])
		for claim in ({}, {"nonce": "d" * 32}):
			with self.subTest(claim=claim):
				with self.assertRaisesRegex(ValueError, "another publisher"):
					coordinated.confirm(self.config, claim)
				with self.assertRaisesRegex(ValueError, "another publisher"):
					coordinated.revert_request(self.config, {"reason": "smoke failed", **claim}, self.edge)
		self.assertEqual(read_state(self.config["production_state"]), before)
		self.assertEqual(self.rollouts, [(NEW_COMMIT, True, go_binary("agent", NEW_COMMIT))])
		self.assertIsNone(self.confirm()["operation"])

	# ================
	# test_a_takeover_resume_is_journaled_and_alerted
	#
	# A publisher that did not open the operation cannot resume it; a
	# takeover resumes only with its reason in the journal first.
	# ================
	def test_a_takeover_resume_is_journaled_and_alerted(self):
		self.publish()
		original = deploy.deploy.side_effect
		deploy.deploy.side_effect = RuntimeError("Nomad refuses the retained server")
		with self.assertRaisesRegex(RuntimeError, "refuses the retained"):
			coordinated.revert(self.config, "browser smoke failed", self.edge)
		deploy.deploy.side_effect = original
		with self.assertRaisesRegex(ValueError, "another publisher"):
			coordinated.resume(self.config, {"nonce": "d" * 32}, self.edge)
		result = coordinated.resume(self.config, {"takeover": "the publisher host is gone"}, self.edge)
		self.assertEqual(result["phase"], "verifying")
		operation = read_state(self.config["production_state"])["operation"]
		takeover = operation["takeovers"][-1]
		self.assertEqual((takeover["action"], takeover["reason"]), ("resume", "the publisher host is gone"))
		self.assertEqual(len(operation["resumes"]), 1)
		self.assertTrue(any("resumed to verifying" in message for message in self.alerts))

	# ================
	# test_a_takeover_is_journaled_and_alerted_before_it_acts
	# ================
	def test_a_takeover_is_journaled_and_alerted_before_it_acts(self):
		self.publish()
		with self.assertRaisesRegex(ValueError, "bounded reason"):
			coordinated.revert_request(self.config, {"reason": "smoke failed", "takeover": " "}, self.edge)
		taken = []

		def observed(*arguments, **options):
			taken.append(read_state(self.config["production_state"])["operation"]["takeovers"])
			return self.fake_deploy(*arguments, **options)

		deploy.deploy.side_effect = observed
		state = coordinated.revert_request(self.config, {"reason": "smoke failed", "takeover": "publisher PC died"},
			self.edge)
		self.assertEqual([[row["action"], row["reason"]] for row in taken[0]], [["revert", "publisher PC died"]])
		self.assertIn("Coordinated release revert by takeover: publisher PC died", self.alerts)
		self.assertIsNone(state["operation"])
		self.assertEqual(self.live_client(), self.live.resolve())

	# ================
	# test_a_stale_publisher_cannot_end_the_next_operation
	#
	# The dead-man reverted a pair whose publisher slept, and a new
	# operation opened under another nonce: the woken publisher's confirm is
	# refused.
	# ================
	def test_a_stale_publisher_cannot_end_the_next_operation(self):
		self.publish()
		coordinated.revert(self.config, "dead-man", self.edge)
		state = read_state(self.config["production_state"])
		self.assertIsNone(state["operation"])
		state["operation"] = dict(begin_pair(production(), server_plan(), client_plan(), 1.0)["operation"],
			nonceSha256=hashlib.sha256(("e" * 32).encode()).hexdigest(), phase="verifying")
		write_state(self.config["production_state"], state)
		with self.assertRaisesRegex(ValueError, "another publisher"):
			self.confirm()


# ================
# DeadmanTests
#
# coordinated.expire, the host timer's body: a pair left past the deadline
# is reverted, anything else stale alerts once, and nothing fresh is touched.
# ================
@unittest.skipIf(os.name == "nt", "publication swaps POSIX directory symlinks")
class DeadmanTests(PairFixture, unittest.TestCase):

	# ================
	# setUp
	# ================
	def setUp(self):
		super().setUp()
		self.alerts = []
		deploy.alert_staff.side_effect = lambda _config, message: self.alerts.append(message)

	# ================
	# started
	# ================
	def started(self):
		operation = read_state(self.config["production_state"])["operation"]
		return operation.get("phaseStartedAt", operation["startedAt"])

	# ================
	# expire_in
	#
	# Publish, put the operation in the given phase, and run the dead-man
	# just past its deadline.
	# ================
	def expire_in(self, phase):
		self.publish()
		state = read_state(self.config["production_state"])
		state["operation"]["phase"] = phase
		write_state(self.config["production_state"], state)
		outcome = coordinated.expire(self.config, self.started() + coordinated.DEADMAN_SECONDS + 1, self.edge)
		self.assertEqual(outcome, "reverted")
		state = read_state(self.config["production_state"])
		self.assertIsNone(state["operation"])
		self.assertTrue(state["lastFailure"]["reason"].startswith("dead-man: the release sat in " + phase + " for 30 min"))
		self.assertEqual(self.live_client(), self.live.resolve())
		self.assertEqual(self.rollouts[-1], (self.state["server"]["commit"], False, b"live server agent"))

	# ================
	# test_a_fresh_operation_is_left_alone
	# ================
	def test_a_fresh_operation_is_left_alone(self):
		self.assertIsNone(coordinated.expire(self.config, 1e12, self.edge))
		self.publish()
		deadline = self.started() + coordinated.DEADMAN_SECONDS
		self.assertIsNone(coordinated.expire(self.config, deadline - 1, self.edge))
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "verifying")
		self.assertEqual(len(self.rollouts), 1)

	# ================
	# test_an_unconfirmed_pair_is_reverted_past_the_deadline
	# ================
	def test_an_unconfirmed_pair_is_reverted_past_the_deadline(self):
		self.expire_in("verifying")

	# ================
	# test_a_publish_that_died_mid_deploy_is_reverted
	# ================
	def test_a_publish_that_died_mid_deploy_is_reverted(self):
		self.expire_in("deploying")

	# ================
	# test_a_failed_rollout_is_reverted
	# ================
	def test_a_failed_rollout_is_reverted(self):
		self.expire_in("failed")

	# ================
	# test_an_interrupted_revert_is_reverted_again
	# ================
	def test_an_interrupted_revert_is_reverted_again(self):
		self.expire_in("reverting")

	# ================
	# test_what_cannot_be_reverted_here_alerts_once
	#
	# A failed revert needs an operator; a single component whose receiver
	# died has no host-side revert. Neither is touched beyond one alert.
	# ================
	def test_what_cannot_be_reverted_here_alerts_once(self):
		for operation in ({"component": "release", "phase": "revert-failed", "startedAt": 1.0, "phaseStartedAt": 1.0},
			{"component": "server", "phase": "deploying", "startedAt": 1.0}):
			with self.subTest(operation=operation):
				self.alerts.clear()
				state = read_state(self.config["production_state"])
				state["operation"] = dict(operation)
				write_state(self.config["production_state"], state)
				now = 1.0 + coordinated.DEADMAN_SECONDS + 1
				self.assertEqual(coordinated.expire(self.config, now, self.edge), "alerted")
				self.assertIsNone(coordinated.expire(self.config, now + 600, self.edge))
				self.assertEqual(len(self.alerts), 1)
				self.assertIn("needs an operator", self.alerts[0])
				self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], operation["phase"])
		self.assertEqual(self.rollouts, [])

	# ================
	# test_the_deadline_is_configurable
	# ================
	def test_the_deadline_is_configurable(self):
		self.publish()
		self.config["deadman_seconds"] = 60
		self.assertEqual(coordinated.expire(self.config, self.started() + 61, self.edge), "reverted")


# ================
# MaintenanceFlowTests
#
# The same pair, built for a maintenance release whose server upgrades the
# store: the gate closes before the rollout, every shard's backup is
# journaled before the deploy finishes, confirm opens the gate and revert
# restores the journaled backups before opening it.
# ================
@unittest.skipIf(os.name == "nt", "publication swaps POSIX directory symlinks")
class MaintenanceFlowTests(PairFixture, unittest.TestCase):
	INTENT = {"coordinated": True, "maintenance": True}
	SERVER = {"compatibility": dict(UPGRADING_SERVER), "maintenance": True}

	# ================
	# setUp
	#
	# The gate is a real file; only its root:sro ownership is injected.
	# ================
	def setUp(self):
		super().setUp()
		self.config["maintenance_accounts"] = [PROBE]
		(self.module / ".state/cluster").mkdir(parents=True, exist_ok=True)
		patcher = patch.object(deploy, "own_gate", side_effect=lambda path: None)
		patcher.start()
		self.addCleanup(patcher.stop)

	# ================
	# gate
	# ================
	def gate(self):
		path = self.module / deploy.MAINTENANCE_GATE
		return json.loads(path.read_text()) if path.exists() else None

	# ================
	# test_the_gate_holds_until_confirmation
	# ================
	def test_the_gate_holds_until_confirmation(self):
		result = self.publish()
		self.assertTrue(result["maintenance"])
		# maintenance reaches the deploy, so it may stop for a changed credential.
		self.assertEqual(self.deployments, [{"upgrade": True, "restore": None, "maintenance": True}])
		# Each shard was journaled as started before its backup was recorded.
		self.assertEqual(self.journaled, [None, None])
		pending = read_state(self.config["production_state"])
		self.assertEqual(sorted(pending["operation"]["authorities"]), list(self.shards))
		self.assertEqual(self.gate(), {"accounts": [PROBE]})
		self.record_evidence(self.report())
		state = self.confirm()
		self.assertIsNone(state["operation"])
		self.assertEqual(state["server"]["compatibility"]["storeWrite"], 14)
		self.assertIsNone(self.gate())

	# ================
	# test_revert_restores_the_journaled_backups_then_opens_the_gate
	# ================
	def test_revert_restores_the_journaled_backups_then_opens_the_gate(self):
		self.publish()
		authorities = read_state(self.config["production_state"])["operation"]["authorities"]
		state = coordinated.revert(self.config, "browser smoke failed", self.edge)
		self.assertEqual(self.deployments[-1], {"upgrade": False, "restore": authorities, "maintenance": True})
		self.assertEqual(self.rollouts[-1], (self.state["server"]["commit"], False, b"live server agent"))
		self.assertEqual(state["server"]["compatibility"]["storeWrite"], 13)
		self.assertIsNone(state["operation"])
		self.assertIsNone(self.gate())

	# ================
	# test_a_failed_restore_keeps_everyone_out
	#
	# A refused restore (a changed backup) leaves the journal for an operator
	# and the gate closed.
	# ================
	def test_a_failed_restore_keeps_everyone_out(self):
		self.publish()
		original = deploy.deploy.side_effect
		deploy.deploy.side_effect = RuntimeError("upgrade backup is missing or changed; restore refused")
		with self.assertRaisesRegex(RuntimeError, "restore refused"):
			coordinated.revert(self.config, "browser smoke failed", self.edge)
		deploy.deploy.side_effect = original
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "revert-failed")
		self.assertEqual(self.gate(), {"accounts": [PROBE]})

	# ================
	# test_a_failed_revert_resumes_behind_the_closed_gate
	#
	# Forward recovery never lets players in: an open gate refuses the
	# resume, and the gate stays closed until the resumed pair confirms.
	# ================
	def test_a_failed_revert_resumes_behind_the_closed_gate(self):
		self.publish()
		original = deploy.deploy.side_effect
		deploy.deploy.side_effect = RuntimeError("upgrade backup is missing or changed; restore refused")
		with self.assertRaisesRegex(RuntimeError, "restore refused"):
			coordinated.revert(self.config, "browser smoke failed", self.edge)
		deploy.deploy.side_effect = original
		(self.module / deploy.MAINTENANCE_GATE).unlink()
		with self.assertRaisesRegex(ValueError, "gate is not closed"):
			coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "revert-failed")
		deploy.shut_gate(self.config)
		result = coordinated.resume(self.config, {"nonce": NONCE}, self.edge)
		self.assertEqual(result["phase"], "verifying")
		self.assertEqual(self.gate(), {"accounts": [PROBE]})
		self.record_evidence(self.report())
		self.assertIsNone(self.confirm()["operation"])
		self.assertIsNone(self.gate())

	# ================
	# test_a_failed_rollout_keeps_the_gate_closed
	# ================
	def test_a_failed_rollout_keeps_the_gate_closed(self):
		deploy.deploy.side_effect = RuntimeError("Nomad health failed")
		with self.assertRaisesRegex(RuntimeError, "health failed"):
			self.publish()
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "failed")
		self.assertEqual(self.gate(), {"accounts": [PROBE]})

	# ================
	# test_no_probe_account_refuses_before_anything_changes
	# ================
	def test_no_probe_account_refuses_before_anything_changes(self):
		del self.config["maintenance_accounts"]
		with self.assertRaisesRegex(ValueError, "maintenance_accounts"):
			self.publish()
		self.assertIsNone(read_state(self.config["production_state"])["operation"])
		self.assertEqual(self.rollouts, [])
		self.assertIsNone(self.gate())

	# ================
	# test_the_public_list_marks_maintenance_candidates
	# ================
	def test_the_public_list_marks_maintenance_candidates(self):
		receiver.candidate_status(self.config)
		listing = json.loads((self.root / "candidates.json").read_text())
		rows = {row["candidate"]: row for row in listing["candidates"]}
		self.assertTrue(rows[self.client]["maintenance"] and rows[self.server]["maintenance"])


if __name__ == "__main__":
	unittest.main()
