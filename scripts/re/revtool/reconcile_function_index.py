#!/usr/bin/env python3
"""Reconcile Rizin and Binary Ninja function starts without inventing ownership.

The progress treemap is a byte partition, but function discovery is not: two
analyzers can disagree, functions can share blocks, and raw analyzer extents can
be wildly oversized.  This tool therefore reconciles *entry points* and keeps
the evidence tier on every row.  It never promotes a start hidden inside a
byte-exact provider, and it uses Binary Ninja's decoded block span instead of a
provisional Rizin max-bound whenever Binary Ninja knows the same entry point.

All starts remain hypotheses unless independently corroborated.  The output is
100% explicit about that distinction:

* ``exact-provider``: byte-exact provider manifest;
* ``cross-analyzer``: Rizin and Binary Ninja agree on the entry point;
* ``decoded-call-target``: an analyzer decoded a direct call to the entry;
* ``rtti-vtable-slot``: a candidate lies in a structurally identified MSVC
  RTTI vtable prefix;
* ``binary-ninja-anchored`` / ``rizin-anchored``: one analyzer plus a
  post-INT3 boundary;
* ``binary-ninja-hypothesis`` / ``rizin-hypothesis``: one analyzer only.

Generic aligned dwords in ``.rdata``/``.data`` are deliberately *not* entry
evidence. UTF-16 and other ordinary data frequently decode numerically as a
``.text`` address (for example ``55 00 49 00`` -> ``0x00490055``), which was
the source of tens of thousands of false Binary Ninja functions in an earlier
reconciliation pass.

Usage::

    ./re-python.cmd tools/re/cpp-replacement-proof/reconcile_function_index.py
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import shutil
from pathlib import Path
from typing import Any

from replacement_workspace import Workspace, require_file, sha256
from exact_progress_treemap import load_rizin_call_graph
from msvc_rtti import scan_vtables


SCHEMA = "sro-reconciled-function-index-v1"


def _integer(value: Any, field: str) -> int:
    if isinstance(value, int):
        return value
    try:
        return int(str(value), 0)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{field} is not an integer: {value!r}") from exc


def _sections(image: bytes) -> list[dict[str, int | str]]:
    pe = int.from_bytes(image[0x3C:0x40], "little")
    section_count = int.from_bytes(image[pe + 6:pe + 8], "little")
    optional_size = int.from_bytes(image[pe + 20:pe + 22], "little")
    optional = pe + 24
    image_base = int.from_bytes(image[optional + 28:optional + 32], "little")
    table = optional + optional_size
    result: list[dict[str, int | str]] = []
    for index in range(section_count):
        entry = table + index * 40
        result.append(
            {
                "name": image[entry:entry + 8].rstrip(b"\0").decode("ascii", "replace"),
                "va": image_base + int.from_bytes(image[entry + 12:entry + 16], "little"),
                "virtual_size": int.from_bytes(image[entry + 8:entry + 12], "little"),
                "raw_size": int.from_bytes(image[entry + 16:entry + 20], "little"),
                "raw_offset": int.from_bytes(image[entry + 20:entry + 24], "little"),
                "characteristics": int.from_bytes(image[entry + 36:entry + 40], "little"),
            }
        )
    return result


def _load_binary_ninja_rows(path: Path) -> dict[int, dict[str, Any]]:
    rows: dict[int, dict[str, Any]] = {}
    with path.open("r", encoding="utf-8", newline="") as stream:
        for ordinal, raw in enumerate(csv.DictReader(stream), 2):
            start = _integer(raw.get("address"), f"{path}:{ordinal}:address")
            if start in rows:
                raise ValueError(f"{path}:{ordinal}: duplicate Binary Ninja start 0x{start:08X}")
            rows[start] = {
                "start": start,
                "total_bytes": _integer(raw.get("total_bytes"), "total_bytes"),
                "lowest_address": _integer(raw.get("lowest_address"), "lowest_address"),
                "highest_address": _integer(raw.get("highest_address"), "highest_address"),
                "basic_blocks": _integer(raw.get("block_count"), "block_count"),
                "name": str(raw.get("name") or f"sub_{start:x}"),
            }
    return rows


def _rtti_vtable_targets(
    image: bytes,
    sections: list[dict[str, int | str]],
    text_va: int,
    mapped_text_end: int,
    candidate_starts: set[int],
    strong_starts: set[int],
) -> set[int]:
    """Return candidates stored in structurally validated MSVC vtables.

    ``strong_starts`` remains in the signature for compatibility with callers,
    but is intentionally not a gate.  Requiring slot zero to be strong before
    RTTI may strengthen it is circular and hid whole real class surfaces when
    their runtime-class accessor was referenced only through the vtable.
    """
    del strong_starts
    return {
        target
        for record in scan_vtables(
            image, sections, text_va, mapped_text_end, candidate_starts
        )
        for target in record.targets
    }


def reconcile(
    rizin_index: dict[str, Any],
    binary_ninja_rows: dict[int, dict[str, Any]],
    providers: list[dict[str, Any]],
    text: bytes,
    mapped_text_size: int,
    decoded_call_targets: set[int],
    rtti_vtable_targets: set[int] | None = None,
) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    # RTTI/vtable discovery is a corroborating evidence source.  Keeping it
    # optional preserves the reconciler's useful core contract for fixtures,
    # older callers, and binaries without MSVC RTTI instead of turning absent
    # metadata into a hard failure.
    rtti_vtable_targets = set(rtti_vtable_targets or ())
    text_va = _integer(rizin_index["text_va"], "text_va")
    text_size = _integer(rizin_index["text_size"], "text_size")
    text_end = text_va + text_size
    mapped_text_end = text_va + mapped_text_size
    if len(text) != text_size:
        raise ValueError(f".text byte count is {len(text)}, expected {text_size}")

    rizin_rows = {
        _integer(row.get("va"), "rizin.va"): dict(row)
        for row in rizin_index.get("functions") or []
        if text_va <= _integer(row.get("va"), "rizin.va") < mapped_text_end
    }
    bn_rows = {
        va: row for va, row in binary_ninja_rows.items()
        if text_va <= va < mapped_text_end
    }
    provider_by_va = {
        _integer(row.get("va"), "provider.va"): row for row in providers
    }
    exact_extents = sorted(
        (va, va + _integer(row.get("size"), "provider.size"))
        for va, row in provider_by_va.items()
    )

    decoded_call_targets = {
        va for va in decoded_call_targets if text_va <= va < mapped_text_end
    }
    rtti_vtable_targets = {
        va for va in rtti_vtable_targets if text_va <= va < mapped_text_end
    }
    candidates = (
        set(rizin_rows) | set(bn_rows) | set(provider_by_va)
        | decoded_call_targets | rtti_vtable_targets
    )
    suppressed: list[dict[str, Any]] = []
    for start in sorted(candidates):
        owner = next(((lo, hi) for lo, hi in exact_extents if lo < start < hi), None)
        if owner is not None:
            candidates.remove(start)
            suppressed.append({
                "va": start,
                "reason": "inside-exact-provider",
                "inside_exact": owner[0],
                "end": owner[1],
            })
            continue
        # A literal INT3 byte is fill/trap content, not evidence of an entry.
        # Binary Ninja generated 49 one-byte functions in such runs in this
        # database. An exact provider would outrank this rule, but provisional
        # analyzer starts do not.
        if start not in provider_by_va and text[start - text_va] == 0xCC:
            candidates.remove(start)
            suppressed.append({"va": start, "reason": "literal-int3"})

    functions: list[dict[str, Any]] = []
    tier_counts: dict[str, int] = {}
    for start in sorted(candidates):
        rz = rizin_rows.get(start)
        bn = bn_rows.get(start)
        provider = provider_by_va.get(start)
        sources: list[str] = []
        if rz is not None:
            sources.append("Rizin")
        if bn is not None:
            sources.append("Binary Ninja")
        if provider is not None:
            sources.append("exact provider")
        if start in decoded_call_targets:
            sources.append("decoded direct call")
        if start in rtti_vtable_targets:
            sources.append("MSVC RTTI vtable slot")

        prior_cc = 0
        cursor = start - text_va - 1
        while cursor >= 0 and text[cursor] == 0xCC:
            prior_cc += 1
            cursor -= 1
        if provider is not None:
            tier = "exact-provider"
        elif rz is not None and bn is not None:
            tier = "cross-analyzer"
        elif start in decoded_call_targets:
            tier = "decoded-call-target"
        elif start in rtti_vtable_targets:
            tier = "rtti-vtable-slot"
        elif bn is not None and prior_cc > 0:
            tier = "binary-ninja-anchored"
        elif rz is not None and prior_cc > 0:
            tier = "rizin-anchored"
        elif bn is not None:
            tier = "binary-ninja-hypothesis"
        else:
            tier = "rizin-hypothesis"
        tier_counts[tier] = tier_counts.get(tier, 0) + 1
        admitted = tier not in {"binary-ninja-hypothesis", "rizin-hypothesis"}

        if provider is not None:
            size = _integer(provider.get("size"), "provider.size")
        elif bn is not None:
            highest = _integer(bn.get("highest_address"), "bn.highest_address")
            size = max(1, highest - start)
        elif rz is not None:
            size = max(1, _integer(rz.get("size", 1), "rizin.size"))
        else:
            # A decoded direct target proves an entry but not its extent.
            size = 0

        name = str(
            (rz or {}).get("name")
            or (bn or {}).get("name")
            or (provider or {}).get("name")
            or f"sub_{start:08x}"
        )
        row: dict[str, Any] = {
            "va": start,
            "rva_in_text": start - text_va,
            "size": size,
            "name": name,
            "basic_blocks": (bn or rz or {}).get("basic_blocks"),
            "basic_blocks_source": (
                "Binary Ninja" if bn is not None else "Rizin" if rz is not None else None
            ),
            "boundary_sources": sources,
            "boundary_tier": tier,
            "admitted": admitted,
            "post_int3_bytes": prior_cc,
            "decoded_call_target": start in decoded_call_targets,
            "rtti_vtable_slot": start in rtti_vtable_targets,
        }
        if rz is not None:
            row["rizin_declared_size"] = _integer(rz.get("size", 0), "rizin.size")
        if bn is not None:
            row["binary_ninja_total_bytes"] = _integer(bn.get("total_bytes", 0), "bn.total_bytes")
            row["binary_ninja_lowest_address"] = _integer(bn.get("lowest_address"), "bn.lowest_address")
            row["binary_ninja_highest_address"] = _integer(bn.get("highest_address"), "bn.highest_address")
        functions.append(row)

    summary = {
        "function_count": len(functions),
        "rizin_starts": len(rizin_rows),
        "binary_ninja_starts": len(bn_rows),
        "exact_provider_starts": len(provider_by_va),
        "rtti_vtable_targets": len(rtti_vtable_targets),
        "suppressed_inside_exact": sum(
            1 for row in suppressed if row["reason"] == "inside-exact-provider"
        ),
        "suppressed_literal_int3": sum(
            1 for row in suppressed if row["reason"] == "literal-int3"
        ),
        "admitted_starts": sum(1 for row in functions if row["admitted"]),
        "single_analyzer_hypotheses": sum(1 for row in functions if not row["admitted"]),
        "tier_counts": tier_counts,
        "suppressed_starts": suppressed,
    }
    return functions, summary


def main() -> int:
    workspace = Workspace.from_script(Path(__file__).resolve())
    current = workspace.root / "temp/sro-native-exact-all-cpp-v1150-current"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rizin-index", type=Path, default=current / "function-index.json")
    parser.add_argument("--binary-ninja-functions", type=Path,
                        default=current / "binary-ninja-function-spans.csv")
    parser.add_argument("--binary-ninja-coverage", type=Path,
                        default=current / "binary-ninja-text-coverage.json")
    parser.add_argument(
        "--rizin",
        type=Path,
        default=Path(shutil.which("rizin.exe") or r"C:\Program Files\Rizin\bin\rizin.exe"),
    )
    parser.add_argument("--project", type=Path, default=workspace.root / "sro.rzdb")
    parser.add_argument("--provider-manifest", type=Path,
                        default=workspace.replacements / "results/all-exact/provider-manifest.json")
    parser.add_argument("--out", type=Path, default=current / "function-index-reconciled.json")
    args = parser.parse_args()

    target = require_file(workspace.target, "retail executable")
    rizin_path = require_file(args.rizin_index, "Rizin function index")
    bn_path = require_file(args.binary_ninja_functions, "Binary Ninja function spans")
    coverage_path = require_file(args.binary_ninja_coverage, "Binary Ninja coverage")
    provider_path = require_file(args.provider_manifest, "exact provider manifest")
    image = target.read_bytes()
    sections = _sections(image)
    text_section = next(row for row in sections if row["name"] == ".text")
    text_va = int(text_section["va"])
    text_size = int(text_section["raw_size"])
    mapped_text_size = int(text_section["virtual_size"])
    raw_offset = int(text_section["raw_offset"])
    text = image[raw_offset:raw_offset + text_size]

    rizin = json.loads(rizin_path.read_text(encoding="utf-8"))
    coverage = json.loads(coverage_path.read_text(encoding="utf-8"))
    manifest = json.loads(provider_path.read_text(encoding="utf-8"))
    target_hash = sha256(target)
    if str(rizin.get("target_sha256", "")).lower() != target_hash:
        raise ValueError("Rizin index is bound to a different executable")
    if str(manifest.get("target_sha256", "")).lower() != target_hash:
        raise ValueError("provider manifest is bound to a different executable")
    if _integer(coverage.get("function_count"), "coverage.function_count") != sum(
        1 for _ in bn_path.open("r", encoding="utf-8")
    ) - 1:
        raise ValueError("Binary Ninja function export count does not match coverage metadata")
    if str(coverage.get("text_sha256", "")).lower() != hashlib.sha256(text).hexdigest():
        raise ValueError("Binary Ninja coverage is bound to different .text bytes")
    declared_spans_hash = str(coverage.get("function_spans_sha256") or "").lower()
    if declared_spans_hash and declared_spans_hash != sha256(bn_path):
        raise ValueError("Binary Ninja function spans do not match the coverage export")

    bn_rows = _load_binary_ninja_rows(bn_path)
    call_graph, direct_call_site_count = load_rizin_call_graph(args.rizin, args.project)
    decoded_call_targets = {
        ref["target"] for refs in call_graph.values() for ref in refs
    }
    raw_rizin_starts = {
        _integer(row.get("va"), "rizin.va") for row in rizin.get("functions") or []
    }
    provider_starts = {
        _integer(row.get("va"), "provider.va") for row in manifest.get("providers") or []
    }
    preliminary_candidates = (
        raw_rizin_starts | set(bn_rows) | provider_starts | decoded_call_targets
    )
    preliminary_candidates = {
        va for va in preliminary_candidates if text_va <= va < text_va + mapped_text_size
    }
    preliminary_strong: set[int] = set()
    for va in preliminary_candidates:
        if text[va - text_va] == 0xCC and va not in provider_starts:
            continue
        prior_int3 = va > text_va and text[va - text_va - 1] == 0xCC
        if (
            va in provider_starts
            or (va in raw_rizin_starts and va in bn_rows)
            or va in decoded_call_targets
            or prior_int3
        ):
            preliminary_strong.add(va)
    rtti_vtable_targets = _rtti_vtable_targets(
        image, sections, text_va, text_va + mapped_text_size,
        preliminary_candidates, preliminary_strong,
    )
    functions, summary = reconcile(
        rizin, bn_rows, list(manifest.get("providers") or []), text,
        mapped_text_size, decoded_call_targets, rtti_vtable_targets,
    )
    result = {
        "schema": SCHEMA,
        "target_sha256": target_hash,
        "text_va": text_va,
        "text_size": text_size,
        "text_virtual_size": mapped_text_size,
        "text_sha256": hashlib.sha256(text).hexdigest(),
        "sources": {
            "rizin_index": str(rizin_path.resolve()),
            "rizin_index_sha256": sha256(rizin_path),
            "binary_ninja_functions": str(bn_path.resolve()),
            "binary_ninja_functions_sha256": sha256(bn_path),
            "binary_ninja_coverage": str(coverage_path.resolve()),
            "binary_ninja_coverage_sha256": sha256(coverage_path),
            "provider_manifest": str(provider_path.resolve()),
            "provider_manifest_sha256": sha256(provider_path),
            "rizin_project": str(require_file(args.project, "Rizin project").resolve()),
            "rizin_project_sha256": sha256(args.project),
        },
        "direct_call_site_count": direct_call_site_count,
        "summary": summary,
        "functions": functions,
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(result, separators=(",", ":")) + "\n", encoding="utf-8")
    print(f"Wrote {args.out.resolve()}")
    print(json.dumps(summary["tier_counts"], sort_keys=True))
    print(
        f"{summary['function_count']} reconciled starts: "
        f"{summary['admitted_starts']} admitted, "
        f"{summary['single_analyzer_hypotheses']} retained as hypotheses; "
        f"suppressed {summary['suppressed_inside_exact']} inside exact providers and "
        f"{summary['suppressed_literal_int3']} on literal INT3 bytes"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
