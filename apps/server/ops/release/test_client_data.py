"""
===========================================================================

test_client_data.py - data releases stage from the live tree and the store.

A data candidate reuses every served file the live release already holds,
writes the rest from verified payloads, re-verifies all of it before
publication, and may change the asset schema that an application release
must keep.

===========================================================================
"""

import gzip
import hashlib
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest

import client_data
from client_deploy import promote, record_smoke
from release_state import admit, read_state, write_state
from test_release_state import CLIENT_CONTRACT, candidate, production

DATA_SCHEMA = CLIENT_CONTRACT["assetSchema"] + 1


# ================
# sha
# ================
def sha(data):
	return hashlib.sha256(data).hexdigest()


# ================
# release
#
# A package on disk and its manifest: an entry page, one pack payload serving
# a plain and a gzip-encoded route, and a publication file that keeps its
# name while its bytes change.
# ================
def release(root, entry, pack, publication, contract):
	root.mkdir(parents=True)
	encoded = gzip.compress(b'{"catalog":true}', mtime=0)
	payload = pack + encoded
	files = {
		"application/index.html": (entry, "application"),
		f"payload/{sha(payload)}.bin": (payload, "data"),
		"publication.json": (publication, "data"),
	}
	for name, (data, _) in files.items():
		(root / name).parent.mkdir(parents=True, exist_ok=True)
		(root / name).write_bytes(data)
	pack_file = f"payload/{sha(payload)}.bin"
	manifest = {
		"format": "sro-beta-release-v1",
		"protocol": contract["protocol"],
		"assetSchema": contract["assetSchema"],
		"sourceHash": sha(entry),
		"files": [{"path": name, "length": len(data), "sha256": sha(data), "kind": kind}
			for name, (data, kind) in files.items()],
		"routes": [
			{"url": "/index.html", "file": "application/index.html", "offset": 0, "length": len(entry), "mime": "text/html"},
			{"url": "/assets/pack.bin", "file": pack_file, "offset": 0, "length": len(pack),
				"mime": "application/octet-stream"},
			{"url": "/assets/catalog.json", "file": pack_file, "offset": len(pack), "length": len(encoded),
				"mime": "application/json", "encoding": "gzip"},
			{"url": "/assets/publication.json", "file": "publication.json", "offset": 0, "length": len(publication),
				"mime": "application/json"},
		],
	}
	identity = {"source": manifest["sourceHash"], "files": manifest["files"], "routes": manifest["routes"]}
	manifest["releaseId"] = sha(json.dumps(identity, separators=(",", ":")).encode())
	write_state(root / "release.json", manifest)
	return manifest


# ================
# DataFixture
#
# A base release, the next release built from it, and a data plan for it.
# CONTRACT and INTENT describe the next release; a coordinated fixture
# changes both.
# ================
class DataFixture:
	CONTRACT = {"protocol": CLIENT_CONTRACT["protocol"], "assetSchema": DATA_SCHEMA}
	INTENT = {}

	# ================
	# setUp
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		self.root = Path(directory.name)
		self.base = release(self.root / "base", b"old", b"shared pack", b"old publication", CLIENT_CONTRACT)
		self.next = release(self.root / "next", b"new", b"shared pack", b"new publication", self.CONTRACT)
		self.config = {"payload_store": str(self.root / "store")}
		self.state = production()
		self.plan = candidate()
		self.plan.update(kind="data", release=self.next["releaseId"], baseRelease=self.base["releaseId"],
			compatibility=dict(self.CONTRACT), **self.INTENT)

	# ================
	# bundle
	# ================
	def bundle(self):
		return client_data.bundle(self.root / "next", self.base, self.plan, self.root / "out")



# ================
# DataStoreTests
#
# The payload store and the operator bundle need no symlinks.
# ================
class DataStoreTests(DataFixture, unittest.TestCase):
	# ================
	# test_only_data_the_live_release_lacks_is_uploaded
	# ================
	def test_only_data_the_live_release_lacks_is_uploaded(self):
		batches = self.bundle()
		self.assertEqual(len(batches), 1)
		with tarfile.open(batches[0]) as batch:
			names = batch.getnames()
		self.assertEqual(names, ["payload.json", sha(b"new publication")])

	# ================
	# test_a_slow_link_splits_the_payload
	#
	# Each needed blob above the requested size starts its own batch; no
	# batch ever passes the host's ceiling.
	# ================
	def test_a_slow_link_splits_the_payload(self):
		changed = release(self.root / "changed", b"new", b"changed pack", b"new publication", self.CONTRACT)
		self.plan.update(release=changed["releaseId"])
		whole = client_data.bundle(self.root / "changed", self.base, self.plan, self.root / "whole")
		split = client_data.bundle(self.root / "changed", self.base, self.plan, self.root / "split", 1)
		self.assertEqual(len(whole), 1)
		self.assertEqual(len(split), 2)
		names = []
		for path in split:
			with tarfile.open(path) as batch:
				names += batch.getnames()[1:]
		with tarfile.open(whole[0]) as batch:
			self.assertEqual(sorted(names), sorted(batch.getnames()[1:]))

	# ================
	# test_the_store_verifies_and_resumes
	# ================
	def test_the_store_verifies_and_resumes(self):
		batch = self.bundle()[0]
		self.assertEqual(client_data.store_payload(self.config, batch), {"stored": 1, "present": 0})
		self.assertEqual(client_data.store_payload(self.config, batch), {"stored": 0, "present": 1})
		forged = self.root / "forged.tar"
		with tarfile.open(batch) as source, tarfile.open(forged, "w") as output:
			for member in source.getmembers():
				data = source.extractfile(member).read()
				if member.name != "payload.json":
					data = b"X" + data[1:]
				member.size = len(data)
				output.addfile(member, __import__("io").BytesIO(data))
		(Path(self.config["payload_store"]) / sha(b"new publication")).unlink()
		with self.assertRaisesRegex(ValueError, "digest"):
			client_data.store_payload(self.config, forged)

	# ================
	# test_a_rerun_skips_payloads_the_host_stores
	#
	# The inventory names only stored payloads at their declared length and
	# keeps them from the 14-day prune; the next bundle leaves them out.
	# ================
	def test_a_rerun_skips_payloads_the_host_stores(self):
		needed = client_data.needed_payloads(self.root / "next", self.base)
		self.assertEqual([row["sha256"] for row in needed], [sha(b"new publication")])
		self.assertEqual(client_data.payload_inventory(self.config, {"files": needed}), {"present": []})
		client_data.store_payload(self.config, self.bundle()[0])
		stored = Path(self.config["payload_store"]) / needed[0]["sha256"]
		old = stored.stat().st_mtime - 10 * 24 * 3600
		os.utime(stored, (old, old))
		self.assertEqual(client_data.payload_inventory(self.config, {"files": needed}), {"present": [needed[0]["sha256"]]})
		self.assertGreater(stored.stat().st_mtime, old + 24 * 3600)
		# A stored file of another length is not offered as present.
		wrong = [{"sha256": needed[0]["sha256"], "length": needed[0]["length"] + 1}]
		self.assertEqual(client_data.payload_inventory(self.config, {"files": wrong}), {"present": []})
		# Nor is a damaged one of the right length: the rerun must resend it.
		intact = stored.read_bytes()
		stored.write_bytes(bytes(len(intact)))
		self.assertEqual(client_data.payload_inventory(self.config, {"files": needed}), {"present": []})
		stored.write_bytes(intact)
		again = client_data.bundle(self.root / "next", self.base, self.plan, self.root / "again",
			present={needed[0]["sha256"]})
		self.assertEqual(again, [])
		for bad in ({"files": [{"sha256": "../x", "length": 1}]}, {"files": [{"sha256": "a" * 64, "length": -1}]},
				{"files": ["a" * 64]},
				{"files": [{"sha256": "a" * 64, "length": 1}] * (client_data.MAX_INVENTORY_FILES + 1)}, {}):
			with self.assertRaises(ValueError):
				client_data.payload_inventory(self.config, bad)

	# ================
	# test_a_build_that_is_not_the_declared_release_is_refused
	# ================
	def test_a_build_that_is_not_the_declared_release_is_refused(self):
		self.plan["compatibility"] = dict(self.plan["compatibility"], assetSchema=DATA_SCHEMA + 1)
		with self.assertRaisesRegex(ValueError, "asset schema"):
			self.bundle()


# ================
# AdmissionTests
# ================
class AdmissionTests(unittest.TestCase):
	# ================
	# test_only_a_data_release_may_change_the_asset_schema
	# ================
	def test_only_a_data_release_may_change_the_asset_schema(self):
		plan = candidate()
		plan["compatibility"] = dict(plan["compatibility"], assetSchema=DATA_SCHEMA)
		with self.assertRaisesRegex(ValueError, "asset schema"):
			admit(production(), plan)
		plan["kind"] = "data"
		self.assertEqual(admit(production(), plan), "client")
		server = candidate("server")
		server["kind"] = "data"
		with self.assertRaisesRegex(ValueError, "release kind"):
			admit(production(), server)


# ================
# LiveDataFixture
#
# The base materialized as the live release, as its first publication did,
# and the next release staged as a data candidate beside it. Staging links
# into the live tree and publication swaps a directory symlink; both need
# POSIX link semantics.
# ================
class LiveDataFixture(DataFixture):
	# ================
	# setUp
	# ================
	def setUp(self):
		super().setUp()
		for name in ("releases", "candidates", "records", "shared"):
			(self.root / name).mkdir()
		self.live = self.root / "releases" / self.base["releaseId"]
		(self.live / "assets").mkdir(parents=True)
		(self.live / "index.html").write_bytes(b"old")
		package = self.root / "base"
		for path, (file, offset, length, decode) in client_data.outputs(self.base).items():
			data = (package / file).read_bytes()[offset:offset + length]
			(self.live / path).parent.mkdir(parents=True, exist_ok=True)
			(self.live / path).write_bytes(gzip.decompress(data) if decode else data)
		self.config.update({
			"production_state": str(self.root / "production.json"),
			"client_link": str(self.root / "live"),
			"client_releases": str(self.root / "releases"),
			"client_candidates": str(self.root / "candidates"),
			"candidate_records": str(self.root / "records"),
			"application_assets": str(self.root / "shared"),
			"client_manifest": str(self.root / "base.json"),
			"origin": "https://example.test",
		})
		os.symlink(self.live, self.config["client_link"], target_is_directory=True)
		self.state["client"]["release"] = self.base["releaseId"]
		write_state(self.config["production_state"], self.state)
		write_state(self.config["client_manifest"], self.base)
		for batch in self.bundle():
			client_data.store_payload(self.config, batch)
		self.staged = client_data.stage(self.config, self.root / "out" / "candidate.tar")
		self.tree = self.root / "candidates" / self.staged["candidate"]


# ================
# DataStagingTests
# ================
@unittest.skipIf(os.name == "nt", "data staging requires POSIX hard links and directory symlinks")
class DataStagingTests(LiveDataFixture, unittest.TestCase):

	# ================
	# test_unchanged_files_are_the_live_files_and_new_ones_are_written
	# ================
	def test_unchanged_files_are_the_live_files_and_new_ones_are_written(self):
		self.assertTrue(os.path.samefile(self.tree / "assets/pack.bin", self.live / "assets/pack.bin"))
		self.assertTrue(os.path.samefile(self.tree / "assets/catalog.json", self.live / "assets/catalog.json"))
		self.assertEqual((self.tree / "assets/publication.json").read_bytes(), b"new publication")
		self.assertEqual((self.live / "assets/publication.json").read_bytes(), b"old publication")
		self.assertEqual((self.tree / "index.html").read_bytes(), b"new")

	# ================
	# test_any_change_to_the_staged_tree_fails_verification
	# ================
	def test_any_change_to_the_staged_tree_fails_verification(self):
		client_data.verify(self.config, self.staged["candidate"])
		(self.tree / "assets/extra.json").write_bytes(b"{}")
		with self.assertRaisesRegex(ValueError, "gained or lost"):
			client_data.verify(self.config, self.staged["candidate"])
		(self.tree / "assets/extra.json").unlink()
		(self.tree / "assets/publication.json").write_bytes(b"tampered")
		with self.assertRaisesRegex(ValueError, "changed after validation"):
			client_data.verify(self.config, self.staged["candidate"])

	# ================
	# test_publication_switches_to_the_data_release_and_its_schema
	# ================
	def test_publication_switches_to_the_data_release_and_its_schema(self):
		report = {**self.staged, "verdict": "PASS", "errors": [],
			"phases": {name: "PASS" for name in ("title", "login", "roster", "world", "gameplay", "resume")}}
		record_smoke(self.config, report)
		result = promote(self.config, self.staged["candidate"], verify=lambda origin, entry: None)
		self.assertEqual(Path(self.config["client_link"]).resolve(), self.tree.resolve())
		self.assertEqual(result["client"]["release"], self.next["releaseId"])
		self.assertEqual(result["client"]["compatibility"]["assetSchema"], DATA_SCHEMA)
		self.assertEqual(read_state(self.config["production_state"])["client"]["release"], self.next["releaseId"])
