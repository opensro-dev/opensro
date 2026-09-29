"""
===========================================================================

plan.py - bind a tested artifact to the production generation it will replace.

Build jobs create this declaration before approval. Deployment jobs recheck
relevant source inputs against main; the host separately checks live state.
Neither check can substitute for the other.

===========================================================================
"""

import argparse
import json
from pathlib import Path
import subprocess

from release_state import PLAN_FORMAT, admit, identity, read_state

MAX_ANCESTORS = 4096
INPUTS = {
	"client": [
		"apps/client-next", "scripts/build", "scripts/lib", "package.json", "pnpm-lock.yaml",
		"pnpm-workspace.yaml", ".npmrc",
		"apps/server/ops/release",
	],
	"server": ["apps/server"],
}


# ================
# git
#
# Arguments are passed directly to Git; candidate text never becomes shell code.
# ================
def git(arguments, root=None):
	return subprocess.check_output(["git", *arguments], cwd=root, text=True).strip()


# ================
# require_current
#
# A later unrelated change does not invalidate a component build. Changes to
# any of its declared inputs do, even if GitHub still offers the old approval.
# ================
def require_current(component, commit, root=None):
	identity(commit)
	if component not in INPUTS:
		raise ValueError("unknown release component")
	subprocess.run(["git", "merge-base", "--is-ancestor", commit, "origin/main"], cwd=root, check=True)
	changed = git(["diff", "--name-only", commit, "origin/main", "--", *INPUTS[component]], root)
	if changed:
		raise ValueError("candidate was superseded by newer component inputs:\n" + changed)


# ================
# build_plan
#
# The caller supplies the immutable artifact identity after building it. The
# plan preserves the production generation read before that build began.
# ================
def build_plan(component, release, state, root=None, kind="application"):
	if root is None:
		root = Path(git(["rev-parse", "--show-toplevel"]))
	commit = git(["rev-parse", "HEAD"], root)
	contract_path = root / "apps/server/ops/release/compatibility.json"
	contracts = json.loads(contract_path.read_text(encoding="utf-8"))
	current = state[component]
	plan = {
		"format": PLAN_FORMAT,
		"component": component,
		"release": identity(release),
		"commit": commit,
		"baseRelease": current["release"],
		"baseGeneration": current["generation"],
		"compatibility": contracts[component],
		"mode": "forward",
		"ancestors": git(["rev-list", "--max-count=" + str(MAX_ANCESTORS), "HEAD"], root).splitlines(),
	}
	# A data release carries its own asset data (client_data.py); an
	# application release reuses the live data and needs no marker.
	if kind != "application":
		plan["kind"] = kind
	admit(state, plan)
	return plan


# ================
# main
#
# Separate freshness validation from plan construction so the approved job can
# repeat it immediately before contacting the host.
# ================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("component", choices=tuple(INPUTS))
	parser.add_argument("--check-commit")
	parser.add_argument("--state")
	parser.add_argument("--release")
	parser.add_argument("--output")
	arguments = parser.parse_args()
	if arguments.check_commit:
		require_current(arguments.component, arguments.check_commit)
		return
	if not all((arguments.state, arguments.release, arguments.output)):
		parser.error("building a plan requires --state, --release and --output")
	plan = build_plan(arguments.component, arguments.release, read_state(arguments.state))
	Path(arguments.output).write_text(json.dumps(plan, indent=2) + "\n", encoding="utf-8", newline="\n")


if __name__ == "__main__":
	main()
