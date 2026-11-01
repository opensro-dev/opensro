"""
===========================================================================

test_plan.py - freshness checks against real component history.

A newer unrelated commit can share a release candidate. A newer component
input supersedes it. A branch commit is never an implicit production release.

===========================================================================
"""

from fnmatch import fnmatchcase
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
	# test_a_variant_tool_change_invalidates_the_client_candidate
	#
	# The variant tools are client inputs by family pattern (#1745): a
	# tool the list never named (alexandria_shop_d11.py, from #1792)
	# supersedes a client candidate the day it changes.
	# ================
	def test_a_variant_tool_change_invalidates_the_client_candidate(self):
		for name in ("scripts/alexandria_shop_d11.py", "scripts/cap110_items.py",
			"scripts/import_vsro_item_glow.py", "scripts/build_alexandria_variant.py",
			"scripts/vsro_archive_pins.json"):
			with self.subTest(path=name):
				previous = git(["rev-parse", "HEAD"], self.root)
				self.commit(name, "variant tool change in " + name)
				with self.assertRaisesRegex(ValueError, "superseded"):
					require_current("client", previous, self.root)
				require_current("server", previous, self.root)



# ================
# VariantInputTests
#
# Every variant tool on disk is a client input (#1745). The family is
# defined here once, by the same shape the INPUTS patterns name; a new
# family member that no pattern covers fails this test instead of
# silently superseding nothing.
# ================
class VariantInputTests(unittest.TestCase):
	# ================
	# variant_family
	# ================
	def variant_family(self):
		scripts = Path(__file__).resolve().parents[4] / "scripts"
		family = []
		for path in sorted(scripts.iterdir()):
			name = path.name
			if name in ("build_alexandria_variant.py", "vsro_archive_pins.json"):
				family.append(name)
			elif path.suffix == ".py" and name.startswith(("alexandria_", "cap110_", "import_vsro_")):
				family.append(name)
		return family

	# ================
	# test_every_variant_tool_is_a_client_input
	# ================
	def test_every_variant_tool_is_a_client_input(self):
		family = self.variant_family()
		self.assertIn("build_alexandria_variant.py", family)
		self.assertIn("vsro_archive_pins.json", family)
		inputs = plan.INPUTS["client"]
		for name in family:
			candidate = "scripts/" + name
			self.assertTrue(
				any(fnmatchcase(candidate, entry) for entry in inputs),
				f"{candidate} is a variant tool but no client input covers it")


# build_plan reads the compatibility contract at this path under the checkout
# root (plan.py), so a directory holding it is the root.
CONTRACT_PATH = "apps/server/ops/release/compatibility.json"
HERE = Path(__file__).resolve().parent


# ================
# checkout_root
#
# The nearest directory above this file that holds the contract, or None when
# ops/release runs outside a full checkout (a host's installed controls), where
# the one test that needs the checkout skips instead of erroring.
# ================
def checkout_root():
	for candidate in (HERE, *HERE.parents):
		if (candidate / CONTRACT_PATH).is_file():
			return candidate
	return None


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
		root = checkout_root()
		if root is None:
			self.skipTest("no repository checkout holds " + CONTRACT_PATH + " above " + str(HERE))
		answers = {"--show-toplevel": str(root), "HEAD": NEW_COMMIT}

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
