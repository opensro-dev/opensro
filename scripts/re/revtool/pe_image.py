#!/usr/bin/env python3
"""Small, strict PE loader-view adapter shared by RE evidence tools.

This module deliberately models two different questions with two different
methods:

``read_file_backed``
    Return bytes that physically exist in the PE file.  It fails when any part
    of the interval is virtual-only storage.

``read_loader``
    Return the bytes a Windows loader initially exposes.  A section tail where
    ``VirtualSize > SizeOfRawData`` is zero-filled; it must never fall through
    into the next section's raw file bytes.

Conflating those questions caused a real reconstruction error at 0x00CE93A4:
an old mapper read resource bytes for a BSS-backed address.  Keep the API split
so a caller has to state which fact it needs.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

@dataclass(frozen=True)
class Section:
    name: str
    rva: int
    virtual_size: int
    raw_size: int
    raw_offset: int
    image_base: int

    @property
    def va(self) -> int:
        return self.image_base + self.rva

    @property
    def mapped_size(self) -> int:
        # The PE/COFF specification permits VirtualSize to be zero in object-
        # like images and SizeOfRawData to exceed VirtualSize due to alignment.
        return max(self.virtual_size, self.raw_size)

    def contains(self, va: int, size: int = 1) -> bool:
        return size >= 0 and self.va <= va and va + size <= self.va + self.mapped_size


class PEImage:
    def __init__(self, path: Path | str):
        self.path = Path(path)
        self.data = self.path.read_bytes()
        data = self.data
        if len(data) < 0x40 or data[:2] != b"MZ":
            raise ValueError(f"not a PE (missing DOS header): {self.path}")
        pe = int.from_bytes(data[0x3C:0x40], "little")
        if pe + 24 > len(data) or data[pe:pe + 4] != b"PE\0\0":
            raise ValueError(f"not a PE (missing PE signature): {self.path}")
        section_count = int.from_bytes(data[pe + 6:pe + 8], "little")
        optional_size = int.from_bytes(data[pe + 20:pe + 22], "little")
        optional = pe + 24
        if optional + optional_size > len(data):
            raise ValueError(f"truncated optional header: {self.path}")
        magic = int.from_bytes(data[optional:optional + 2], "little")
        if magic == 0x10B:
            self.image_base = int.from_bytes(data[optional + 28:optional + 32], "little")
        elif magic == 0x20B:
            self.image_base = int.from_bytes(data[optional + 24:optional + 32], "little")
        else:
            raise ValueError(f"unsupported PE optional-header magic 0x{magic:X}")
        self.image_size = int.from_bytes(data[optional + 56:optional + 60], "little")
        table = optional + optional_size
        if table + section_count * 40 > len(data):
            raise ValueError(f"truncated section table: {self.path}")
        sections: list[Section] = []
        for index in range(section_count):
            entry = table + index * 40
            sections.append(Section(
                name=data[entry:entry + 8].rstrip(b"\0").decode("ascii", "replace"),
                virtual_size=int.from_bytes(data[entry + 8:entry + 12], "little"),
                rva=int.from_bytes(data[entry + 12:entry + 16], "little"),
                raw_size=int.from_bytes(data[entry + 16:entry + 20], "little"),
                raw_offset=int.from_bytes(data[entry + 20:entry + 24], "little"),
                image_base=self.image_base,
            ))
        self.sections = tuple(sections)

    def section_for_va(self, va: int, size: int = 1) -> Section | None:
        return next((section for section in self.sections if section.contains(va, size)), None)

    def section_for_file_offset(self, offset: int) -> Section | None:
        return next((
            section for section in self.sections
            if section.raw_offset <= offset < section.raw_offset + section.raw_size
        ), None)

    def read_file_backed(self, va: int, size: int) -> bytes:
        section = self.section_for_va(va, size)
        if section is None:
            raise ValueError(f"VA interval 0x{va:08X}+{size} is not mapped in {self.path}")
        delta = va - section.va
        if delta + size > section.raw_size:
            raise ValueError(
                f"VA interval 0x{va:08X}+{size} reaches virtual-only bytes "
                f"in {section.name}"
            )
        start = section.raw_offset + delta
        end = start + size
        if end > len(self.data):
            raise ValueError(f"section raw range is truncated in {self.path}")
        return self.data[start:end]

    def read_loader(self, va: int, size: int) -> bytes:
        section = self.section_for_va(va, size)
        if section is None:
            raise ValueError(f"VA interval 0x{va:08X}+{size} is not mapped in {self.path}")
        delta = va - section.va
        raw_count = max(0, min(size, section.raw_size - delta))
        raw = b""
        if raw_count:
            start = section.raw_offset + delta
            end = start + raw_count
            if end > len(self.data):
                raise ValueError(f"section raw range is truncated in {self.path}")
            raw = self.data[start:end]
        return raw + b"\0" * (size - raw_count)

    def classify(self, va: int, size: int = 1) -> str:
        section = self.section_for_va(va, size)
        if section is None:
            return "unmapped"
        delta = va - section.va
        return "file-backed" if delta + size <= section.raw_size else "virtual-only"

    def rtti_sections(self) -> list[dict[str, int | str]]:
        return [{
            "name": section.name,
            "va": section.va,
            "virtual_size": section.virtual_size,
            "raw_size": section.raw_size,
            "raw_offset": section.raw_offset,
        } for section in self.sections]

    def pointer_refs(self, target: int) -> list[dict[str, int | str | bool]]:
        """Return aligned, file-backed dword references to ``target``."""
        needle = target.to_bytes(4, "little")
        refs: list[dict[str, int | str | bool]] = []
        start = 0
        while True:
            offset = self.data.find(needle, start)
            if offset < 0:
                break
            section = self.section_for_file_offset(offset)
            if section is not None and (offset - section.raw_offset) % 4 == 0:
                refs.append({
                    "va": section.va + offset - section.raw_offset,
                    "section": section.name,
                    "aligned": True,
                })
            start = offset + 1
        return refs

    def direct_call_refs(self, target: int) -> list[int]:
        """Decode `.text` and return direct near-call sites for ``target``.

        The previous implementation searched for every raw 0xE8 byte, which
        also finds immediates and embedded data.  iced-x86 supplies instruction
        boundaries and branch targets; the result remains an observation until
        a function boundary/proof admits the containing instruction.
        """
        # Keep the foundational PE mapper usable by scripts that only need
        # section facts; decoding is an opt-in dependency of this operation.
        from iced_x86 import Decoder, FlowControl

        text = next((section for section in self.sections if section.name == ".text"), None)
        if text is None:
            return []
        payload = self.data[text.raw_offset:text.raw_offset + text.raw_size]
        return [
            instruction.ip
            for instruction in Decoder(32, payload, ip=text.va)
            if instruction.flow_control == FlowControl.CALL
            and instruction.near_branch_target == target
        ]
