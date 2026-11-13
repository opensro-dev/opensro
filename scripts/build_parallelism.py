#
# ===========================================================================
#
# build_parallelism.py - how many processes a Python build stage runs
#
# The Python half of SRO_BUILD_JOBS: scripts/build/shared/buildParallelism.mjs
# owns the rule, and every Python stage with a process pool (image
# conversion, the catalogue's WebP encodes) asks build_jobs here. The
# module imports nothing of the build's roots, so any script can use it.
#
# ===========================================================================
#
import os

# ================
# build_jobs
#
# SRO_BUILD_JOBS (scripts/build/shared/buildParallelism.mjs owns the rule):
# a positive integer, else every core but one. Under the Node build the
# variable is always set, to a count capped by free memory; a stage run on
# its own has no free-memory reading, so set SRO_BUILD_JOBS for it by hand
# on a small machine.
# ================
def build_jobs() -> int:
    raw = os.environ.get("SRO_BUILD_JOBS", "")
    if not raw:
        return max(1, (os.cpu_count() or 2) - 1)
    if not raw.isdigit() or int(raw) < 1:
        raise SystemExit(f"SRO_BUILD_JOBS must be a positive integer; got {raw!r}")
    return int(raw)
