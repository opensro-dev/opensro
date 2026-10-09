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
			(build / source).write_bytes(b"next server " + name.encode())
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
	def fake_deploy(self, _config, staging, manifest, notice=True, upgrade=False, on_upgrade=None, restore=None):
		self.rollouts.append((manifest["commit"], notice, (Path(staging) / "agent").read_bytes()))
		self.deployments.append({"upgrade": upgrade, "restore": restore})
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
		request = {"server": self.server, "client": self.client}
		return coordinated.publish(self.config, request, tempfile.mkdtemp(dir=self.root), self.edge)

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
		self.assertEqual(self.rollouts, [(NEW_COMMIT, True, b"next server agent")])
		self.assertEqual(self.deployments, [{"upgrade": False, "restore": None}])
		self.assertEqual(self.live_client(), self.tree.resolve())
		pending = read_state(self.config["production_state"])
		self.assertEqual(pending["operation"]["phase"], "verifying")
		self.assertEqual(pending["server"], self.state["server"])
		with self.assertRaisesRegex(ValueError, "reconciliation"):
			admit(pending, client_plan(release=self.next["releaseId"], baseRelease=self.base["releaseId"]))
		self.record_evidence(self.report())
		state = coordinated.confirm(self.config)
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
			coordinated.confirm(self.config)
		self.publish()
		with self.assertRaisesRegex(ValueError, "no browser evidence"):
			coordinated.confirm(self.config)
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
			coordinated.confirm(self.config)
		self.assertEqual(read_state(self.config["production_state"])["operation"]["phase"], "verifying")
		with self.assertRaisesRegex(ValueError, "not allowed"):
			receiver.request(self.config, "stage", {"operation": "confirm-coordinated"}, self.root / "unused")


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
		self.assertEqual(self.deployments, [{"upgrade": True, "restore": None}])
		# Each shard was journaled as started before its backup was recorded.
		self.assertEqual(self.journaled, [None, None])
		pending = read_state(self.config["production_state"])
		self.assertEqual(sorted(pending["operation"]["authorities"]), list(self.shards))
		self.assertEqual(self.gate(), {"accounts": [PROBE]})
		self.record_evidence(self.report())
		state = coordinated.confirm(self.config)
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
		self.assertEqual(self.deployments[-1], {"upgrade": False, "restore": authorities})
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
