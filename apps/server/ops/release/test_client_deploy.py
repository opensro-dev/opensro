"""
===========================================================================

test_client_deploy.py - exercise real client staging, promotion and rollback.

Use temporary files, real hard links and real symlink publication. Only the
remote HTTPS health request is injected; artifact and filesystem boundaries
remain the same ones used on the production host.

===========================================================================
"""

import copy
import gzip
import hashlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import client_bundle
from client_deploy import application_routes, digest, promote, record_smoke, stage, switch
from release_state import read_state, write_state
from rollback import prepare
from retention import directory as retained_directory
from test_release_state import CLIENT_CONTRACT, candidate, production


# ================
# manifest_for
#
# Build a small valid release with one retained data object and one application
# entry. Its identity uses the same ordered JSON projection as the JS builder.
# ================
def manifest_for(entry):
	data = b"verified assets"
	manifest = {
		"format": "sro-beta-release-v1",
		"protocol": CLIENT_CONTRACT["protocol"],
		"assetSchema": CLIENT_CONTRACT["assetSchema"],
		"sourceHash": hashlib.sha256(entry).hexdigest(),
		"assetAuthorityHash": hashlib.sha256(data).hexdigest(),
		"files": [
			{"path": "application/index.html", "length": len(entry),
				"sha256": hashlib.sha256(entry).hexdigest(), "kind": "application"},
			{"path": "payload/data.bin", "length": len(data),
				"sha256": hashlib.sha256(data).hexdigest(), "kind": "data"},
		],
		"routes": [
			{"url": "/index.html", "file": "application/index.html", "offset": 0,
				"length": len(entry), "mime": "text/html"},
			{"url": "/assets/data.bin", "file": "payload/data.bin", "offset": 0,
				"length": len(data), "mime": "application/octet-stream"},
		],
	}
	identity = {"source": manifest["sourceHash"], "files": manifest["files"], "routes": manifest["routes"]}
	manifest["releaseId"] = hashlib.sha256(json.dumps(identity, separators=(",", ":")).encode()).hexdigest()
	return manifest


# ================
# ClientDeployTests
#
# Linux publication relies on POSIX directory-symlink replacement. Windows can
# create symlinks under CI but does not implement that atomic rename contract.
# ================
@unittest.skipIf(os.name == "nt", "Linux publication requires POSIX directory-symlink replacement")
class ClientDeployTests(unittest.TestCase):
	# ================
	# setUp
	# Create the inspected live release and a candidate bound to its generation.
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name).resolve()
		self.base = manifest_for(b"old browser")
		self.next = manifest_for(b"new browser")
		self.config = {
			"production_state": str(self.root / "production.json"),
			"client_link": str(self.root / "live"),
			"client_releases": str(self.root / "releases"),
			"client_candidates": str(self.root / "candidates"),
			"candidate_records": str(self.root / "records"),
			"application_assets": str(self.root / "shared"),
			"client_manifest": str(self.root / "base.json"),
			"origin": "https://example.test",
		}
		for name in ("releases", "candidates", "records", "shared"):
			(self.root / name).mkdir()
		self.live = self.root / "releases" / self.base["releaseId"]
		(self.live / "assets").mkdir(parents=True)
		(self.live / "index.html").write_bytes(b"old browser")
		(self.live / "assets/data.bin").write_bytes(b"verified assets")
		os.symlink(self.live, self.config["client_link"], target_is_directory=True)
		state = production()
		state["client"]["release"] = self.base["releaseId"]
		write_state(self.config["production_state"], state)
		write_state(self.config["client_manifest"], self.base)
		self.plan = candidate()
		self.plan.update(baseRelease=self.base["releaseId"], release=self.next["releaseId"])
		package = self.root / "package"
		(package / "application").mkdir(parents=True)
		(package / "application/index.html").write_bytes(b"new browser")
		write_state(package / "release.json", self.next)
		self.archive = self.root / "candidate.tar"
		with patch.object(client_bundle, "build_plan", return_value=self.plan):
			client_bundle.bundle(package, self.archive, state)

	# ================
	# passing_report
	# Bind all required browser phases to the staged archive and actual entry.
	# ================
	def passing_report(self, staged):
		return {
			**staged,
			"verdict": "PASS",
			"errors": [],
			"phases": {name: "PASS" for name in ("title", "login", "roster", "world", "gameplay", "resume")},
		}

	# ================
	# test_staging_cannot_publish_or_modify_retained_bytes
	# ================
	def test_staging_cannot_publish_or_modify_retained_bytes(self):
		before = read_state(self.config["production_state"])
		staged = stage(self.config, self.archive)
		self.assertEqual(Path(self.config["client_link"]).resolve(), self.live)
		self.assertEqual((self.live / "index.html").read_bytes(), b"old browser")
		self.assertEqual(read_state(self.config["production_state"]), before)
		new = Path(self.config["client_candidates"]) / staged["candidate"]
		self.assertEqual((new / "index.html").read_bytes(), b"new browser")
		self.assertTrue(os.path.samefile(new / "assets/data.bin", self.live / "assets/data.bin"))

	# ================
	# test_staging_preserves_candidate_encoding_and_existing_shared_encoding
	#
	# The manifest owns the candidate's exact compressed bytes, while the shared
	# URL owns decoded content. Existing browser tabs keep their original variant.
	# ================
	def test_staging_preserves_candidate_encoding_and_existing_shared_encoding(self):
		body = b"export const value = 'immutable';"
		encoded = gzip.compress(body, mtime=1)
		retained = gzip.compress(body, mtime=2)
		name = "assets/entry-abcdefgh.js"
		package = self.root / "package"
		payloads = {"application/" + name: body, "encoded/application.gz": encoded}
		manifest = copy.deepcopy(self.next)
		for file, data in payloads.items():
			path = package / file
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(data)
			manifest["files"].append({"path": file, "length": len(data),
				"sha256": hashlib.sha256(data).hexdigest(), "kind": "application"})
		manifest["routes"].append({"url": "/" + name, "file": "application/" + name,
			"offset": 0, "length": len(body), "mime": "text/javascript",
			"gzip": {"file": "encoded/application.gz", "offset": 0, "length": len(encoded)}})
		identity = {"source": manifest["sourceHash"], "files": manifest["files"], "routes": manifest["routes"]}
		manifest["releaseId"] = hashlib.sha256(json.dumps(identity, separators=(",", ":")).encode()).hexdigest()
		write_state(package / "release.json", manifest)
		plan = dict(self.plan, release=manifest["releaseId"])
		with patch.object(client_bundle, "build_plan", return_value=plan):
			self.archive.unlink()
			client_bundle.bundle(package, self.archive, read_state(self.config["production_state"]))
		shared = Path(self.config["application_assets"]) / name
		shared.parent.mkdir(parents=True)
		shared.write_bytes(body)
		compressed = shared.with_name(shared.name + ".gz")
		compressed.write_bytes(retained)
		staged = stage(self.config, self.archive)
		candidate_root = Path(self.config["client_candidates"]) / staged["candidate"]
		self.assertEqual((candidate_root / (name + ".gz")).read_bytes(), encoded)
		self.assertEqual(compressed.read_bytes(), retained)
		self.assertEqual(stage(self.config, self.archive), staged)
		self.assertEqual(Path(self.config["client_link"]).resolve(), self.live)

	# ================
	# test_staging_retry_reuses_only_unchanged_bytes
	# ================
	def test_staging_retry_reuses_only_unchanged_bytes(self):
		before = read_state(self.config["production_state"])
		first = stage(self.config, self.archive)
		self.assertEqual(stage(self.config, self.archive), first)
		self.assertEqual(read_state(self.config["production_state"]), before)
		entry = Path(self.config["client_candidates"]) / first["candidate"] / "index.html"
		entry.write_bytes(b"corrupted staged browser")
		with self.assertRaisesRegex(ValueError, "staged application bytes changed"):
			stage(self.config, self.archive)

	# ================
	# test_failed_atomic_rename_does_not_block_a_later_recovery
	# ================
	def test_failed_atomic_rename_does_not_block_a_later_recovery(self):
		link = Path(self.config["client_link"])
		with patch("client_deploy.os.replace", side_effect=OSError("rename refused")):
			with self.assertRaisesRegex(OSError, "rename refused"):
				switch(link, self.root / "other")
		self.assertEqual(link.resolve(), self.live)
		self.assertFalse(link.with_name(link.name + ".incoming").is_symlink())
		switch(link, self.live)
		self.assertEqual(link.resolve(), self.live)

	# ================
	# test_promotion_requires_complete_browser_evidence
	# ================
	def test_promotion_requires_complete_browser_evidence(self):
		staged = stage(self.config, self.archive)
		with self.assertRaises(FileNotFoundError):
			promote(self.config, staged["candidate"], verify=lambda *_arguments: None)
		report = self.passing_report(staged)
		del report["phases"]["world"]
		with self.assertRaisesRegex(ValueError, "missing phase"):
			record_smoke(self.config, report)
		self.assertEqual(Path(self.config["client_link"]).resolve(), self.live)

	# ================
	# test_success_changes_only_the_client_generation
	# ================
	def test_success_changes_only_the_client_generation(self):
		staged = stage(self.config, self.archive)
		record_smoke(self.config, self.passing_report(staged))
		before = read_state(self.config["production_state"])
		checks = []
		result = promote(self.config, staged["candidate"], verify=lambda origin, expected: checks.append((origin, expected)))
		self.assertEqual(result["server"], before["server"])
		self.assertEqual(result["client"]["generation"], 2)
		self.assertEqual(Path(self.config["client_link"]).resolve().name, staged["candidate"])
		self.assertEqual(checks, [(self.config["origin"], staged["entrySha256"])])

	# ================
	# test_failed_edge_check_restores_old_client_and_invalidates_approval
	# ================
	def test_failed_edge_check_restores_old_client_and_invalidates_approval(self):
		staged = stage(self.config, self.archive)
		record_smoke(self.config, self.passing_report(staged))
		old_digest = digest(self.live / "index.html")

		# ================
		# verify
		# Reject the candidate, then prove the actual restored entry is checked.
		# ================
		def verify(_origin, expected):
			if expected != old_digest:
				raise RuntimeError("candidate failed HTTPS admission")
			self.assertEqual(Path(self.config["client_link"]).resolve(), self.live)

		with self.assertRaisesRegex(RuntimeError, "HTTPS admission"):
			promote(self.config, staged["candidate"], verify=verify)
		self.assertEqual(Path(self.config["client_link"]).resolve(), self.live)
		self.assertEqual(json.loads(Path(self.config["client_manifest"]).read_text()), self.base)
		self.assertEqual(read_state(self.config["production_state"])["client"]["generation"], 2)
		with self.assertRaisesRegex(ValueError, "superseded"):
			promote(self.config, staged["candidate"], verify=verify)

	# ================
	# test_staged_tampering_cannot_use_an_older_passing_report
	# ================
	def test_staged_tampering_cannot_use_an_older_passing_report(self):
		staged = stage(self.config, self.archive)
		record_smoke(self.config, self.passing_report(staged))
		entry = Path(self.config["client_candidates"]) / staged["candidate"] / "index.html"
		entry.write_bytes(b"tampered browser")
		with self.assertRaisesRegex(ValueError, "bytes changed"):
			promote(self.config, staged["candidate"], verify=lambda *_arguments: None)
		self.assertEqual(Path(self.config["client_link"]).resolve(), self.live)

	# ================
	# test_changed_data_authority_is_not_an_application_release
	# ================
	def test_changed_data_authority_is_not_an_application_release(self):
		changed = copy.deepcopy(self.next)
		changed["routes"][1]["offset"] = 1
		with self.assertRaisesRegex(ValueError, "data or routes"):
			client_bundle.validate_base(self.base, changed)

	# ================
	# test_application_cannot_shadow_an_existing_data_route
	# ================
	def test_application_cannot_shadow_an_existing_data_route(self):
		changed = copy.deepcopy(self.next)
		changed["routes"][0]["url"] = changed["routes"][1]["url"]
		with self.assertRaisesRegex(ValueError, "duplicates"):
			client_bundle.validate_base(self.base, changed)
		with self.assertRaisesRegex(ValueError, "outside"):
			application_routes(changed, {"application/index.html": b"new browser"})

	# ================
	# test_explicit_rollback_reuses_verified_bytes_with_a_new_approval
	#
	# Exercise a complete forward publication and rollback on real filesystem
	# roots. Both transitions advance generations; the server never changes.
	# ================
	def test_explicit_rollback_reuses_verified_bytes_with_a_new_approval(self):
		staged = stage(self.config, self.archive)
		record_smoke(self.config, self.passing_report(staged))
		forward = promote(self.config, staged["candidate"], verify=lambda *_arguments: None)
		request = {"component": "client", "release": self.base["releaseId"], "reason": "Browser regression"}
		archive = prepare(self.config, request, self.root)
		rollback = stage(self.config, archive)
		record_smoke(self.config, self.passing_report(rollback))
		result = promote(self.config, rollback["candidate"], verify=lambda *_arguments: None)
		self.assertEqual(result["client"]["release"], self.base["releaseId"])
		self.assertEqual(result["client"]["generation"], 3)
		self.assertEqual(result["server"], forward["server"])
		self.assertEqual((Path(self.config["client_link"]) / "index.html").read_bytes(), b"old browser")
		with self.assertRaisesRegex(ValueError, "superseded"):
			promote(self.config, staged["candidate"], verify=lambda *_arguments: None)

	# ================
	# test_rollback_rejects_corrupted_retained_bytes
	# ================
	def test_rollback_rejects_corrupted_retained_bytes(self):
		staged = stage(self.config, self.archive)
		record_smoke(self.config, self.passing_report(staged))
		promote(self.config, staged["candidate"], verify=lambda *_arguments: None)
		retained = retained_directory(self.config, "client", self.base["releaseId"])
		(retained / "application/index.html").write_bytes(b"corrupt")
		with self.assertRaisesRegex(ValueError, "differs"):
			prepare(self.config, {"component": "client", "release": self.base["releaseId"], "reason": "Regression"}, self.root)
		self.assertEqual(read_state(self.config["production_state"])["client"]["release"], self.next["releaseId"])


if __name__ == "__main__":
	unittest.main()


# ================
# ClientProtocolTests
#
# A client candidate is admitted only when the release protocol compiled into
# the build equals the one its approval declares. No symlinks are involved,
# so these run on every platform.
# ================
class ClientProtocolTests(unittest.TestCase):
	# ================
	# bundle_with
	# Bundle a one-file build whose manifest reports `protocol`.
	# ================
	def bundle_with(self, protocol, schema=CLIENT_CONTRACT["assetSchema"]):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		root = Path(directory.name)
		manifest = manifest_for(b"browser")
		manifest["protocol"] = protocol
		manifest["assetSchema"] = schema
		package = root / "package"
		(package / "application").mkdir(parents=True)
		(package / "application/index.html").write_bytes(b"browser")
		write_state(package / "release.json", manifest)
		plan = candidate()
		plan.update(release=manifest["releaseId"])
		with patch.object(client_bundle, "build_plan", return_value=plan):
			return client_bundle.bundle(package, root / "candidate.tar", production())

	# ================
	# test_a_build_speaking_the_declared_protocol_is_bundled
	# ================
	def test_a_build_speaking_the_declared_protocol_is_bundled(self):
		self.assertEqual(self.bundle_with(CLIENT_CONTRACT["protocol"])["plan"]["compatibility"], CLIENT_CONTRACT)

	# ================
	# test_a_build_speaking_another_protocol_is_refused
	# ================
	def test_a_build_speaking_another_protocol_is_refused(self):
		for protocol in (CLIENT_CONTRACT["protocol"] + 1, None, "2"):
			with self.assertRaisesRegex(ValueError, "release protocol"):
				self.bundle_with(protocol)

	# ================
	# test_a_build_packaging_another_asset_schema_is_refused
	# ================
	def test_a_build_packaging_another_asset_schema_is_refused(self):
		for schema in (CLIENT_CONTRACT["assetSchema"] + 1, None):
			with self.assertRaisesRegex(ValueError, "asset schema"):
				self.bundle_with(CLIENT_CONTRACT["protocol"], schema)
