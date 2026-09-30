"""
===========================================================================

prepare_client_resources.py - extract a licensed v1.150 client for the build

The asset build reads an extracted client, not the archives. This tool makes
that extraction on the operator's own machine from their own client, so no
game data ever enters the repository:

	<game root>/Media.pk2      -> extracted/Media_extracted/
	<game root>/Data.pk2       -> extracted/Data_extracted/
	<game root>/Map.pk2        -> extracted/Map_extracted/
	<game root>/Particles.pk2  -> extracted/Particles_extracted/
	<game root>/Music.pk2      -> extracted/Music_mp3/  (OGG tracks as MP3)

Every file lands at its archive path (CP949-decoded, original case) with its
stored bytes. Files already correct are left alone and wrong or missing ones
are rewritten, so an interrupted run is repaired by running again. Files the
archives do not contain are never touched.

extracted/.opensro-preparation.json records each archive's hash and file
count once that archive is complete; `pnpm assets doctor` reads it.

Music: the client's BGM references (textdata effectenvsnd, event and fortress
music) use the ASCII-named tracks. The CP949-named tracks are referenced only
by the legacy resinfo table the port does not read, so they are not
converted. `ffmpeg -codec:a libmp3lame -q:a 0` with the recorded ffmpeg
reproduces the existing published MP3s byte for byte.

	py -3 scripts/prepare_client_resources.py [--only Media,Data,...]

The game root is SRO_GAME_ROOT or the parent of the checkout (sro_paths.py).

===========================================================================
"""
import argparse
import hashlib
import json
import mmap
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import sro_pk2  # noqa: E402
from sro_paths import GAME_ROOT  # noqa: E402

MANIFEST_FORMAT = "opensro-client-preparation-v1"
MANIFEST_NAME = ".opensro-preparation.json"
# (archive, output folder under extracted/)
RESOURCE_ARCHIVES = (
	("Media", "Media_extracted"),
	("Data", "Data_extracted"),
	("Map", "Map_extracted"),
	("Particles", "Particles_extracted"),
)
MUSIC_ARCHIVE = "Music"
MUSIC_FOLDER = "Music_mp3"
MUSIC_ENCODER_ARGS = ["-codec:a", "libmp3lame", "-q:a", "0"]
# Characters Windows refuses in a file name; ":" would open an alternate stream.
INVALID_NAME_CHARACTERS = set('<>:"|?*') | {chr(n) for n in range(32)}
HASH_CHUNK = 1 << 20
PROGRESS_EVERY = 5000


def log(message):
	print(message, flush=True)


def file_digest(path):
	hasher = hashlib.sha256()
	with open(path, "rb") as source:
		for chunk in iter(lambda: source.read(HASH_CHUNK), b""):
			hasher.update(chunk)
	return hasher.hexdigest()


#============================================================================


def read_manifest(extracted):
	path = extracted / MANIFEST_NAME
	if not path.exists():
		return {"format": MANIFEST_FORMAT, "archives": {}}
	manifest = json.loads(path.read_text(encoding="utf-8"))
	if manifest.get("format") != MANIFEST_FORMAT:
		raise SystemExit(f"{path}: unknown preparation format {manifest.get('format')!r}")
	return manifest


def write_manifest(extracted, manifest):
	path = extracted / MANIFEST_NAME
	partial = path.with_name(path.name + ".partial")
	partial.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
	os.replace(partial, path)


def safe_target(root, archive_path):
	"""The output path of one archive entry, refused if it could leave root."""
	parts = archive_path.split("/")
	for part in parts:
		if part in ("", ".", "..") or any(c in INVALID_NAME_CHARACTERS for c in part):
			raise ValueError(f"Archive path is not a safe file name: {archive_path!r}")
	target = root.joinpath(*parts)
	if root.resolve() not in target.resolve().parents:
		raise ValueError(f"Archive path escapes the output folder: {archive_path!r}")
	return target


def write_atomically(target, data):
	target.parent.mkdir(parents=True, exist_ok=True)
	partial = target.with_name(target.name + ".partial")
	with open(partial, "wb") as out:
		out.write(data)
	os.replace(partial, target)


#============================================================================


def extract_archive(archive_path, output):
	"""
	Extract every file of one archive into output. Returns (files, written,
	archive sha256). A file already holding the stored bytes is kept.
	"""
	with open(archive_path, "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
		archive_hash = hashlib.sha256(data).hexdigest()
		entries = sro_pk2.read_directory(data)
		written = 0
		for number, entry in enumerate(entries, 1):
			target = safe_target(output, entry.path)
			stored = sro_pk2.payload(data, entry)
			if not (target.is_file() and target.stat().st_size == entry.size and file_digest(target) == sro_pk2.digest(stored)):
				write_atomically(target, stored)
				written += 1
			if number % PROGRESS_EVERY == 0:
				log(f"  {number}/{len(entries)} files checked, {written} written")
		return len(entries), written, archive_hash


def ffmpeg_version(ffmpeg):
	result = subprocess.run([ffmpeg, "-hide_banner", "-version"], capture_output=True, text=True, check=True)
	return result.stdout.splitlines()[0]


def find_ffmpeg():
	"""The ffmpeg that converts the music, refused before any record changes."""
	ffmpeg = shutil.which("ffmpeg")
	if not ffmpeg:
		raise SystemExit("ffmpeg is not on PATH; it converts the Music.pk2 tracks (see docs/GETTING_STARTED.md)")
	return ffmpeg


def prepare_music(archive_path, output, previous, ffmpeg):
	"""
	Convert the ASCII-named OGG tracks to MP3. A track whose source hash,
	encoder and ffmpeg are unchanged since the last run, and whose MP3 still
	exists, is kept.
	"""
	version = ffmpeg_version(ffmpeg)
	same_encoder = previous.get("ffmpeg") == version and previous.get("encoder") == MUSIC_ENCODER_ARGS
	previous_tracks = previous.get("tracks", {}) if same_encoder else {}
	with open(archive_path, "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
		archive_hash = hashlib.sha256(data).hexdigest()
		entries = [e for e in sro_pk2.read_directory(data) if e.path.lower().endswith(".ogg") and e.path.isascii()]
		output.mkdir(parents=True, exist_ok=True)
		tracks, converted = {}, 0
		with tempfile.TemporaryDirectory(prefix="opensro-music-") as scratch:
			for entry in entries:
				stored = sro_pk2.payload(data, entry)
				source_hash = sro_pk2.digest(stored)
				name = Path(entry.path).stem + ".mp3"
				target = safe_target(output, name)
				known = previous_tracks.get(name)
				if known and known["source"] == source_hash and target.is_file() and file_digest(target) == known["mp3"]:
					tracks[name] = known
					continue
				# A file input (not a pipe) keeps the container probe identical to
				# the recorded conversions.
				source = Path(scratch) / Path(entry.path).name
				source.write_bytes(stored)
				partial = Path(scratch) / name
				subprocess.run(
					[ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", str(source), *MUSIC_ENCODER_ARGS, str(partial)],
					check=True
				)
				write_atomically(target, partial.read_bytes())
				tracks[name] = {"source": source_hash, "mp3": file_digest(target)}
				converted += 1
	if not tracks:
		raise SystemExit(f"{archive_path} holds no ASCII-named OGG tracks")
	return {
		"archive": archive_path.name,
		"sha256": archive_hash,
		"ffmpeg": version,
		"encoder": MUSIC_ENCODER_ARGS,
		"tracks": tracks
	}, converted


#============================================================================


def main():
	parser = argparse.ArgumentParser(description="Extract a licensed v1.150 client for the asset build.")
	names = [name for name, _ in RESOURCE_ARCHIVES] + [MUSIC_ARCHIVE]
	parser.add_argument("--only", help="comma-separated subset of " + ",".join(names))
	args = parser.parse_args()
	selected = names if not args.only else [n.strip() for n in args.only.split(",")]
	unknown = sorted(set(selected) - set(names))
	if unknown:
		raise SystemExit(f"Unknown archives {unknown}; choose from {names}")

	missing = [f"{name}.pk2" for name in selected if not (GAME_ROOT / f"{name}.pk2").is_file()]
	if missing:
		raise SystemExit(f"Game root {GAME_ROOT} lacks {', '.join(missing)}. Set SRO_GAME_ROOT to the client folder.")
	# A missing encoder fails before any archive work or record change.
	ffmpeg = find_ffmpeg() if MUSIC_ARCHIVE in selected else None
	extracted = GAME_ROOT / "extracted"
	extracted.mkdir(exist_ok=True)
	manifest = read_manifest(extracted)
	log(f"Game root: {GAME_ROOT}")

	for name, folder in RESOURCE_ARCHIVES:
		if name not in selected:
			continue
		log(f"{name}.pk2 -> extracted/{folder}")
		# Clear the record first: an interrupted run must read as incomplete.
		manifest["archives"].pop(name, None)
		write_manifest(extracted, manifest)
		files, written, archive_hash = extract_archive(GAME_ROOT / f"{name}.pk2", extracted / folder)
		manifest["archives"][name] = {"output": folder, "sha256": archive_hash, "files": files}
		write_manifest(extracted, manifest)
		log(f"  {files} files, {written} written")

	if MUSIC_ARCHIVE in selected:
		log(f"{MUSIC_ARCHIVE}.pk2 -> extracted/{MUSIC_FOLDER}")
		previous = manifest.get("music", {})
		manifest.pop("music", None)
		write_manifest(extracted, manifest)
		record, converted = prepare_music(GAME_ROOT / f"{MUSIC_ARCHIVE}.pk2", extracted / MUSIC_FOLDER, previous, ffmpeg)
		manifest["music"] = record
		write_manifest(extracted, manifest)
		log(f"  {len(record['tracks'])} tracks, {converted} converted")

	log("Preparation complete. Next: pnpm assets doctor")


if __name__ == "__main__":
	main()
