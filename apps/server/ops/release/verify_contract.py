"""
===========================================================================

verify_contract.py - compare release admission with compiled runtime authorities.

The server's protocol and schema owners emit the actual versions. A declaration
that disagrees fails candidate preparation before an archive can be staged.

===========================================================================
"""

import json
from pathlib import Path
import sys

from release_state import compatibility


# ================
# verify
#
# Require exact capability equality; an unimplemented wider range is as unsafe
# as a lower schema version that would hide a required database migration.
# ================
def verify(actual, declared):
	compatibility(actual, "server")
	if actual != declared["server"]:
		raise ValueError("release compatibility differs from compiled server authorities")


# ================
# main
#
# Read the runtime report from stdin so no generated document enters source.
# ================
def main():
	declaration = Path(__file__).with_name("compatibility.json")
	verify(json.load(sys.stdin), json.loads(declaration.read_text()))


if __name__ == "__main__":
	main()
