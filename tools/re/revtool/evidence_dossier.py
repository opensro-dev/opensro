"""Build one hash-bound, multi-analyzer function evidence dossier.

The dossier is the required ingress for byte matching. It does not choose an
analyzer winner: it records byte facts, exact-provider facts, structural
anchors, analyzer observations, semantic claims, conflicts, and unknowns as
separate fields.
"""

from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import csv
from functools import lru_cache
import hashlib
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    # importlib-based tests and other library consumers do not automatically
    # add the loaded module's directory to sys.path as script execution does.
    sys.path.insert(0, str(HERE))
from pe_image import PEImage

import revtool_paths as _paths

# Every path resolves through revtool_paths (repo layout + REVTOOL_*
# overrides). The research layout this file grew up in rooted the
# derived tree at <workspace>/temp/sro-native-exact-all-cpp-v1150-current,
# a directory that no longer exists; the tree now lives under the
# per-machine data dir.
CURRENT = Path(_paths.EVIDENCE_CURRENT)
PROGRESS = Path(_paths.EVIDENCE_PROGRESS)
TARGET = Path(_paths.DEFAULT_BINARY)
DB = Path(_paths.DEFAULT_DB)
DUMP = Path(_paths.DEFAULT_DUMP)
LEDGER = Path(_paths.DEFAULT_LEDGER)
LOCK = Path(_paths.DEFAULT_EVIDENCE_LOCK)
PROVIDERS = Path(_paths.DATA_DIR) / "native/replacements/results/all-exact/provider-manifest.json"
RECONCILED = CURRENT / "function-index-reconciled.json"
RIZIN_INDEX = CURRENT / "function-index.json"
BN_SPANS = CURRENT / "binary-ninja-function-spans.csv"
BN_COVERAGE = CURRENT / "binary-ninja-text-coverage.json"
BN_CALLS = CURRENT / "binary-ninja-direct-calls.csv"
PLAN = PROGRESS / "plan.json"
PROGRESS_DATA = PROGRESS / "progress.json"
TARGET_SHA256 = "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a"
SUPPORTED_PLAN_SCHEMA = "sro-ui-family-plan-v7"


@lru_cache(maxsize=None)
def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


@lru_cache(maxsize=None)
def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def require_plan_schema(document: dict[str, Any]) -> None:
    actual = document.get("schema")
    if actual != SUPPORTED_PLAN_SCHEMA:
        raise ValueError(
            "unsupported UI family plan schema %r; expected %s; "
            "regenerate the plan with its producer, then rerun "
            "`revtool doctor --refresh-lock`" % (actual, SUPPORTED_PLAN_SCHEMA)
        )


def integer(value: Any) -> int:
    return int(value, 0) if isinstance(value, str) else int(value)


def resolve_target(con: sqlite3.Connection, text: str) -> tuple[int, str | None]:
    raw = text.strip()
    try:
        va = int(raw, 0) if raw.lower().startswith("0x") else int(raw, 16)
        row = con.execute("SELECT name FROM functions WHERE addr=?",
                          (f"{va:08x}",)).fetchone()
        return va, row[0] if row else None
    except ValueError:
        pass
    row = con.execute("SELECT addr,name FROM functions WHERE name=?", (raw,)).fetchone()
    if not row:
        alias = con.execute("SELECT orig_name FROM globals WHERE new_name=?", (raw,)).fetchone()
        if alias:
            row = con.execute("SELECT addr,name FROM functions WHERE name=?", alias).fetchone()
    if not row:
        raise ValueError(f"cannot resolve function target: {text}")
    return int(row[0], 16), row[1]


@lru_cache(maxsize=None)
def load_bn_index(path: Path) -> dict[int, dict[str, Any]]:
    result = {}
    with path.open(encoding="utf-8", newline="") as f:
        for row in csv.DictReader(f):
            result[int(row["address"])] = {
                key: (int(value) if key != "name" and value else value)
                for key, value in row.items()
            }
    return result


def load_bn_row(path: Path, va: int) -> dict[str, Any] | None:
    return load_bn_index(path).get(va)


@lru_cache(maxsize=None)
def json_va_index(path: Path, collection: str) -> dict[int, dict[str, Any]]:
    return {
        integer(row["va"]): row
        for row in load_json(path).get(collection, [])
        if row.get("va") is not None
    }


def raw_disassembly(target: Path, va: int, size: int) -> list[dict[str, Any]]:
    rizin = Path(shutil.which("rizin.exe") or r"C:\Program Files\Rizin\bin\rizin.exe")
    if not rizin.is_file():
        return []
    command = [str(rizin), "-q", "-N", "-c", "e scr.color=false",
               "-c", f"pDj {size} @ 0x{va:08X}", "-c", "q", str(target)]
    result = subprocess.run(command, capture_output=True, text=True, encoding="utf-8")
    if result.returncode != 0:
        return []
    start, end = result.stdout.find("["), result.stdout.rfind("]")
    if start < 0 or end < start:
        return []
    rows = json.loads(result.stdout[start:end + 1])
    return [{"va": row.get("offset"), "bytes": row.get("bytes"),
             "opcode": row.get("opcode"), "type": row.get("type")}
            for row in rows if isinstance(row, dict)]


@lru_cache(maxsize=None)
def pe_image(path: Path) -> PEImage:
    return PEImage(path)


def revtool_evidence(con: sqlite3.Connection, va: int, orig_name: str | None) -> dict[str, Any]:
    names = {orig_name, f"sub_{va:x}", f"sub_{va:08x}"}
    names.discard(None)
    global_rows = []
    for name in names:
        row = con.execute(
            "SELECT orig_name,new_name,kind,note,tier,author,evidence,updated_at,"
            "disputed,dispute_reason,basis,method,blindspots FROM globals WHERE orig_name=?",
            (name,)).fetchone()
        if row:
            cols = ["orig_name", "new_name", "kind", "note", "tier", "author",
                    "evidence", "updated_at", "disputed", "dispute_reason", "basis",
                    "method", "blindspots"]
            global_rows.append(dict(zip(cols, row)))
    note_rows = con.execute(
        "SELECT id,target,body,author,tier,updated_at,basis,method,blindspots "
        "FROM notes WHERE target IN (?,?,?) OR target LIKE ? OR target LIKE ? ORDER BY id",
        (f"{va:08x}", f"0x{va:08x}", orig_name or "", (orig_name or "") + "@%",
         f"sub_{va:x}@%"),).fetchall()
    note_cols = ["id", "target", "body", "author", "tier", "updated_at", "basis",
                 "method", "blindspots"]
    history = con.execute(
        "SELECT event_id,ts,author,kind,ckey,action,old,new,reason,target_sha256,"
        "dump_sha256,tool_version FROM history WHERE ckey LIKE ? OR ckey LIKE ? ORDER BY id",
        (f"%sub_{va:x}%", f"%{va:08x}%"),).fetchall()
    history_cols = ["event_id", "ts", "author", "kind", "ckey", "action", "old",
                    "new", "reason", "target_sha256", "dump_sha256", "tool_version"]
    return {"claims": global_rows, "notes": [dict(zip(note_cols, r)) for r in note_rows],
            "events": [dict(zip(history_cols, r)) for r in history]}


# The authored-reconstruction trees the dossier cross-reads. They are
# per-machine authoring state, not repo content: REVTOOL_CORPUS_ROOT
# points at them where they exist; the default (the data dir) simply
# finds none, and the dossier reports no related files.
CORPUS_ROOT = Path(os.environ.get("REVTOOL_CORPUS_ROOT") or _paths.DATA_DIR)


@lru_cache(maxsize=1)
def related_file_corpus() -> tuple[tuple[str, str], ...]:
    roots = [CORPUS_ROOT / "native/include", CORPUS_ROOT / "native/replacements",
             CORPUS_ROOT / "native/workbench", CORPUS_ROOT / "investigations",
             CORPUS_ROOT / "wip"]
    suffixes = {".cpp", ".hpp", ".h", ".md", ".json"}
    corpus = []
    for root in roots:
        if not root.is_dir():
            continue
        for path in root.rglob("*"):
            if not path.is_file() or path.suffix.lower() not in suffixes:
                continue
            try:
                text = path.read_text(encoding="utf-8", errors="ignore")
            except OSError:
                continue
            corpus.append((path.relative_to(CORPUS_ROOT).as_posix(), text))
    return tuple(corpus)


def related_files(va: int, names: list[str], dependency_vas: list[int]) -> dict[str, list[str]]:
    all_vas = [va] + dependency_vas
    primary_patterns = [f"0x{va:08x}", f"0x{va:08X}", f"sub_{va:x}", f"{va:08x}"]
    dependency_patterns = []
    patterns = names[:] + primary_patterns
    for address in all_vas:
        address_patterns = (f"0x{address:08x}", f"0x{address:08X}",
                            f"sub_{address:x}", f"{address:08x}")
        patterns.extend(address_patterns)
        if address != va:
            dependency_patterns.extend(address_patterns)
    primary, definitions, references = [], [], []
    for relative, text in related_file_corpus():
        if not any(pattern and pattern in text for pattern in patterns):
            continue
        if any(pattern in text for pattern in primary_patterns):
            primary.append(relative)
        elif any(f"sub_{address:x}_" in relative.lower() for address in dependency_vas):
            definitions.append(relative)
        else:
            references.append(relative)
    return {
        "primary": sorted(set(primary)),
        "dependencyDefinitions": sorted(set(definitions)),
        # Cross-references are useful corpus examples but can number in the
        # hundreds for allocators. Keep the machine-readable handoff focused.
        "crossReferences": sorted(set(references))[:20],
    }


def namespace_related_files(related: dict[str, list[str]]) -> dict[str, list[dict[str, str]]]:
    """Keep current/exact evidence separate from snapshots and research leads."""
    namespaced: dict[str, list[dict[str, str]]] = {}
    for category, paths in related.items():
        for path in paths:
            normalized = path.replace("\\", "/").lower()
            filename = Path(normalized).name
            if filename == "evidence-ingress.json" or filename == "evidence-lock.json":
                namespace, authority = "INGRESS SNAPSHOT", "historical-non-authoritative"
            elif filename == "evidence-final.json":
                namespace, authority = "PROMOTION SNAPSHOT", "historical-non-authoritative"
            elif "/native/replacements/" in normalized:
                namespace, authority = "EXACT", "exact-provider-package"
            elif "/native/include/" in normalized:
                namespace, authority = "CURRENT STRUCTURAL", "current-declaration"
            elif "/wip/" in normalized or "/investigations/" in normalized or "/workbench/" in normalized:
                namespace, authority = "WIP", "research-lead-non-authoritative"
            elif "/archive/" in normalized:
                namespace, authority = "ARCHIVED", "historical-non-authoritative"
            else:
                namespace, authority = "CURRENT STRUCTURAL", "current-repository-reference"
            namespaced.setdefault(namespace, []).append({
                "path": path,
                "relationship": category,
                "authority": authority,
            })
    return {key: rows for key, rows in sorted(namespaced.items())}


def direct_targets(instructions: list[dict[str, Any]]) -> list[int]:
    targets = set()
    for row in instructions:
        if row.get("type") != "call":
            continue
        match = re.search(r"\b0x([0-9a-fA-F]+)\b", row.get("opcode") or "")
        if match:
            targets.add(int(match.group(1), 16))
    return sorted(targets)


@lru_cache(maxsize=None)
def binary_ninja_direct_ref_index(path: Path) -> dict[int, list[dict[str, Any]]]:
    """Return source-attributed direct sites without rescanning all of `.text`."""
    refs: dict[int, list[dict[str, Any]]] = {}
    if not path.is_file():
        raise FileNotFoundError(f"Binary Ninja direct-call export is missing: {path}")
    with path.open(encoding="utf-8", newline="") as stream:
        for row in csv.DictReader(stream):
            raw_target = str(row.get("target_address") or "").strip()
            if raw_target:
                target = int(raw_target)
                refs.setdefault(target, []).append({
                    "source": "Binary Ninja",
                    "caller": int(row["caller_address"]),
                    "site": int(row["site_address"]),
                    "kind": str(row.get("kind") or "unknown"),
                })
    return refs


def binary_ninja_direct_refs(path: Path, target: int) -> list[dict[str, Any]]:
    return list(binary_ninja_direct_ref_index(path).get(target, ()))


@lru_cache(maxsize=None)
def doctor(db: Path, dump: Path, binary: Path) -> dict[str, Any]:
    command = [sys.executable, str(HERE / "revtool.py"), "--db", str(db),
               "--dump", str(dump), "--binary", str(binary), "doctor", "--json"]
    result = subprocess.run(command, capture_output=True, text=True, encoding="utf-8")
    try:
        report = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError("revtool doctor did not return JSON:\n" + result.stdout + result.stderr) from exc
    report["returncode"] = result.returncode
    return report


def plan_class_relationships(plan_row: dict[str, Any] | None) -> dict[str, list[dict[str, Any]]]:
    """Translate planner-v5 class evidence without inventing ownership.

    The old dossier reader looked for ``confirmedVas``/``candidateVas`` on
    family rows.  Planner v5 deliberately removed those fields, so the lookup
    silently returned an empty family set even while the embedded function row
    named its bounded RTTI relationship.  Keep the roles separate here:
    vtable dispatch reachability, an authored reconstruction declaration, and
    inherited dispatch are different claims.
    """
    relationships = {
        "selectedDispatch": [],
        "providerDeclarations": [],
        "inheritedDispatch": [],
        "other": [],
    }
    if not plan_row:
        return relationships
    selected = set(plan_row.get("selectedDispatchFamilies") or ())
    declared = set(plan_row.get("providerDeclaredFamilies") or ())
    evidence = plan_row.get("familyEvidence") or {}
    names = set(evidence) | selected | declared
    for name in sorted(names):
        reasons = list(evidence.get(name) or ())
        row = {"name": name, "evidence": reasons}
        if name in selected:
            relationships["selectedDispatch"].append(row)
        elif name in declared:
            relationships["providerDeclarations"].append(row)
        elif "inherited RTTI slot" in reasons:
            relationships["inheritedDispatch"].append(row)
        else:
            relationships["other"].append(row)
    return relationships


def plan_class_context(
    plan: dict[str, Any], plan_row: dict[str, Any] | None
) -> list[dict[str, Any]]:
    """Expose RTTI direct-base facts for class-attributed non-vtable work.

    A destructor, constructor, factory, or callback can carry strong semantic
    class evidence without occupying a vtable cell. The old dossier omitted
    the family's direct-base graph in that case, which made a secondary base
    look like a plausible direct base. This is context only: it does not turn
    a Revtool name or provider declaration into original body ownership.
    """
    if not plan_row:
        return []
    sources: dict[str, set[str]] = {}
    for field, label in (
        ("selectedDispatchFamilies", "bounded RTTI class-selected slot"),
        ("providerDeclaredFamilies", "exact-provider retail symbol"),
        ("researchFamilies", "Revtool semantic class hypothesis"),
    ):
        for name in plan_row.get(field) or ():
            sources.setdefault(str(name), set()).add(label)
    families = {
        str(row.get("name")): row for row in plan.get("families") or []
        if row.get("name")
    }
    result = []
    for name in sorted(sources):
        family = families.get(name)
        if not family:
            continue
        result.append({
            "className": name,
            "attribution": sorted(sources[name]),
            "directBases": list(family.get("directBases") or ()),
            "vtables": list(family.get("vtables") or ()),
            "planningStatus": family.get("planningStatus"),
            "inventoryStatus": family.get("inventoryStatus"),
            "ownershipClaim": False,
        })
    return result


def build_dossier(target_text: str, *, db: Path = DB, dump: Path = DUMP,
                  binary: Path = TARGET, allow_stale: bool = False) -> dict[str, Any]:
    health = doctor(db, dump, binary)
    if not health.get("ok") and not allow_stale:
        # Name every failed check with its artifact detail (the path or
        # hash doctor actually saw), so the refusal says what to rebuild
        # instead of making the reader guess which of the derived
        # artifacts is missing or stale.
        failed = [row for row in health.get("checks", []) if not row.get("ok")]
        lines = ["evidence preflight failed:"]
        for row in failed:
            detail = row.get("detail") or ""
            lines.append(f"  - {row['name']}: {detail}" if detail else f"  - {row['name']}")
        if any(row["name"] == "evidence-graph" for row in failed):
            lines.append(
                "derived evidence is stale or missing under "
                f"{_paths.EVIDENCE_DIR}: regenerate the named artifacts with "
                "their producers (each lock record names its recipe), then "
                "rerun `revtool doctor --refresh-lock`")
        raise RuntimeError("\n".join(lines))
    con = sqlite3.connect(f"file:{db.as_posix()}?mode=ro", uri=True)
    try:
        va, orig_name = resolve_target(con, target_text)
        pe = pe_image(binary)
        if sha256(binary) != TARGET_SHA256:
            raise ValueError("target executable hash is not pinned v1.150")
        providers_by_va = json_va_index(PROVIDERS, "providers")
        provider = providers_by_va.get(va)
        reconciled = json_va_index(RECONCILED, "functions").get(va)
        rizin = json_va_index(RIZIN_INDEX, "functions").get(va)
        bn = load_bn_row(BN_SPANS, va)
        plan = load_json(PLAN) if PLAN.is_file() else {}
        if plan:
            require_plan_schema(plan)
        plan_row = (plan.get("functions") or {}).get(str(va))
        class_relationships = plan_class_relationships(plan_row)
        class_context = plan_class_context(plan, plan_row)
        families = [
            dict(row, role=role)
            for role, rows in class_relationships.items()
            for row in rows
        ]

        extent_sources = {}
        if provider:
            extent_sources["exact_provider"] = integer(provider["size"])
        if reconciled and integer(reconciled.get("size", 0)):
            extent_sources["reconciled_hypothesis"] = integer(reconciled["size"])
        if bn and int(bn.get("highest_address", 0)) > va:
            extent_sources["binary_ninja"] = int(bn["highest_address"]) - va
        if rizin and integer(rizin.get("size", 0)):
            extent_sources["rizin"] = integer(rizin["size"])
        size = extent_sources.get("exact_provider") or extent_sources.get(
            "reconciled_hypothesis") or extent_sources.get("binary_ninja") or extent_sources.get(
            "rizin") or 32
        size = max(1, min(size, 0x10000))
        raw = pe.read_file_backed(va, size)
        instructions = raw_disassembly(binary, va, size)
        dependency_vas = sorted(set(direct_targets(instructions)) | {
            int(target) for target in (plan_row or {}).get("callees", [])
        })
        rev = revtool_evidence(con, va, orig_name)
        semantic_names = [
            row["new_name"] for row in rev["claims"]
            if row.get("new_name") and not row.get("disputed")
        ]
        if provider and provider.get("symbol"):
            semantic_names.append(str(provider["symbol"]))
        dependencies = []
        for dependency_va in dependency_vas:
            function_row = con.execute(
                "SELECT name FROM functions WHERE addr=?",
                (f"{dependency_va:08x}",),
            ).fetchone()
            dependency_orig = function_row[0] if function_row else None
            dependency_provider = providers_by_va.get(dependency_va)
            dependency_revtool = revtool_evidence(
                con, dependency_va, dependency_orig)
            dependencies.append({
                "va": f"0x{dependency_va:08X}",
                "origName": dependency_orig,
                "exactProvider": dependency_provider,
                "revtool": dependency_revtool,
            })
            semantic_names.extend(
                row["new_name"] for row in dependency_revtool["claims"]
                if row.get("new_name") and not row.get("disputed"))
        structural_analogues = []
        analogue_vas = set()
        for placement in (plan_row or {}).get("dispatchPlacements", []):
            if placement.get("role") != "selected" or placement.get("baseTarget") is None:
                continue
            analogue_va = int(placement["baseTarget"])
            analogue_vas.add(analogue_va)
            function_row = con.execute(
                "SELECT name FROM functions WHERE addr=?",
                (f"{analogue_va:08x}",),
            ).fetchone()
            analogue_orig = function_row[0] if function_row else None
            analogue_revtool = revtool_evidence(con, analogue_va, analogue_orig)
            structural_analogues.append({
                "relationship": "direct-base vtable slot",
                "className": placement.get("className"),
                "directBaseClass": placement.get("directBaseClass"),
                "slotOffset": int(placement["slotOffset"]),
                "va": f"0x{analogue_va:08X}",
                "origName": analogue_orig,
                "exactProvider": providers_by_va.get(analogue_va),
                "revtool": analogue_revtool,
            })
            semantic_names.extend(
                row["new_name"] for row in analogue_revtool["claims"]
                if row.get("new_name") and not row.get("disputed"))
        # A direct base can itself be unlabeled. Exact replacements selecting
        # the same PE32 vtable cell in other UI classes are useful contract-
        # shape examples, but slot coincidence is not semantic proof.
        selected_cells = {
            (int(item["objectOffset"]), int(item["slotOffset"]))
            for item in (plan_row or {}).get("dispatchPlacements", [])
            if item.get("role") == "selected"
        }
        same_slot = []
        for candidate in (plan.get("functions") or {}).values():
            candidate_va = int(candidate.get("va") or 0)
            if candidate_va == va or not candidate.get("exact") or not candidate.get("symbol"):
                continue
            cells = {
                (int(item["objectOffset"]), int(item["slotOffset"]))
                for item in candidate.get("dispatchPlacements", [])
                if item.get("role") == "selected"
            }
            if selected_cells.intersection(cells):
                same_slot.append(candidate)
        for candidate in sorted(
            same_slot, key=lambda row: (str(row.get("symbol")), int(row["va"]))
        )[:12]:
            analogue_va = int(candidate["va"])
            if analogue_va in analogue_vas:
                continue
            analogue_vas.add(analogue_va)
            candidate_placements = [
                item for item in candidate.get("dispatchPlacements", [])
                if (int(item["objectOffset"]), int(item["slotOffset"])) in selected_cells
            ]
            symbol = str(candidate["symbol"])
            structural_analogues.append({
                "relationship": "exact same-cell RTTI analogue (semantic clue, not proof)",
                "className": candidate.get("providerDeclaredClass"),
                "directBaseClass": None,
                "slotOffset": int(candidate_placements[0]["slotOffset"]),
                "va": f"0x{analogue_va:08X}",
                "origName": candidate.get("analyzerName"),
                "symbol": symbol,
                "exactProvider": providers_by_va.get(analogue_va),
                "revtool": candidate.get("revtoolClaim"),
            })
            semantic_names.append(symbol)
        direct_refs = binary_ninja_direct_refs(BN_CALLS, va)
        sources = {
            "exact_provider": bool(provider),
            "binary_ninja": bool(bn),
            "rizin": bool(rizin),
            "rtti_vtable": bool(reconciled and reconciled.get("rtti_vtable_slot")),
            "decoded_direct_call": bool(reconciled and reconciled.get("decoded_call_target")),
        }
        conflicts = []
        if bool(bn) != bool(rizin):
            conflicts.append("function entry is present in only one static analyzer")
        if len(set(extent_sources.values())) > 1:
            conflicts.append("function extent hypotheses disagree")
        unknowns = []
        if not provider:
            unknowns.append("byte extent is not exact-provider proven")
        active_revtool_claims = [
            row for row in rev["claims"] if not row.get("disputed")
        ]
        if not active_revtool_claims:
            unknowns.append("no shared Revtool semantic claim")
        if rev["claims"] and not active_revtool_claims:
            unknowns.append("all recorded semantic claims are disputed and non-authoritative")
        if not any(class_relationships.values()):
            unknowns.append(
                "no bounded RTTI dispatch relationship or exact-provider class declaration"
            )
        if not sources["decoded_direct_call"] and not sources["rtti_vtable"]:
            unknowns.append("no decoded direct-call or bounded RTTI entry anchor")

        artifact_paths = [binary, dump, LOCK, LEDGER, PROVIDERS, RECONCILED,
                          RIZIN_INDEX, BN_SPANS, BN_COVERAGE, BN_CALLS, PLAN,
                          PROGRESS_DATA]
        artifacts = {os.path.relpath(path, _paths.REPO_ROOT).replace(os.sep, "/"): sha256(path)
                     for path in artifact_paths if path.is_file()}
        related = related_files(
            va, semantic_names, sorted(set(dependency_vas) | analogue_vas))
        dossier = {
            "schema": "sro-function-evidence-dossier-v1",
            "targetSha256": TARGET_SHA256,
            "function": {"va": f"0x{va:08X}", "origName": orig_name,
                         "selectedSize": size, "extentSources": extent_sources,
                         "rawSha256": hashlib.sha256(raw).hexdigest(), "rawHex": raw.hex()},
            "proofState": "compiled-byte-exact" if provider else "extracted",
            "sourceMatrix": sources,
            "exactProvider": provider,
            "reconciledEntry": reconciled,
            "binaryNinja": bn,
            "rizin": rizin,
            "revtool": rev,
            "families": families,
            "classRelationships": class_relationships,
            "classContext": class_context,
            "planner": plan_row,
            "structuralReferences": {
                "alignedDataPointers": pe.pointer_refs(va),
                "directControlRefs": direct_refs,
            },
            "instructions": instructions,
            "dependencies": dependencies,
            "structuralAnalogues": structural_analogues,
            "relatedFiles": related,
            "relatedEvidence": namespace_related_files(related),
            "conflicts": conflicts,
            "unknowns": unknowns,
            "artifacts": artifacts,
            "preflight": health,
            "claimPolicy": {
                "byteTruth": "original PE / exact provider",
                "boundaryTruth": "exact provider only; other extents are hypotheses",
                "semanticTruth": "tiered claims; a readable name is not a recovered source symbol",
            },
        }
        canonical = json.dumps(dossier, sort_keys=True, separators=(",", ":")).encode("utf-8")
        dossier["dossierSha256"] = hashlib.sha256(canonical).hexdigest()
        return dossier
    finally:
        con.close()


def print_human(dossier: dict[str, Any]) -> None:
    f = dossier["function"]
    print(f"FUNCTION DOSSIER {f['va']}  {f.get('origName') or '<no Revtool symbol>'}")
    print(f"proof state: {dossier['proofState']}  selected bytes: {f['selectedSize']}")
    print("source matrix:")
    for name, present in dossier["sourceMatrix"].items():
        print(f"  {'YES' if present else 'NO ':3}  {name}")
    print("extent evidence:")
    for source, size in f["extentSources"].items():
        print(f"  {source:<26} {size}")
    if dossier["families"]:
        relationships = dossier.get("classRelationships") or {}
        selected = relationships.get("selectedDispatch") or []
        declared = relationships.get("providerDeclarations") or []
        inherited = relationships.get("inheritedDispatch") or []
        if selected:
            print("class-selected RTTI dispatch: " + ", ".join(
                row["name"] for row in selected))
        if declared:
            print("reconstruction declarations (not original body ownership): " + ", ".join(
                row["name"] for row in declared))
        if inherited:
            sample = ", ".join(row["name"] for row in inherited[:6])
            suffix = " ..." if len(inherited) > 6 else ""
            print(f"inherited dispatch surfaces: {len(inherited)} ({sample}{suffix})")
        placements = (dossier.get("planner") or {}).get("dispatchPlacements") or []
        for row in (item for item in placements if item.get("role") == "selected"):
            base = row.get("directBaseClass") or "no matching direct base table"
            target = row.get("baseTarget")
            replaced = f"0x{int(target):08X}" if target is not None else "no base slot"
            print(
                "  RTTI cell: %s vtable 0x%08X this+0x%X slot +0x%X; %s -> %s"
                % (
                    row["className"], int(row["tableVa"]), int(row["objectOffset"]),
                    int(row["slotOffset"]), base, replaced,
                )
            )
    if dossier.get("classContext"):
        print("class context (RTTI structure; not body ownership):")
        for row in dossier["classContext"]:
            bases = ", ".join(row.get("directBases") or ()) or "none recorded"
            sources = ", ".join(row.get("attribution") or ())
            print(f"  {row['className']}: direct base(s) {bases} [{sources}]")
    claims = dossier["revtool"]["claims"]
    active_claims = [row for row in claims if not row.get("disputed")]
    disputed_claims = [row for row in claims if row.get("disputed")]
    print("semantic claims:")
    if active_claims:
        for row in active_claims:
            print(f"  {row['orig_name']} -> {row['new_name']} "
                  f"[{row.get('tier') or '?'}; {row.get('basis') or '?'}]")
    else:
        print("  NONE")
    if disputed_claims:
        print("disputed semantic claims (not active evidence):")
        for row in disputed_claims:
            print(
                f"  {row['orig_name']} -/-> {row['new_name']} "
                f"[{row.get('dispute_reason') or 'no reason recorded'}]"
            )
    print("direct dependencies:")
    if dossier.get("dependencies"):
        for dependency in dossier["dependencies"]:
            claims = dependency.get("revtool", {}).get("claims", [])
            labels = ", ".join(
                f"{row['new_name']}[{row.get('tier') or '?'}]"
                for row in claims if row.get("new_name") and not row.get("disputed")) or "unlabeled"
            proof = "exact" if dependency.get("exactProvider") else "unproven"
            print(f"  {dependency['va']}  {labels}  ({proof})")
    else:
        print("  none")
    print("dispatch structural analogues:")
    if dossier.get("structuralAnalogues"):
        for analogue in dossier["structuralAnalogues"]:
            revtool = analogue.get("revtool") or {}
            claims = revtool.get("claims", []) if isinstance(revtool, dict) else []
            labels = ", ".join(
                f"{row['new_name']}[{row.get('tier') or '?'}]"
                for row in claims if row.get("new_name") and not row.get("disputed")) or "unlabeled"
            if analogue.get("symbol"):
                labels = str(analogue["symbol"])
            proof = "exact" if analogue.get("exactProvider") else "unproven"
            print(
                f"  slot +0x{int(analogue['slotOffset']):X}: "
                f"{analogue.get('className') or analogue.get('directBaseClass') or 'other class'} "
                f"{analogue['va']}  {labels}  ({proof}; {analogue['relationship']})"
            )
    else:
        print("  none")
    print("conflicts:")
    for item in dossier["conflicts"] or ["none"]:
        print("  " + item)
    print("unknowns:")
    for item in dossier["unknowns"] or ["none"]:
        print("  " + item)
    print("instructions:")
    for row in dossier["instructions"]:
        print(f"  0x{int(row['va']):08X}  {row.get('bytes') or '':<16} {row.get('opcode')}")
    print("related repository evidence (namespaced):")
    related = dossier.get("relatedEvidence") or {}
    shown = False
    namespace_order = (
        "EXACT", "CURRENT STRUCTURAL", "LIVE", "INGRESS SNAPSHOT",
        "PROMOTION SNAPSHOT", "WIP", "ARCHIVED",
    )
    for namespace in namespace_order:
        rows = related.get(namespace) or []
        if not rows:
            continue
        shown = True
        print(f"  {namespace}:")
        for row in rows:
            print(f"    {row['path']}  [{row['authority']}; {row['relationship']}]")
    if not shown:
        print("  none")
    print(f"dossier sha256: {dossier['dossierSha256']}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "target", nargs="+", help="VA, original dump symbol, or applied Revtool name",
    )
    parser.add_argument("--db", type=Path, default=DB)
    parser.add_argument("--dump", type=Path, default=DUMP)
    parser.add_argument("--binary", type=Path, default=TARGET)
    parser.add_argument("--out", type=Path, default=None,
                        help="write canonical JSON dossier here")
    parser.add_argument(
        "--batch-out-dir", type=Path,
        help="write one <VA>.json dossier per target after the complete batch succeeds",
    )
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--json", action="store_true", help="print JSON instead of summary")
    parser.add_argument("--allow-stale", action="store_true",
                        help="diagnostic escape hatch; dossier remains marked failed preflight")
    args = parser.parse_args()
    if args.workers < 1:
        parser.error("--workers must be positive")
    if len(args.target) > 1 and not args.batch_out_dir:
        parser.error("multiple targets require --batch-out-dir")
    if args.batch_out_dir and args.out:
        parser.error("--out and --batch-out-dir are mutually exclusive")
    db = args.db.resolve()
    dump = args.dump.resolve()
    binary = args.binary.resolve()
    if args.batch_out_dir:
        # Warm every shared immutable input before threads start. Per-target
        # work is then limited to SQLite queries, disassembly, and filtering
        # the in-memory related-file corpus.
        health = doctor(db, dump, binary)
        if not health.get("ok") and not args.allow_stale:
            failed = [row["name"] for row in health.get("checks", []) if not row.get("ok")]
            raise RuntimeError("evidence preflight failed: " + ", ".join(failed))
        sha256(binary)
        pe_image(binary)
        json_va_index(PROVIDERS, "providers")
        json_va_index(RECONCILED, "functions")
        json_va_index(RIZIN_INDEX, "functions")
        load_bn_index(BN_SPANS)
        binary_ninja_direct_ref_index(BN_CALLS)
        load_json(PLAN) if PLAN.is_file() else {}
        related_file_corpus()
        dossiers: list[dict[str, Any]] = []
        with ThreadPoolExecutor(max_workers=min(args.workers, len(args.target))) as executor:
            futures = {
                executor.submit(
                    build_dossier, target, db=db, dump=dump, binary=binary,
                    allow_stale=args.allow_stale,
                ): target
                for target in args.target
            }
            for future in as_completed(futures):
                target = futures[future]
                try:
                    dossiers.append(future.result())
                except Exception as error:
                    raise RuntimeError(f"dossier batch failed for {target}: {error}") from error
        dossiers.sort(key=lambda row: integer(row["function"]["va"]))
        args.batch_out_dir.mkdir(parents=True, exist_ok=True)
        for dossier in dossiers:
            va = integer(dossier["function"]["va"])
            path = args.batch_out_dir / f"{va:08x}.json"
            path.write_text(json.dumps(dossier, indent=2) + "\n", encoding="utf-8")
        print(json.dumps({
            "schema": "sro-function-evidence-dossier-batch-v1",
            "count": len(dossiers),
            "output": str(args.batch_out_dir),
        }, indent=2))
        return 0

    dossier = build_dossier(args.target[0], db=db, dump=dump,
                            binary=binary, allow_stale=args.allow_stale)
    if args.out:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(json.dumps(dossier, indent=2) + "\n", encoding="utf-8")
        print(f"wrote {args.out}")
    if args.json:
        print(json.dumps(dossier, indent=2))
    else:
        print_human(dossier)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (FileNotFoundError, RuntimeError, ValueError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(2)
