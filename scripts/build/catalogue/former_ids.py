"""
===========================================================================

former_ids.py - the ids a published monster left, for the catalogue

A monster the pin moved off a released id (published_ids.settle_shared,
#3994) keeps a link to its old page: the catalogue's monster row names the
old id in formerIds, the website redirects it, and the host's catalogue
gate accepts the lost id because the same monster names it. The pin is
the one owner of that record; this prints its "characters" part as JSON,
codename -> [ids], for buildCatalogue.mjs.

===========================================================================
"""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import published_ids  # noqa: E402


# ================
# main
# ================
def main():
	print(json.dumps(published_ids.former_ids("characters"), sort_keys=True))


if __name__ == "__main__":
	main()
