"""
===========================================================================

import_vsro_worldmap_buttons.py - the later client's M-map buttons

The v1.150 archives ship the small titlebar world button but not the
dungeon-era map chrome the later client draws over the map itself: the
big globe that leaves a dungeon page, the pushpin that shows the map's
follow mode (its two faces are named for the captioned Auto Move /
Manual Move states), and the right-edge floor selector of a dungeon
page (up and down arrows with their disabled faces, the floor box, its
chosen variant and the tall track behind them). The asset build runs
this step when SRO_VSRO_ROOT names the pinned archive (#2956), so a
build on a machine with the archive publishes the buttons on its own;
run this by hand only to refresh a built tree without one, and follow
it with `pnpm assets repack`: the client loads published assets only.
No retail art is committed to Git.

===========================================================================
"""
import argparse
import os
from pathlib import Path

import sro_pk2
from convert_images import save_image_payload
from import_vsro_alexandria import verify_vsro_archive
from import_vsro_tomb_minimap import ddj_payload, entry_name
from sro_paths import PUBLIC_ROOT

ARCHIVE_ROOT = "interface/"
# One image each, by its path under interface/: the globe, the follow
# pin's two mode faces, the floor arrows in their working and disabled
# faces, the floor boxes over their track, and the v1.188 window's own
# chrome (worldmapguide.2dt): the foot frame that carries the level
# note, the top-left corner the side tab hangs from, the map panel's
# four corner edges and the guide's Field and Monster tabs in their
# two faces. The focus and press faces of the globe and the pin stay
# in the archive: the map tools draw one state face per frame, as they
# already draw the checkbutton's on and off faces.
BUTTONS = (
	"worldmap/wmap_big_button_world",
	"worldmap/wmap_button_automatic",
	"worldmap/wmap_button_passivity",
	"worldmap/wmap_up_button",
	"worldmap/wmap_up_button_disable",
	"worldmap/wmap_down_button",
	"worldmap/wmap_down_button_disable",
	"worldmap/wmap_floor_box",
	"worldmap/wmap_floor_choice_box",
	"worldmap/wmap_floor_big_box",
	"worldmap/wmap_wnd_bottom_frame",
	"worldmap/wmap_wnd_corner_02",
	"worldmap/wmap_window_edge_1",
	"worldmap/wmap_window_edge_2",
	"worldmap/wmap_window_edge_3",
	"worldmap/wmap_window_edge_4",
	"guide/gd_area_tab_on",
	"guide/gd_area_tab_off",
	"guide/gd_monster_tab_on",
	"guide/gd_monster_tab_off",
	"guide/gd_long_index_open",
	"guide/gd_long_contents_event",
)


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
	data = (Path(args.vsro_root) / "Media.pk2").read_bytes()
	directory = sro_pk2.read_directory(data)
	entries = {
		entry_name(entry).lower().replace("\\", "/"): entry
		for entry in (directory.values() if isinstance(directory, dict) else directory)
	}

	images = PUBLIC_ROOT / "assets" / "images" / "Media_extracted" / "interface"
	for name in BUTTONS:
		entry = ARCHIVE_ROOT + name + ".ddj"
		if entry not in entries:
			raise SystemExit(f"The pinned archive has no {entry}; the button set is incomplete.")
		target = images / f"{name}.png"
		target.parent.mkdir(parents=True, exist_ok=True)
		save_image_payload(ddj_payload(sro_pk2.payload(data, entries[entry])), target, target)
	print(f"{len(BUTTONS)} world map image(s) imported")


if __name__ == "__main__":
	main()
