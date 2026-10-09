"""
===========================================================================

test_plan.py - freshness checks against real component history.

A newer unrelated commit can share a release candidate. A newer component
input supersedes it. A branch commit is never an implicit production release.

===========================================================================
"""

from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import plan
from plan import build_plan, git, require_current
from test_release_state import NEW_COMMIT, NEXT_RELEASE, OLD_COMMIT, production


# ================
# PlanTests
#
# Each test owns an isolated Git repository and never changes the checkout.
# ================
class PlanTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		git(["init", "--quiet"], self.root)
		git(["config", "user.name", "Release test"], self.root)
		git(["config", "user.email", "release@example.invalid"], self.root)
		self.original = self.commit("README.md", "initial")

	# ================
	# commit
	# Advance the tracked main reference only after recording a complete change.
	# ================
	def commit(self, name, content):
		path = self.root / name
		path.parent.mkdir(parents=True, exist_ok=True)
		path.write_text(content, encoding="utf-8")
		git(["add", name], self.root)
		git(["commit", "--quiet", "-m", content], self.root)
		commit = git(["rev-parse", "HEAD"], self.root)
		git(["update-ref", "refs/remotes/origin/main", commit], self.root)
		return commit

	# ================
	# test_unrelated_server_change_keeps_client_candidate_current
	# ================
	def test_unrelated_server_change_keeps_client_candidate_current(self):
		self.commit("apps/server/internal/game/change.go", "server change")
		require_current("client", self.original, self.root)
		with self.assertRaisesRegex(ValueError, "superseded"):
			require_current("server", self.original, self.root)

	# ================
	# test_new_client_input_invalidates_old_candidate
	# ================
	def test_new_client_input_invalidates_old_candidate(self):
		self.commit("apps/client-next/src/main.ts", "client change")
		with self.assertRaisesRegex(ValueError, "superseded"):
			require_current("client", self.original, self.root)

	# ================
	# test_dependency_policy_changes_invalidate_only_the_client_candidate
	#
	# Workspace catalogs and install-script policy can change a build without
	# modifying client source or the dependency lock. Compare each change alone.
	# ================
	def test_dependency_policy_changes_invalidate_only_the_client_candidate(self):
		for name in ("package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".npmrc"):
			with self.subTest(path=name):
				previous = git(["rev-parse", "HEAD"], self.root)
				self.commit(name, "dependency policy for " + name)
				with self.assertRaisesRegex(ValueError, "superseded"):
					require_current("client", previous, self.root)
				require_current("server", previous, self.root)

	# ================
	# test_unmerged_candidate_is_rejected
	# ================
	def test_unmerged_candidate_is_rejected(self):
		branch = self.commit("branch.txt", "branch change")
		git(["update-ref", "refs/remotes/origin/main", self.original], self.root)
		with self.assertRaises(subprocess.CalledProcessError):
			require_current("client", branch, self.root)



# ================
# IntentTests
#
# The declarations a plan carries, with Git answering for this checkout.
# ================
class IntentTests(unittest.TestCase):
	# ================
	# plan
	# ================
	def plan(self, intent):
		root = str(Path(__file__).resolve().parents[4])
		answers = {"--show-toplevel": root, "HEAD": NEW_COMMIT}

		# ================
		# fake_git
		# ================
		def fake_git(arguments, _root=None):
			if arguments[0] == "rev-list":
				return NEW_COMMIT + "\n" + OLD_COMMIT
			return answers[arguments[-1]]

		with patch.object(plan, "git", side_effect=fake_git):
			return build_plan("client", NEXT_RELEASE, production(), intent)

	# ================
	# test_maintenance_implies_coordinated
	# ================
	def test_maintenance_implies_coordinated(self):
		declared = self.plan({"kind": "data", "maintenance": True})
		self.assertEqual((declared["coordinated"], declared["maintenance"], declared["kind"]), (True, True, "data"))
		# Without either declaration the protocol-changing client stands alone,
		# and the live server refuses it.
		with self.assertRaisesRegex(ValueError, "incompatible"):
			self.plan({"kind": "data", "coordinated": False, "maintenance": False})
		with self.assertRaisesRegex(ValueError, "unknown plan intent"):
			self.plan({"kind": "data", "closed": True})


if __name__ == "__main__":
	unittest.main()
