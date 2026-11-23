"""
===========================================================================

import_vsro_res_layouts.py - the later client's quest windows' layouts

The quest list and the mini quest tracker of the later clients are drawn
from res_ui/questlist.2dt and res_ui/questminilist.2dt (#4245); the
v1.150 archives have neither. This step reads both from the pinned VSRO
Media.pk2 (res_ui_layout.py owns the format), publishes each as JSON
under assets/data/res-ui/, and publishes every piece of art they name:
the control textures with whichever _focus/_press/_disable faces the
archive holds, each frame tile or nine-piece frame, and the faces the
quest list swaps in at run time (the blue and red content-view buttons,
the tracker's open fold). index.json lists the layouts and every public
path written, so the build claims exactly what this step produced. The
asset build runs it when SRO_VSRO_ROOT names the pinned archive; no
retail art is committed to Git.

===========================================================================
"""

import argparse
import json
import os
from pathlib import Path

import res_ui_layout
import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import verify_vsro_archive
from import_vsro_tomb_minimap import ddj_payload, entry_name
from sro_paths import PUBLIC_ROOT

LAYOUTS = ("questlist", "questminilist")

# Art the quest windows swap in at run time, which no record names: the
# content-view button's faces for a quest far below or above the
# player's level, and the tracker's unfolded face.
SWAPPED_ART = (
	"interface/quest/qst_contentviewblue_button.ddj",
	"interface/quest/qst_contentviewred_button.ddj",
	"interface/quest/qst_list_open_button.ddj",
)

BUTTON_FACES = ("_focus", "_press", "_disable")
LAYOUT_ROOT = "assets/data/res-ui"
IMAGE_ROOT = "assets/images/Media_extracted"


"""
================
art_entries

The archive entries one layout set needs: each named texture with the
faces the archive holds, and each frame (a .ddj tile as it is, a prefix
as every .ddj that starts with it). A named texture the archive lacks
refuses the import.
================
"""
def art_entries(names, textures, frames):
	wanted = set()
	for texture in textures:
		if texture not in names:
			raise SystemExit(f"The pinned archive has no {texture}, which a quest layout names.")
		wanted.add(texture)
		stem = texture[:-len(".ddj")]
		wanted.update(stem + face + ".ddj" for face in BUTTON_FACES if stem + face + ".ddj" in names)
	for frame in frames:
		pieces = [frame] if frame.endswith(".ddj") else [name for name in names if name.startswith(frame) and name.endswith(".ddj")]
		if not pieces or any(piece not in names for piece in pieces):
			raise SystemExit(f"The pinned archive has no art for the frame {frame}.")
		wanted.update(pieces)
	return sorted(wanted)


"""
================
public_image

The public path of one archive image: the v1.150 extraction's layout,
with the .ddj published as .png.
================
"""
def public_image(entry):
	return f"{IMAGE_ROOT}/{entry[:-len('.ddj')]}.png"


def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument(
		"--vsro-root", default=os.environ.get("SRO_VSRO_ROOT"),
		help="the pinned VSRO client folder (else SRO_VSRO_ROOT)"
	)
	args = parser.parse_args()
	if not args.vsro_root:
		raise SystemExit("Pass --vsro-root or set SRO_VSRO_ROOT to the pinned VSRO client folder.")
	verify_vsro_archive(args.vsro_root, "Media")
	data = sro_pk2.map_archive(Path(args.vsro_root) / "Media.pk2")
	directory = sro_pk2.read_directory(data)
	entries = {
		entry_name(entry).lower().replace("\\", "/"): entry
		for entry in (directory.values() if isinstance(directory, dict) else directory)
	}
	layouts, textures, frames = [], set(SWAPPED_ART), set()
	for name in LAYOUTS:
		entry = f"res_ui/{name}.2dt"
		if entry not in entries:
			raise SystemExit(f"The pinned archive has no {entry}.")
		layout = res_ui_layout.read_layout(name, bytes(sro_pk2.payload(data, entries[entry])))
		named, framed = res_ui_layout.layout_art(layout)
		textures.update(named)
		frames.update(framed)
		layouts.append(layout)
	published = []
	for layout in layouts:
		target = PUBLIC_ROOT / LAYOUT_ROOT / f"{layout['name']}.json"
		target.parent.mkdir(parents=True, exist_ok=True)
		target.write_text(json.dumps(layout, ensure_ascii=False, indent="\t") + "\n", encoding="utf-8")
		published.append(f"/{LAYOUT_ROOT}/{layout['name']}.json")
	index_path = PUBLIC_ROOT / LAYOUT_ROOT / "index.json"
	ours = set(json.loads(index_path.read_text(encoding="utf-8"))["published"]) if index_path.exists() else set()
	for entry in art_entries(entries.keys(), textures, frames):
		target = PUBLIC_ROOT / public_image(entry)
		# The v1.150 extraction's own art wins: other windows draw these
		# frames, so only a missing file or this step's own is written.
		if target.exists() and "/" + public_image(entry) not in ours:
			continue
		target.parent.mkdir(parents=True, exist_ok=True)
		save_image_payload(ddj_payload(sro_pk2.payload(data, entries[entry])), target, target)
		published.append("/" + public_image(entry))
	index = {"layouts": list(LAYOUTS), "published": published}
	index_path.write_text(json.dumps(index, indent="\t") + "\n", encoding="utf-8")
	print(f"{len(LAYOUTS)} res_ui layout(s) and {len(published) - len(LAYOUTS)} image(s) imported")


if __name__ == "__main__":
	main()
