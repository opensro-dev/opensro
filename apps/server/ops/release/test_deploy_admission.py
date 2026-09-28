"""
===========================================================================

test_deploy_admission.py - server admission, retained bytes and health commit.

Inject the Nomad rollout owner while using the real journal and filesystem.
Preflight failure and post-health cleanup failure have different outcomes.

===========================================================================
"""

import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from bundle import FILES
import deploy
from release_state import read_state, write_state
from retention import directory as retained_directory
from test_release_state import candidate, production


# ================
# DeployAdmissionTests
#
# Each fixture describes the same inspected production state as its disk files.
# ================
class DeployAdmissionTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		self.module = self.root / "module"
		self.state = production()
		self.config = {"module": str(self.module), "production_state": str(self.root / "production.json"),
			"candidate_records": str(self.root / "records")}
		original = {"format": "opensro-server-v1", "commit": self.state["server"]["commit"], "files": {}}
		for name in FILES:
			path = self.module / name
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(b"verified production input")
			original["files"][name] = hashlib.sha256(path.read_bytes()).hexdigest()
		write_state(self.module / "release.json", original)
		write_state(self.config["production_state"], self.state)
		self.manifest = {**original, "format": "opensro-server-v2", "plan": candidate("server")}
		self.manifest["commit"] = self.manifest["plan"]["commit"]

	# ================
	# test_stale_approval_never_reaches_nomad
	# ================
	def test_stale_approval_never_reaches_nomad(self):
		self.manifest["plan"]["baseGeneration"] += 1
		with patch.object(deploy, "deploy") as rollout, self.assertRaisesRegex(ValueError, "superseded"):
			deploy.deploy_approved(self.config, self.root, self.manifest)
		rollout.assert_not_called()
		self.assertEqual(read_state(self.config["production_state"]), self.state)

	# ================
	# test_failed_rollout_retains_inputs_and_blocks_blind_retries
	# ================
	def test_failed_rollout_retains_inputs_and_blocks_blind_retries(self):
		with patch.object(deploy, "deploy", side_effect=RuntimeError("Nomad health failed")):
			with self.assertRaisesRegex(RuntimeError, "health failed"):
				deploy.deploy_approved(self.config, self.root, self.manifest)
		state = read_state(self.config["production_state"])
		self.assertEqual(state["operation"]["phase"], "failed")
		self.assertEqual(state["server"], self.state["server"])
		retained = retained_directory(self.config, "server", self.state["server"]["release"])
		self.assertEqual((retained / "agent").read_bytes(), b"verified production input")
		with patch.object(deploy, "deploy") as rollout, self.assertRaisesRegex(ValueError, "reconciliation"):
			deploy.deploy_approved(self.config, self.root, self.manifest)
		rollout.assert_not_called()

	# ================
	# test_post_health_cleanup_failure_preserves_successful_release_identity
	# ================
	def test_post_health_cleanup_failure_preserves_successful_release_identity(self):
		# ================
		# healthy_then_cleanup_failure
		# The durable release marker is written only after Nomad health succeeds.
		# ================
		def healthy_then_cleanup_failure(_config, _staging, manifest):
			write_state(self.module / "release.json", manifest)
			raise RuntimeError("token revocation failed")

		with patch.object(deploy, "deploy", side_effect=healthy_then_cleanup_failure):
			deploy.deploy_approved(self.config, self.root, self.manifest)
		state = read_state(self.config["production_state"])
		self.assertIsNone(state["operation"])
		self.assertEqual(state["server"]["commit"], self.manifest["commit"])
		self.assertEqual(state["server"]["generation"], 2)
		self.assertEqual(state["client"], self.state["client"])
		self.assertIn("cleanup failed", state["lastWarning"]["detail"])


if __name__ == "__main__":
	unittest.main()
