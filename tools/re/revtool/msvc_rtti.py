"""Small, deterministic x86 MSVC RTTI reader used by the RE evidence graph.

The important invariant is that RTTI and analyzer starts corroborate each
other; neither is allowed to bootstrap itself through an address-specific
exception.  A vtable is admitted only when all of these are true:

* the word immediately before it points at a structurally valid x86 complete
  object locator;
* the locator names a class and points at a bounded class hierarchy whose
  first base descriptor names that same class;
* slot zero is an analyzer/provider candidate in mapped ``.text``; and
* the table is a contiguous prefix of candidate entries, stopping at the next
  locator, non-candidate word, INT3 byte, or obvious UTF-16 text.

This deliberately does *not* require slot zero to be independently strong.
Doing so is circular: a real vtable is itself the direct structural evidence
that turns a single-analyzer function hypothesis into a confirmed entry.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Iterable


@dataclass(frozen=True)
class VtableRecord:
    class_name: str
    table_va: int
    locator_va: int
    complete_object_offset: int
    type_descriptor_va: int
    hierarchy_va: int
    targets: tuple[int, ...]


def _normalise_sections(
    sections: Iterable[dict[str, Any] | tuple[str, int, int, int]],
) -> list[tuple[str, int, int, int]]:
    result: list[tuple[str, int, int, int]] = []
    for row in sections:
        if isinstance(row, dict):
            result.append((
                str(row["name"]), int(row["va"]),
                int(row["raw_offset"]), int(row["raw_size"]),
            ))
        else:
            name, va, offset, size = row
            result.append((str(name), int(va), int(offset), int(size)))
    return result


class _Reader:
    def __init__(self, image: bytes, sections) -> None:
        self.image = image
        self.sections = _normalise_sections(sections)
        self.readable = [
            (va, offset, size)
            for name, va, offset, size in self.sections
            if name in (".rdata", ".data")
        ]
        self.rdata = next(
            (row for row in self.sections if row[0] == ".rdata"), None
        )
        if self.rdata is None:
            raise ValueError("PE has no .rdata section")

    def dword(self, va: int) -> int | None:
        for base, offset, size in self.readable:
            if base <= va <= base + size - 4:
                at = offset + va - base
                return int.from_bytes(self.image[at:at + 4], "little")
        return None

    def cstring(self, va: int) -> str:
        for base, offset, size in self.readable:
            if base <= va < base + size:
                at = offset + va - base
                end = self.image.find(b"\0", at, offset + size)
                if end >= 0:
                    return self.image[at:end].decode("ascii", "replace")
        return ""

    def class_name(self, type_descriptor: int | None) -> str | None:
        if not type_descriptor:
            return None
        decorated = self.cstring(type_descriptor + 8)
        if decorated.startswith((".?AV", ".?AU")):
            return decorated[4:].rstrip("@")
        return None

    def text_byte(self, va: int, text_lo: int) -> int | None:
        text = next((row for row in self.sections if row[0] == ".text"), None)
        if text is None:
            return None
        _, base, offset, size = text
        if not (base <= va < base + size) or base != text_lo:
            return None
        return self.image[offset + va - base]


def _valid_locator(reader: _Reader, locator_va: int) -> tuple[str, int, int, int, set[str]] | None:
    """Validate one x86 ``RTTICompleteObjectLocator`` and its hierarchy."""
    if reader.dword(locator_va) != 0:
        return None
    complete_object_offset = reader.dword(locator_va + 4)
    type_descriptor = reader.dword(locator_va + 0x0C)
    hierarchy = reader.dword(locator_va + 0x10)
    owner = reader.class_name(type_descriptor)
    if complete_object_offset is None or not owner or hierarchy is None:
        return None
    if reader.dword(hierarchy) != 0:
        return None
    count = reader.dword(hierarchy + 8)
    array = reader.dword(hierarchy + 0x0C)
    if count is None or array is None or not (1 <= count <= 256):
        return None
    bases: list[str] = []
    for index in range(count):
        descriptor = reader.dword(array + index * 4)
        base = reader.class_name(reader.dword(descriptor) if descriptor else None)
        if not base:
            return None
        bases.append(base)
    # MSVC's complete-object base array starts with the complete type.  This
    # check rejects pointer-shaped data that happens to satisfy the shallower
    # locator fields.
    if not bases or bases[0] != owner:
        return None
    return owner, complete_object_offset, type_descriptor, hierarchy, set(bases)


def rtti_hierarchies(image: bytes, sections) -> dict[str, set[str]]:
    """Return validated class -> complete base-class set from every locator."""
    reader = _Reader(image, sections)
    _, rd_va, rd_offset, rd_size = reader.rdata
    result: dict[str, set[str]] = {}
    # Locator structures themselves are aligned and live in .rdata.  Scanning
    # them directly also retains types whose emitted vtable cannot yet be
    # bounded by any analyzer candidate.
    for offset in range(0, rd_size - 0x14 + 1, 4):
        locator_va = rd_va + offset
        valid = _valid_locator(reader, locator_va)
        if valid is None:
            continue
        owner, _, _, _, bases = valid
        result.setdefault(owner, set()).update(bases)
    return result


def rtti_direct_bases(image: bytes, sections) -> dict[str, set[str]]:
    """Return the direct bases encoded by each validated hierarchy.

    MSVC stores base descriptors in preorder.  ``numContainedBases`` is the
    size of that descriptor's subtree excluding itself, so walking from entry
    one and skipping each subtree yields exactly the complete type's direct
    bases without relying on a decompiler's rendered RTTI labels.
    """
    reader = _Reader(image, sections)
    _, rd_va, _, rd_size = reader.rdata
    result: dict[str, set[str]] = {}
    for offset in range(0, rd_size - 0x14 + 1, 4):
        locator_va = rd_va + offset
        valid = _valid_locator(reader, locator_va)
        if valid is None:
            continue
        owner, _, _, hierarchy, _ = valid
        count = reader.dword(hierarchy + 8)
        array = reader.dword(hierarchy + 0x0C)
        if count is None or array is None:
            continue
        entries: list[tuple[str, int]] = []
        for index in range(count):
            descriptor = reader.dword(array + index * 4)
            name = reader.class_name(reader.dword(descriptor) if descriptor else None)
            contained = reader.dword(descriptor + 4) if descriptor else None
            if not name or contained is None or contained >= count:
                entries = []
                break
            entries.append((name, contained))
        if not entries or entries[0][0] != owner:
            continue
        direct: set[str] = set()
        cursor = 1
        while cursor < len(entries):
            name, contained = entries[cursor]
            direct.add(name)
            cursor += contained + 1
        result.setdefault(owner, set()).update(direct)
    return result


def scan_vtables(
    image: bytes,
    sections,
    text_lo: int,
    text_hi: int,
    candidate_starts: set[int],
) -> list[VtableRecord]:
    """Return structurally validated vtables and their contiguous targets."""
    reader = _Reader(image, sections)
    _, rd_va, rd_offset, rd_size = reader.rdata
    blob = image[rd_offset:rd_offset + rd_size]

    def starts_utf16(offset: int) -> bool:
        sample = blob[offset:offset + 12]
        return (
            len(sample) >= 8
            and all(sample[index] == 0 for index in range(1, len(sample), 2))
            and all(0x20 <= sample[index] < 0x7F for index in range(0, len(sample), 2))
        )

    starts: list[tuple[int, tuple[str, int, int, int, set[str]]]] = []
    for offset in range(0, rd_size - 8, 4):
        locator_va = int.from_bytes(blob[offset:offset + 4], "little")
        first = int.from_bytes(blob[offset + 4:offset + 8], "little")
        if first not in candidate_starts or not (text_lo <= first < text_hi):
            continue
        valid = _valid_locator(reader, locator_va)
        if valid is not None:
            starts.append((offset + 4, valid))

    records: list[VtableRecord] = []
    for ordinal, (start, valid) in enumerate(starts):
        owner, object_offset, type_descriptor, hierarchy, _ = valid
        stop = starts[ordinal + 1][0] if ordinal + 1 < len(starts) else rd_size
        cursor = start
        targets: list[int] = []
        while cursor + 4 <= stop and not starts_utf16(cursor):
            target = int.from_bytes(blob[cursor:cursor + 4], "little")
            if target not in candidate_starts or not (text_lo <= target < text_hi):
                break
            if reader.text_byte(target, text_lo) == 0xCC:
                break
            targets.append(target)
            cursor += 4
        if not targets:
            continue
        locator_va = int.from_bytes(blob[start - 4:start], "little")
        records.append(VtableRecord(
            class_name=owner,
            table_va=rd_va + start,
            locator_va=locator_va,
            complete_object_offset=object_offset,
            type_descriptor_va=type_descriptor,
            hierarchy_va=hierarchy,
            targets=tuple(targets),
        ))
    return records


def targets_by_class(records: Iterable[VtableRecord]) -> dict[str, set[int]]:
    result: dict[str, set[int]] = {}
    for record in records:
        result.setdefault(record.class_name, set()).update(record.targets)
    return result
