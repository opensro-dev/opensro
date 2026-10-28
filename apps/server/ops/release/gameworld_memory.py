"""
===========================================================================

gameworld_memory.py - the GameWorld memory reservation a release deploys

The GameWorld job (gameworld.nomad.hcl) owns the reservation's default
and derives GOMEMLIMIT from it: three quarters, in MiB. The host config
may raise the reservation, never lower it. This module refuses a lower
one before a deployment touches anything, and after the deploy checks
that every registered GameWorld job renders what the release intended.
A 1024 left in the host config after #1946 kept the live soft limit at
768MiB, with the collector running every 1.6 s (#2027).

===========================================================================
"""

import json

# The GameWorld job's memory_mb default (gameworld.nomad.hcl, #1946).
GAMEWORLD_MEMORY_FLOOR_MB = 3072


# ================
# gameworld_memory_mb
#
# The GameWorld reservation the release deploys: the host config's value
# when set, else the job's default. Refused below the floor before any
# backup, notice or stop (the rehearsal reaches it at staging).
# ================
def gameworld_memory_mb(config):
	memory = config.get("gameworld_memory_mb", GAMEWORLD_MEMORY_FLOOR_MB)
	if type(memory) is not int or memory < GAMEWORLD_MEMORY_FLOOR_MB:
		raise ValueError("gameworld_memory_mb must be an integer of at least " + str(GAMEWORLD_MEMORY_FLOOR_MB) +
			" (the job's default, #1946); the host config has " + repr(memory))
	return memory


# ================
# memory_limit_problems
#
# What the registered GameWorld job actually renders against what the
# release intended: the task reserves memory MB, and GOMEMLIMIT is three
# quarters of it in MiB (the job's env formula). Empty means it matches.
# ================
def memory_limit_problems(job, memory):
	job = job.get("Job", job)
	want = str(memory * 3 // 4) + "MiB"
	found = []
	tasks = [task for group in job.get("TaskGroups") or [] for task in group.get("Tasks") or []
		if "GOMEMLIMIT" in (task.get("Env") or {})]
	if not tasks:
		found.append(job.get("ID", "the job") + " renders no GOMEMLIMIT")
	for task in tasks:
		reserved = (task.get("Resources") or {}).get("MemoryMB")
		if reserved != memory:
			found.append("%s reserves %r MB, intended %d" % (task.get("Name"), reserved, memory))
		if task["Env"]["GOMEMLIMIT"] != want:
			found.append("%s GOMEMLIMIT is %s, intended %s" % (task.get("Name"), task["Env"]["GOMEMLIMIT"], want))
	return found


# ================
# verify_memory_limit
#
# After the deploy passes Nomad's health checks and before the release is
# recorded: every enabled shard's registered job must render the intended
# reservation and soft limit (#2027). run is deploy.py's command runner.
# ================
def verify_memory_limit(shards, environment, memory, run):
	found = []
	for shard in shards:
		inspected = run(["nomad", "job", "inspect", "-namespace", "sro", "-json", "sro-gameworld-" + shard],
			env=environment, capture=True)
		found += memory_limit_problems(json.loads(inspected.stdout), memory)
	if found:
		raise RuntimeError("GameWorld memory limit differs from the release: " + "; ".join(found))
