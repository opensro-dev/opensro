"""
===========================================================================

test_release_state.py - exercise stale approvals and compatibility admission.

Fixtures describe observable production releases. Tests drive deployments and
rollbacks through the same state transitions the host receiver uses.

===========================================================================
"""

import copy
from pathlib import Path
import tempfile
import unittest

from release_state import admit, begin, complete, read_state, store_upgrade_required, write_state

CLIENT_RELEASE = "a" * 64
SERVER_RELEASE = "b" * 40
NEXT_RELEASE = "c" * 64
OLD_COMMIT = "d" * 40
NEW_COMMIT = "e" * 40
CLIENT_CONTRACT = {"protocol": 2, "assetSchema": 1}
SERVER_CONTRACT = {
	"protocolMin": 2,
	"protocolMax": 2,
	"storeReadMin": 13,
	"storeReadMax": 13,
	"storeWrite": 13,
}


# ================
# production
#
# Each test receives separate data so a rejected attempt cannot be hidden by
# mutation shared with another case.
# ================
def production():
	return {
		"format": "opensro-production-v1",
		"client": {
			"release": CLIENT_RELEASE,
			"commit": OLD_COMMIT,
			"generation": 1,
			"compatibility": dict(CLIENT_CONTRACT),
		},
		"server": {
			"release": SERVER_RELEASE,
			"commit": OLD_COMMIT,
			"generation": 1,
			"compatibility": dict(SERVER_CONTRACT),
		},
		"history": [],
		"operation": None,
	}


# ================
# candidate
#
# Bind the candidate to the component generation observed at build time.
# ================
def candidate(component="client"):
	state = production()
	return {
		"format": "opensro-deployment-v1",
		"component": component,
		"baseRelease": state[component]["release"],
		"baseGeneration": state[component]["generation"],
		"release": NEXT_RELEASE,
		"commit": NEW_COMMIT,
		"ancestors": [NEW_COMMIT, OLD_COMMIT],
		"mode": "forward",
		"compatibility": copy.deepcopy(state[component]["compatibility"]),
	}


# ================
# ReleaseStateTests
#
# Prove that approval intent survives neither production drift nor an unsafe
# protocol/schema change. No test relies on implementation text.
# ================
class ReleaseStateTests(unittest.TestCase):
	# ================
	# test_client_publication_leaves_server_identity_untouched
	# ================
	def test_client_publication_leaves_server_identity_untouched(self):
		state = production()
		plan = candidate()
		pending = begin(state, plan, "start")
		self.assertEqual(state, production())
		self.assertEqual(pending["client"], state["client"])
		result = complete(pending, plan, "finish")
		self.assertEqual(result["server"], state["server"])
		self.assertEqual(result["client"]["release"], NEXT_RELEASE)
		self.assertEqual(result["client"]["generation"], 2)
		self.assertEqual(result["history"][0]["release"], CLIENT_RELEASE)
		self.assertIsNone(result["operation"])

	# ================
	# test_old_approval_is_rejected_even_after_returning_to_same_release
	# ================
	def test_old_approval_is_rejected_even_after_returning_to_same_release(self):
		state = production()
		state["client"]["generation"] = 3
		with self.assertRaisesRegex(ValueError, "superseded"):
			admit(state, candidate())

	# ================
	# test_normal_deployment_cannot_be_used_as_rollback
	# ================
	def test_normal_deployment_cannot_be_used_as_rollback(self):
		plan = candidate()
		plan["ancestors"] = [NEW_COMMIT]
		with self.assertRaisesRegex(ValueError, "backwards"):
			admit(production(), plan)

	# ================
	# test_interrupted_deployment_blocks_a_second_attempt
	# ================
	def test_interrupted_deployment_blocks_a_second_attempt(self):
		pending = begin(production(), candidate(), "start")
		with self.assertRaisesRegex(ValueError, "reconciliation"):
			admit(pending, candidate())

	# ================
	# test_incompatible_client_is_rejected_before_mutation
	# ================
	def test_incompatible_client_is_rejected_before_mutation(self):
		state = production()
		plan = candidate()
		plan["compatibility"]["protocol"] = 3
		with self.assertRaisesRegex(ValueError, "incompatible"):
			begin(state, plan, "start")
		self.assertEqual(state, production())

	# ================
	# test_server_expansion_allows_client_upgrade_without_breaking_old_tabs
	# ================
	def test_server_expansion_allows_client_upgrade_without_breaking_old_tabs(self):
		plan = candidate("server")
		plan["compatibility"]["protocolMax"] = 3
		state = complete(begin(production(), plan, "start"), plan, "finish")
		client = candidate()
		client["compatibility"]["protocol"] = 3
		self.assertEqual(admit(state, client), "client")

	# ================
	# test_server_cannot_abandon_already_running_clients
	# ================
	def test_server_cannot_abandon_already_running_clients(self):
		state = production()
		state["server"]["compatibility"]["protocolMax"] = 3
		with self.assertRaisesRegex(ValueError, "existing browser"):
			admit(state, candidate("server"))

	# ================
	# test_rollback_requires_reason_and_exact_recorded_artifact
	# ================
	def test_rollback_requires_reason_and_exact_recorded_artifact(self):
		forward = candidate()
		state = complete(begin(production(), forward, "start"), forward, "finish")
		rollback = dict(forward, mode="rollback", release=CLIENT_RELEASE, commit=OLD_COMMIT,
			baseRelease=NEXT_RELEASE, baseGeneration=2)
		with self.assertRaisesRegex(ValueError, "reason"):
			admit(state, rollback)
		rollback["reason"] = "Verified rendering regression"
		self.assertEqual(admit(state, rollback), "client")
		rollback["compatibility"] = {"protocol": 3, "assetSchema": 1}
		with self.assertRaisesRegex(ValueError, "recorded release"):
			admit(state, rollback)

	# ================
	# test_database_downgrade_is_never_a_release_rollback
	# ================
	def test_database_downgrade_is_never_a_release_rollback(self):
		state = production()
		state["server"]["compatibility"].update(storeReadMax=14, storeWrite=14)
		plan = candidate("server")
		with self.assertRaisesRegex(ValueError, "live database"):
			admit(state, plan)
		plan["compatibility"]["storeReadMax"] = 14
		with self.assertRaisesRegex(ValueError, "downgrade"):
			admit(state, plan)

	# ================
	# test_offline_store_upgrade_admission
	#
	# A forward server that declares an offline upgrade from the live schema is
	# admitted and told to run it; one that cannot reach the live schema, or
	# declares an upgrade above what it reads, is refused.
	# ================
	def test_offline_store_upgrade_admission(self):
		state = production()
		plan = candidate("server")
		plan["compatibility"].update(storeReadMin=14, storeReadMax=14, storeWrite=14)
		with self.assertRaisesRegex(ValueError, "live database"):
			admit(state, plan)
		plan["compatibility"]["storeUpgradeFrom"] = 13
		self.assertEqual(admit(state, plan), "server")
		self.assertTrue(store_upgrade_required(state, plan))
		self.assertFalse(store_upgrade_required(state, candidate("server")))
		state["server"]["compatibility"].update(storeReadMin=12, storeWrite=12)
		with self.assertRaisesRegex(ValueError, "live database"):
			admit(state, plan)
		plan["compatibility"]["storeUpgradeFrom"] = 15
		with self.assertRaisesRegex(ValueError, "offline upgrade"):
			admit(production(), plan)

	# ================
	# test_completion_cannot_confirm_a_different_release
	# ================
	def test_completion_cannot_confirm_a_different_release(self):
		plan = candidate()
		pending = begin(production(), plan, "start")
		plan["release"] = "f" * 64
		with self.assertRaisesRegex(ValueError, "does not match"):
			complete(pending, plan, "finish")

	# ================
	# test_ledger_round_trip_and_missing_state_refusal
	# ================
	def test_ledger_round_trip_and_missing_state_refusal(self):
		with tempfile.TemporaryDirectory() as directory:
			path = Path(directory) / "production.json"
			with self.assertRaises(FileNotFoundError):
				read_state(path)
			write_state(path, production())
			self.assertEqual(read_state(path), production())
			self.assertFalse(path.with_name(path.name + ".incoming").exists())


if __name__ == "__main__":
	unittest.main()
