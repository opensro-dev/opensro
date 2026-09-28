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

from plan import git, require_current


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
	# test_unmerged_candidate_is_rejected
	# ================
	def test_unmerged_candidate_is_rejected(self):
		branch = self.commit("branch.txt", "branch change")
		git(["update-ref", "refs/remotes/origin/main", self.original], self.root)
		with self.assertRaises(subprocess.CalledProcessError):
			require_current("client", branch, self.root)


if __name__ == "__main__":
	unittest.main()
