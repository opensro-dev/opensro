"""Regression tests for hash/provenance and conflict-safe collaboration ingress."""

import importlib.util
import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock


TOOLS = Path(__file__).resolve().parents[1]


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, TOOLS / filename)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


revtool = load("revtool_provenance_under_test", "revtool.py")
dossier = load("evidence_dossier_under_test", "evidence_dossier.py")


class Args:
    def __init__(self, **values):
        self.action = "import"
        self.path = None
        self.take = None
        self.author = "tester"
        self.__dict__.update(values)


class LedgerParsingTests(unittest.TestCase):
    def ledger(self, rows):
        fd, path = tempfile.mkstemp(suffix=".jsonl")
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            for row in rows:
                stream.write(json.dumps(row, sort_keys=True) + "\n")
        self.addCleanup(lambda: os.path.exists(path) and os.unlink(path))
        return path

    def test_identical_legacy_duplicate_is_collapsed(self):
        row = {"_t": "notes", "target": "sub_401000", "body": "same",
               "author": "a", "tier": "guess", "updated_at": "t"}
        _meta, claims, _events = revtool.read_ledger(self.ledger([row, row]))
        self.assertEqual(len(claims), 1)

    def test_conflicting_duplicate_is_rejected(self):
        first = {"_t": "notes", "target": "sub_401000", "body": "same",
                 "author": "a", "tier": "guess", "updated_at": "t"}
        second = dict(first, author="b")
        with self.assertRaisesRegex(ValueError, "conflicting duplicate"):
            revtool.read_ledger(self.ledger([first, second]))

    def test_legacy_type_defaults_do_not_create_false_conflict(self):
        path = self.ledger([{"_t": "types", "name": "CIFWnd", "note": None}])
        _meta, claims, _events = revtool.read_ledger(path)
        row = next(iter(claims.values()))
        self.assertEqual(row["tier"], "guess")
        self.assertEqual(row["disputed"], 0)


class ConflictSafeImportTests(unittest.TestCase):
    def setUp(self):
        fd, self.db_path = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db_path)
        self.con = revtool.open_db(self.db_path)
        revtool.meta_set(self.con, "target_sha256", revtool.TARGET_SHA256)
        self.con.execute(
            "INSERT INTO globals(orig_name,new_name,kind,tier,author,disputed) "
            "VALUES('sub_401000','LocalName','func','mechanism','alice',0)")
        self.con.commit()

    def tearDown(self):
        self.con.close()
        if os.path.exists(self.db_path):
            os.unlink(self.db_path)

    def test_import_records_conflict_without_overwrite(self):
        local = revtool.db_claims(self.con)[("globals", ("sub_401000",))]
        remote = dict(local, new_name="RemoteName", author="bob")
        rows = [revtool.ledger_meta(self.con), remote]
        fd, ledger = tempfile.mkstemp(suffix=".jsonl")
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            stream.write("\n".join(revtool.stable_json(row) for row in rows) + "\n")
        try:
            rc = revtool.cmd_sync(Args(path=ledger), self.con)
            self.assertEqual(rc, 2)
            value = self.con.execute(
                "SELECT new_name FROM globals WHERE orig_name='sub_401000'").fetchone()[0]
            self.assertEqual(value, "LocalName")
            self.assertEqual(
                self.con.execute("SELECT count(*) FROM sync_conflicts WHERE resolved=0").fetchone()[0],
                1,
            )
        finally:
            os.unlink(ledger)


class BindingContractTests(unittest.TestCase):
    def test_bind_rejects_pointer_declarator_before_creating_a_phantom_type(self):
        fd, db_path = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(db_path)
        con = revtool.open_db(db_path)
        try:
            rc = revtool.cmd_bind(
                Args(func="sub_401000", var="self", type="CIFWnd*", tier="mechanism"),
                con,
            )
            self.assertEqual(rc, 1)
            self.assertEqual(con.execute("SELECT count(*) FROM types").fetchone()[0], 0)
        finally:
            con.close()
            os.unlink(db_path)


class DossierParsingTests(unittest.TestCase):
    def test_stale_graph_error_names_the_refresh_command(self):
        health = {"ok": False, "checks": [
            {"name": "evidence-graph", "ok": False},
        ]}
        with mock.patch.object(dossier, "doctor", return_value=health), self.assertRaisesRegex(
            RuntimeError, r"refresh_evidence_graph\.py, then rerun doctor"
        ):
            dossier.build_dossier("0x401000")

    def test_dossier_rejects_old_or_future_planner_schemas(self):
        dossier.require_plan_schema({"schema": "sro-ui-family-plan-v7"})
        for schema in ("sro-ui-family-plan-v6", "sro-ui-family-plan-v999"):
            with self.subTest(schema=schema), self.assertRaisesRegex(
                ValueError, "refresh_evidence_graph"
            ):
                dossier.require_plan_schema({"schema": schema})

    def test_direct_call_targets_are_extracted_without_claiming_jumps(self):
        rows = [
            {"type": "call", "opcode": "call 0x53dfa0"},
            {"type": "call", "opcode": "call 0x00a06ad0"},
            {"type": "jmp", "opcode": "jmp 0x123456"},
        ]
        self.assertEqual(dossier.direct_targets(rows), [0x53DFA0, 0xA06AD0])

    def test_planner_class_roles_are_not_read_from_removed_family_fields(self):
        row = {
            "selectedDispatchFamilies": ["Derived"],
            "providerDeclaredFamilies": ["AuthoredClass"],
            "familyEvidence": {
                "Derived": ["RTTI class-selected slot"],
                "BaseConsumer": ["inherited RTTI slot"],
                "AuthoredClass": ["exact provider decorated symbol"],
            },
        }
        got = dossier.plan_class_relationships(row)
        self.assertEqual([x["name"] for x in got["selectedDispatch"]], ["Derived"])
        self.assertEqual(
            [x["name"] for x in got["providerDeclarations"]], ["AuthoredClass"])
        self.assertEqual(
            [x["name"] for x in got["inheritedDispatch"]], ["BaseConsumer"])

    def test_non_vtable_class_research_exposes_direct_base_context(self):
        plan = {
            "families": [{
                "name": "Derived",
                "directBases": ["PrimaryBase"],
                "vtables": [{"va": 0x9000, "objectOffset": 0, "targets": 4}],
                "planningStatus": "known-surface",
                "inventoryStatus": "open-non-dispatch-inventory",
            }]
        }
        row = {
            "researchFamilies": ["Derived"],
            "selectedDispatchFamilies": [],
            "providerDeclaredFamilies": [],
        }
        self.assertEqual(
            dossier.plan_class_context(plan, row),
            [{
                "className": "Derived",
                "attribution": ["Revtool semantic class hypothesis"],
                "directBases": ["PrimaryBase"],
                "vtables": [{"va": 0x9000, "objectOffset": 0, "targets": 4}],
                "planningStatus": "known-surface",
                "inventoryStatus": "open-non-dispatch-inventory",
                "ownershipClaim": False,
            }],
        )


class StructuralRttiOwnershipTests(unittest.TestCase):
    def test_class_and_vtable_commands_share_the_pe_rtti_reader(self):
        import inspect

        self.assertIn("_best_rtti_index", inspect.getsource(revtool.cmd_classes))
        self.assertIn("_best_rtti_index", inspect.getsource(revtool._collect_vtable_addrs))
        self.assertIn("scan_vtables", inspect.getsource(revtool._structural_rtti_index))
        self.assertNotIn("_rtti_index(dump)", inspect.getsource(revtool.cmd_classes))


class EvidenceGraphTests(unittest.TestCase):
    def test_stale_derived_source_hash_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            paths = {name: str(root / name) for name in (
                "target", "binary_ninja_hlil", "binary_ninja_functions",
                "binary_ninja_calls", "binary_ninja_coverage",
                "rizin_function_index", "rizin_project",
                "reconciled_function_index", "progress_data", "ui_family_plan",
                "exact_provider_manifest",
            )}
            ledger = root / "ledger.jsonl"

            def write(name, content):
                Path(paths[name]).write_bytes(content)
                return hashlib.sha256(content).hexdigest()

            target_hash = write("target", b"retail")
            write("binary_ninja_hlil", b"hlil")
            spans_hash = write("binary_ninja_functions", b"spans")
            calls_hash = write("binary_ninja_calls", b"calls")
            project_hash = write("rizin_project", b"rizin")
            provider_hash = write("exact_provider_manifest", b"providers")
            ledger.write_bytes(b"ledger")
            ledger_hash = hashlib.sha256(b"ledger").hexdigest()
            coverage = {
                "schema": "sro-binary-ninja-text-coverage-v3",
                "binary_sha256": target_hash,
                "function_spans_sha256": spans_hash,
                "direct_calls_sha256": calls_hash,
            }
            coverage_hash = write(
                "binary_ninja_coverage", json.dumps(coverage).encode())
            rizin = {"target_sha256": target_hash,
                     "rizin_project_sha256": project_hash}
            rizin_hash = write("rizin_function_index", json.dumps(rizin).encode())
            reconciled = {
                "target_sha256": target_hash,
                "sources": {
                    "rizin_index_sha256": rizin_hash,
                    "binary_ninja_functions_sha256": spans_hash,
                    "binary_ninja_coverage_sha256": coverage_hash,
                    "provider_manifest_sha256": provider_hash,
                    "rizin_project_sha256": project_hash,
                },
            }
            reconciled_hash = write(
                "reconciled_function_index", json.dumps(reconciled).encode())
            progress_sources = {
                "functionIndexSha256": reconciled_hash,
                "providerManifestSha256": provider_hash,
                "binaryNinjaCoverageSha256": coverage_hash,
                "rizinProjectSha256": project_hash,
                "revtoolLedgerSha256": ledger_hash,
            }
            write("progress_data", json.dumps({
                "targetSha256": target_hash, "sources": progress_sources,
            }).encode())
            write("ui_family_plan", json.dumps({
                "schema": "sro-ui-family-plan-v7",
                "targetSha256": target_hash,
                "sources": {
                    "functionIndexSha256": reconciled_hash,
                    "providerManifestSha256": provider_hash,
                    "rizinProjectSha256": project_hash,
                    "binaryNinjaCallsSha256": calls_hash,
                    "revtoolLedgerSha256": ledger_hash,
                },
            }).encode())

            original_paths = revtool.evidence_artifact_paths
            original_target = revtool.TARGET_SHA256
            revtool.evidence_artifact_paths = lambda _con, _args: paths
            revtool.TARGET_SHA256 = target_hash
            self.addCleanup(setattr, revtool, "evidence_artifact_paths", original_paths)
            self.addCleanup(setattr, revtool, "TARGET_SHA256", original_target)
            args = Args(db=str(root / "db"), ledger=str(ledger))

            self.assertEqual(revtool.validate_evidence_graph(None, args), [])
            broken = json.loads(Path(paths["progress_data"]).read_text())
            broken["sources"]["functionIndexSha256"] = "0" * 64
            Path(paths["progress_data"]).write_text(json.dumps(broken))
            issues = revtool.validate_evidence_graph(None, args)
            self.assertTrue(any("progress_data.functionIndexSha256" in x for x in issues))


if __name__ == "__main__":
    unittest.main()
