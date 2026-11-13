"""
===========================================================================

script_paths.py - the one owner of the Python test tree's import path

Every test module here imports the scripts it tests by bare name
(cap110_monsters, generate_loot_catalog, ...). Those live in scripts/,
scripts/build, scripts/build/catalogue and scripts/analysis, which
`unittest discover -s scripts/test/python` does not put on sys.path: it
adds only this folder.

Each module used to insert its own folder. #3410: six modules inserted
none and imported only because a sibling, run earlier in the full
discovery, had done it for them; run alone they failed at import. Now
every test module starts with `import script_paths` and this file is the
one list. No module name repeats across these folders, so their order
does not change what imports.

===========================================================================
"""

import sys
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parents[2]
FOLDERS = (SCRIPTS, SCRIPTS / "build", SCRIPTS / "build" / "catalogue", SCRIPTS / "analysis")

for folder in FOLDERS:
	if str(folder) not in sys.path:
		sys.path.insert(0, str(folder))
