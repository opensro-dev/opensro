"""
===========================================================================

convert_images.py - DDJ and texture conversion into the image intermediate

Converts the extracted client textures (DDJ, DDS, TGA, BMP) into PNG
under .generated/intermediate/images and keeps the conversion manifest.
Roots come from sro_paths.py.

===========================================================================
"""
from __future__ import annotations

import csv
import os
import shutil
import struct
import subprocess
import sys
import tempfile
import time
from concurrent.futures import ProcessPoolExecutor
from dataclasses import dataclass
from io import BytesIO
from pathlib import Path

from PIL import Image

from rebuild_lock import generated_assets_lock
from sro_paths import EXTRACTED_ROOT, GAME_ROOT, GENERATED_ROOT


SOURCE_ROOTS = [
    "Data_extracted",
    "Map_extracted",
    "Media_extracted",
    "Particles_extracted",
]
OUTPUT_ROOT = GENERATED_ROOT / "intermediate" / "images"
MANIFEST_PATH = GENERATED_ROOT / "intermediate" / "image-manifest.csv"
SUPPORTED_EXTENSIONS = {".ddj", ".tga", ".dat"}
# Assets handed to a worker per round trip: small enough to balance, large
# enough that pickling stays negligible next to a decode.
CONVERT_CHUNK = 32


# ================
# ImageAsset
# ================
@dataclass(frozen=True)
class ImageAsset:
    source: Path
    relative: Path
    output: Path
    kind: str


# ================
# main
# ================
def main() -> int:
    start = time.monotonic()
    OUTPUT_ROOT.mkdir(parents=True, exist_ok=True)
    MANIFEST_PATH.parent.mkdir(parents=True, exist_ok=True)

    # Optional CLI filters: only convert assets whose relative path contains any
    # of the given substrings (case-insensitive). Useful for targeted re-converts.
    filters = [arg.lower() for arg in sys.argv[1:]]
    assets = discover_assets(filters)
    if filters:
        assets = [a for a in assets if any(f in a.relative.as_posix().lower() for f in filters)]
    print(f"Discovered {len(assets)} image assets.")
    print(f"Output: {OUTPUT_ROOT}")

    ok = 0
    skipped = 0
    failed: list[tuple[ImageAsset, str]] = []
    # Incremental by default: a target PNG that is newer than its source is not re-decoded.
    # SRO_FORCE_IMAGE_CONVERT=1 restores the old always-convert behavior.
    force_convert = os.environ.get("SRO_FORCE_IMAGE_CONVERT") == "1"
    jobs = build_jobs()
    print(f"Conversion processes: {jobs} (SRO_BUILD_JOBS)")

    with MANIFEST_PATH.open("w", newline="", encoding="utf-8") as manifest_file:
        writer = csv.writer(manifest_file)
        writer.writerow(["source", "output", "kind", "bytes"])

        # Results come back in input order, so the manifest is identical
        # whatever the process count.
        with ProcessPoolExecutor(max_workers=jobs) as pool:
            results = pool.map(convert_one, assets, [force_convert] * len(assets), chunksize=CONVERT_CHUNK)
            for index, (asset, (status, detail)) in enumerate(zip(assets, results), start=1):
                if status == "failed":
                    failed.append((asset, detail))
                else:
                    if status == "skipped":
                        skipped += 1
                    else:
                        ok += 1
                    writer.writerow(
                        [
                            asset.relative.as_posix(),
                            asset.output.relative_to(OUTPUT_ROOT).as_posix(),
                            asset.kind,
                            detail,
                        ]
                    )

                if index == 1 or index % 500 == 0 or index == len(assets):
                    elapsed = time.monotonic() - start
                    print(
                        f"[{index}/{len(assets)}] converted={ok} skipped={skipped} failed={len(failed)} elapsed={elapsed:.1f}s"
                    )

    if failed:
        failure_path = GENERATED_ROOT / "intermediate" / "image-conversion-failures.txt"
        with failure_path.open("w", encoding="utf-8") as failure_file:
            for asset, error in failed:
                failure_file.write(f"{asset.relative.as_posix()}: {error}\n")
        print(f"Failures written to {failure_path}", file=sys.stderr)
        return 1

    elapsed = time.monotonic() - start
    print(f"Converted {ok} image assets ({skipped} already fresh) in {elapsed:.1f}s.")
    print(f"Manifest: {MANIFEST_PATH}")
    return 0


# ================
# build_jobs
#
# SRO_BUILD_JOBS (scripts/build/shared/buildParallelism.mjs owns the rule):
# a positive integer, else every core but one.
# ================
def build_jobs() -> int:
    raw = os.environ.get("SRO_BUILD_JOBS", "")
    if not raw:
        return max(1, (os.cpu_count() or 2) - 1)
    if not raw.isdigit() or int(raw) < 1:
        raise SystemExit(f"SRO_BUILD_JOBS must be a positive integer; got {raw!r}")
    return int(raw)


# ================
# convert_one
#
# One asset in a worker process: ("skipped" | "converted", source bytes) or
# ("failed", the error). Every conversion failure is reported, none raised.
# ================
def convert_one(asset: ImageAsset, force_convert: bool) -> tuple[str, int | str]:
    try:
        if not force_convert and output_is_fresh(asset):
            return "skipped", asset.source.stat().st_size
        convert_asset(asset)
        return "converted", asset.source.stat().st_size
    except Exception as exc:  # noqa: BLE001 - report all conversion failures.
        return "failed", str(exc)


# ================
# output_is_fresh
# ================
def output_is_fresh(asset: ImageAsset) -> bool:
    try:
        output_stat = asset.output.stat()
        if output_stat.st_size == 0:
            return False
        return output_stat.st_mtime >= asset.source.stat().st_mtime
    except OSError:
        return False


# ================
# discover_assets
# ================
def discover_assets(filters: list[str] | None = None) -> list[ImageAsset]:
    candidates: list[Path] = []
    scan_roots = pruned_scan_roots(filters) if filters else None
    if scan_roots is None:
        scan_roots = [EXTRACTED_ROOT / root_name for root_name in SOURCE_ROOTS]

    for root in scan_roots:
        if not root.exists():
            continue
        for path in root.rglob("*"):
            if path.is_file() and path.suffix.lower() in SUPPORTED_EXTENSIONS and is_image_asset(path):
                candidates.append(path)

    candidates.sort(key=lambda value: value.relative_to(EXTRACTED_ROOT).as_posix().lower())
    output_paths = build_output_paths(candidates)

    assets: list[ImageAsset] = []
    for source in candidates:
        relative = source.relative_to(EXTRACTED_ROOT)
        assets.append(
            ImageAsset(
                source=source,
                relative=relative,
                output=output_paths[source],
                kind=detect_kind(source),
            )
        )
    return assets


# ================
# pruned_scan_roots
# ================
def pruned_scan_roots(filters: list[str]) -> list[Path] | None:
    """Map exact files and subtree filters onto the narrowest safe scan directories.

    Exact files deliberately map to their parent: output-name collision groups are
    per-directory, so the sibling scan preserves the .ddj.png naming contract when a
    same-stem TGA/DAT exists. Unknown substring filters retain the full-scan fallback.
    """
    subtrees: list[Path] = []
    for raw_filter in filters:
        fragment = raw_filter.replace("\\", "/").strip("/")
        if not fragment or fragment.startswith(".") or ".." in fragment.split("/"):
            return None
        matches = []
        for root_name in SOURCE_ROOTS:
            candidate = EXTRACTED_ROOT / root_name / Path(fragment)
            if candidate.is_dir():
                matches.append(candidate)
            elif candidate.is_file() and candidate.suffix.lower() in SUPPORTED_EXTENSIONS:
                matches.append(candidate.parent)
        if not matches:
            return None
        subtrees.extend(matches)

    unique: list[Path] = []
    for subtree in sorted(subtrees, key=lambda value: len(value.as_posix())):
        if not any(other == subtree or other in subtree.parents for other in unique):
            unique.append(subtree)
    return unique


# ================
# build_output_paths
# ================
def build_output_paths(paths: list[Path]) -> dict[Path, Path]:
    desired: dict[Path, list[Path]] = {}
    for source in paths:
        relative = source.relative_to(EXTRACTED_ROOT)
        target = OUTPUT_ROOT / relative.parent / f"{source.stem}.png"
        desired.setdefault(target, []).append(source)

    output_paths: dict[Path, Path] = {}
    for target, sources in desired.items():
        if len(sources) == 1:
            output_paths[sources[0]] = target
            continue

        for source in sources:
            output_paths[source] = target.with_name(f"{source.stem}.{source.suffix.lower().lstrip('.')}.png")

    return output_paths


# ================
# is_image_asset
# ================
def is_image_asset(path: Path) -> bool:
    suffix = path.suffix.lower()
    if suffix == ".tga":
        return True

    # Read only the header: read_bytes() pulled ENTIRE files into memory during
    # discovery, which dominated every invocation (gigabytes across the roots).
    try:
        with path.open("rb") as handle:
            header = handle.read(20)
    except OSError:
        return False

    if suffix == ".ddj":
        return header.startswith(b"JMXVDDJ ")
    if suffix == ".dat":
        return header.startswith(b"BM")
    return False


# ================
# detect_kind
# ================
def detect_kind(path: Path) -> str:
    suffix = path.suffix.lower()
    if suffix == ".ddj":
        return "ddj-dds"
    if suffix == ".tga":
        return "tga"
    if suffix == ".dat":
        return "bmp-dat"
    return "unknown"


# ================
# convert_asset
# ================
def convert_asset(asset: ImageAsset) -> None:
    asset.output.parent.mkdir(parents=True, exist_ok=True)
    suffix = asset.source.suffix.lower()

    if suffix == ".ddj":
        payload = extract_ddj_payload(asset.source)
        save_image_payload(payload, asset.output, source_for_fallback=asset.source)
    elif suffix in {".tga", ".dat"}:
        save_image_file(asset.source, asset.output)
    else:
        raise ValueError(f"unsupported image extension: {suffix}")


# ================
# extract_ddj_payload
# ================
def extract_ddj_payload(path: Path) -> bytes:
    data = path.read_bytes()
    if len(data) < 20 or not data.startswith(b"JMXVDDJ "):
        raise ValueError("not a JMXVDDJ texture")

    size, _texture_type = struct.unpack_from("<II", data, 12)
    payload = data[20 : 20 + size]
    if not payload.startswith(b"DDS "):
        raise ValueError("DDJ payload is not DDS")
    return payload


# ================
# decode_native_rgb16
# ================
def decode_native_rgb16(payload: bytes) -> Image.Image | None:
    """Retail A1R5G5B5/R5G6B5 expansion replicates high bits into low bits.

    Pillow truncates scaled channels. Normalized rounding also differs at
    5-bit values 7, 24 and 28. Retail screenshot samples discriminate both.
    Keep compressed/32-bit DDS formats on their existing decoder.
    """
    if len(payload) < 128 or payload[:4] != b"DDS ":
        return None
    size, flags, height, width, pitch = struct.unpack_from("<5I", payload, 4)
    pixel_format = struct.unpack_from("<8I", payload, 76)
    alpha = pixel_format == (32, 65, 0, 16, 0x7C00, 0x3E0, 0x1F, 0x8000)
    rgb565 = pixel_format == (32, 64, 0, 16, 0xF800, 0x7E0, 0x1F, 0)
    if size != 124 or not (alpha or rgb565):
        return None
    stride = pitch if flags & 8 else width * 2
    if not width or not height or stride < width * 2 or len(payload) < 128 + stride * height:
        raise ValueError("truncated or invalid native RGB16 DDS")
    pixels = bytearray(width * height * 4)
    for y in range(height):
        for x in range(width):
            value = struct.unpack_from("<H", payload, 128 + y * stride + x * 2)[0]
            r = value >> (10 if alpha else 11) & 31
            g = value >> 5 & (31 if alpha else 63)
            b = value & 31
            at = (y * width + x) * 4
            pixels[at:at + 4] = bytes((r << 3 | r >> 2, (g << 3 | g >> 2) if alpha else (g << 2 | g >> 4), b << 3 | b >> 2, 255 if not alpha or value & 0x8000 else 0))
    return Image.frombytes("RGBA", (width, height), bytes(pixels))


# ================
# save_image_payload
# ================
def save_image_payload(payload: bytes, output: Path, source_for_fallback: Path) -> None:
    native = decode_native_rgb16(payload)
    if native is not None:
        native.save(output, "PNG")
        return
    try:
        with Image.open(BytesIO(payload)) as image:
            image.save(output, "PNG")
        return
    except Exception:
        pass

    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg:
        raise RuntimeError("Pillow failed and ffmpeg is unavailable")

    with tempfile.TemporaryDirectory(prefix="sro-ddj-") as temp_dir:
        dds_path = Path(temp_dir) / f"{source_for_fallback.stem}.dds"
        dds_path.write_bytes(payload)
        run_ffmpeg(dds_path, output)


# ================
# save_image_file
# ================
def save_image_file(source: Path, output: Path) -> None:
    try:
        with Image.open(source) as image:
            image.save(output, "PNG")
        return
    except Exception:
        pass

    ffmpeg = shutil.which("ffmpeg")
    if not ffmpeg:
        raise RuntimeError("Pillow failed and ffmpeg is unavailable")
    run_ffmpeg(source, output)


# ================
# run_ffmpeg
# ================
def run_ffmpeg(source: Path, output: Path) -> None:
    result = subprocess.run(
        [
            "ffmpeg",
            "-hide_banner",
            "-loglevel",
            "error",
            "-y",
            "-i",
            str(source),
            str(output),
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or f"ffmpeg failed with {result.returncode}")


if __name__ == "__main__":
    with generated_assets_lock("image conversion"):
        raise SystemExit(main())
