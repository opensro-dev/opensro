#!/usr/bin/env python3
"""Export Binary Ninja basic-block coverage from a live GUI BinaryView.

This is intentionally a GUI-side helper so a Personal license is sufficient.
Run it in Binary Ninja's Python console with the SRO database open, with
your own paths (the output belongs in the research tree's
targets/<sha256>/derived/ folder, beside the other analyzer exports)::

    exec(compile(open(r"<checkout>/scripts/re/revtool/export_binary_ninja_text_coverage.py").read(), "export_binary_ninja_text_coverage.py", "exec"))
    export_binary_ninja_text_coverage(
        bv,
        r"<research>/targets/<sha256>/derived/binary-ninja-text-coverage.json",
    )

The export contains the union of decoded basic blocks, function-span
hypotheses, and function-attributed call-like sites.  The latter includes tail
jumps that Binary Ninja models as calls and unresolved indirect sites with an
empty target.  Keeping unresolved sites is important: otherwise "no exported
callee" is indistinguishable from "the call graph was never available".

None of these records turns a Binary Ninja hypothesis into authoritative
function ownership or extent.
"""

from __future__ import annotations

import csv
import hashlib
import io
import json
from pathlib import Path
from typing import Any


SCHEMA = "sro-binary-ninja-text-coverage-v3"
EXPORTER_VERSION = 3


def _u16(data: bytes, offset: int) -> int:
    return int.from_bytes(data[offset:offset + 2], "little")


def _u32(data: bytes, offset: int) -> int:
    return int.from_bytes(data[offset:offset + 4], "little")


def _raw_text_section(raw_view: Any) -> tuple[int, int, int, int]:
    raw_start = int(raw_view.start)
    raw_size = int(raw_view.end - raw_start)
    header = raw_view.read(raw_start, min(raw_size, 0x1000))
    pe = _u32(header, 0x3C)
    section_count = _u16(header, pe + 6)
    optional_size = _u16(header, pe + 20)
    optional = pe + 24
    image_base = _u32(header, optional + 28)
    section_table = optional + optional_size
    for index in range(section_count):
        entry = section_table + index * 40
        name = header[entry:entry + 8].rstrip(b"\0")
        if name != b".text":
            continue
        virtual_size = _u32(header, entry + 8)
        virtual_address = _u32(header, entry + 12)
        raw_size = _u32(header, entry + 16)
        raw_offset = _u32(header, entry + 20)
        return image_base + virtual_address, virtual_size, raw_offset, raw_size
    raise ValueError("PE has no .text section")


def _merge(intervals: list[tuple[int, int]]) -> list[list[int]]:
    merged: list[list[int]] = []
    for start, stop in sorted(intervals):
        if stop <= start:
            continue
        if not merged or start > merged[-1][1]:
            merged.append([start, stop])
        elif stop > merged[-1][1]:
            merged[-1][1] = stop
    return merged


def export_binary_ninja_text_coverage(binary_view: Any, destination: str | Path) -> dict[str, Any]:
    """Write a hash-bound basic-block coverage artifact and return its metadata."""
    raw_view = binary_view.file.raw
    text_va, virtual_size, raw_offset, raw_size = _raw_text_section(raw_view)
    text_end = text_va + raw_size
    intervals: list[tuple[int, int]] = []
    function_rows: list[dict[str, Any]] = []
    direct_call_rows: list[dict[str, Any]] = []
    function_count = 0
    basic_block_count = 0
    call_site_count = 0
    unresolved_call_site_count = 0
    for function in binary_view.functions:
        if text_va <= function.start < text_end:
            function_count += 1
        blocks = list(function.basic_blocks)
        if text_va <= function.start < text_end:
            function_rows.append(
                {
                    "address": function.start,
                    "total_bytes": function.total_bytes,
                    "lowest_address": min((block.start for block in blocks), default=function.start),
                    "highest_address": max((block.end for block in blocks), default=function.start),
                    "block_count": len(blocks),
                    "name": function.name,
                }
            )
            for site in function.call_sites:
                call_site_count += 1
                site_address = int(site.address)
                targets = sorted({
                    int(target)
                    for target in binary_view.get_code_refs_from(site_address)
                    if int(target) != 0
                })
                disassembly = str(binary_view.get_disassembly(site_address) or "").strip()
                kind = disassembly.split(None, 1)[0].lower() if disassembly else "unknown"
                if not targets:
                    unresolved_call_site_count += 1
                    direct_call_rows.append({
                        "caller_address": int(function.start),
                        "site_address": site_address,
                        "target_address": "",
                        "kind": kind,
                    })
                else:
                    for target in targets:
                        direct_call_rows.append({
                            "caller_address": int(function.start),
                            "site_address": site_address,
                            "target_address": target,
                            "kind": kind,
                        })
        for block in blocks:
            basic_block_count += 1
            start = max(text_va, block.start)
            stop = min(text_end, block.end)
            if start < stop:
                intervals.append((start, stop))
    merged = _merge(intervals)
    text = raw_view.read(int(raw_view.start) + raw_offset, raw_size)
    if len(text) != raw_size:
        raise ValueError(f"short raw .text read: {len(text)} of {raw_size} bytes")
    raw_start = int(raw_view.start)
    binary_size = int(raw_view.end - raw_start)
    raw_binary = raw_view.read(raw_start, binary_size)
    if len(raw_binary) != binary_size:
        raise ValueError(
            f"short raw binary read: {len(raw_binary)} of {binary_size} bytes"
        )
    try:
        import binaryninja  # available in the GUI console, absent in unit tests
        core_version = str(binaryninja.core_version())
    except (ImportError, AttributeError):
        core_version = "unavailable"
    function_rows.sort(key=lambda row: (row["address"], row["name"]))
    direct_call_rows.sort(key=lambda row: (
        int(row["caller_address"]), int(row["site_address"]),
        int(row["target_address"]) if row["target_address"] != "" else -1,
        str(row["kind"]),
    ))
    payload: dict[str, Any] = {
        "schema": SCHEMA,
        "exporter_version": EXPORTER_VERSION,
        "binary_ninja_core_version": core_version,
        # Paths make otherwise identical exports differ between machines.  The
        # basename identifies the opened analysis database without binding the
        # evidence packet to one developer's checkout.
        "binary_view": Path(str(binary_view.file.filename)).name,
        "binary_size": binary_size,
        "binary_sha256": hashlib.sha256(raw_binary).hexdigest(),
        "architecture": str(binary_view.arch),
        "platform": str(binary_view.platform),
        "text_va": text_va,
        "text_virtual_size": virtual_size,
        "text_raw_offset": raw_offset,
        "text_size": raw_size,
        "text_sha256": hashlib.sha256(text).hexdigest(),
        "function_count": function_count,
        "basic_block_count": basic_block_count,
        "call_site_count": call_site_count,
        "direct_call_edge_count": sum(
            1 for row in direct_call_rows if row["target_address"] != ""
        ),
        "unresolved_call_site_count": unresolved_call_site_count,
        "merged_interval_count": len(merged),
        "covered_bytes": sum(stop - start for start, stop in merged),
        "intervals": merged,
    }
    output = Path(destination)
    output.parent.mkdir(parents=True, exist_ok=True)
    spans_output = output.with_name("binary-ninja-function-spans.csv")
    buffer = io.StringIO(newline="")
    writer = csv.DictWriter(
        buffer,
        fieldnames=[
            "address", "total_bytes", "lowest_address", "highest_address",
            "block_count", "name",
        ],
        lineterminator="\n",
    )
    writer.writeheader()
    writer.writerows(function_rows)
    spans_bytes = buffer.getvalue().encode("utf-8")
    spans_output.write_bytes(spans_bytes)
    payload["function_spans_csv"] = spans_output.name
    payload["function_spans_sha256"] = hashlib.sha256(spans_bytes).hexdigest()

    calls_output = output.with_name("binary-ninja-direct-calls.csv")
    call_buffer = io.StringIO(newline="")
    call_writer = csv.DictWriter(
        call_buffer,
        fieldnames=["caller_address", "site_address", "target_address", "kind"],
        lineterminator="\n",
    )
    call_writer.writeheader()
    call_writer.writerows(direct_call_rows)
    call_bytes = call_buffer.getvalue().encode("utf-8")
    calls_output.write_bytes(call_bytes)
    payload["direct_calls_csv"] = calls_output.name
    payload["direct_calls_sha256"] = hashlib.sha256(call_bytes).hexdigest()
    output.write_text(json.dumps(payload, separators=(",", ":")) + "\n", encoding="utf-8")
    print(
        f"Wrote {output}: {function_count} function hypotheses, "
        f"{basic_block_count} blocks, {payload['covered_bytes']}/{raw_size} .text bytes covered\n"
        f"Wrote {spans_output}: {len(function_rows)} function spans\n"
        f"Wrote {calls_output}: {payload['direct_call_edge_count']} direct edges and "
        f"{unresolved_call_site_count} unresolved call-like sites"
    )
    return payload
