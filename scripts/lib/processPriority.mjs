/*
===========================================================================

processPriority.mjs - checks yield the CPU to the local game servers

Several agents run full client checks and server gates on the one machine
that also hosts the shared local Agent and GameWorld. A check saturates
every core, and the GameWorld's tick workers then sit runnable but
unscheduled: on 2026-10-25 its watchdog restarted it twice for a tick that
was only starved (#1578). A check lowers its own priority to below normal
before it starts its gate; every process it spawns inherits that class
(Windows: a child of a BELOW_NORMAL process defaults to it; POSIX: the
nice value is inherited), so the servers and the player's browser win
the CPU while the check still uses whatever is left.

===========================================================================
*/
import os from "node:os";

// SRO_CHECK_PRIORITY=normal keeps a check at normal priority (a machine
// that runs nothing else).
const PRIORITY_ENV = "SRO_CHECK_PRIORITY";

/*
================
yieldCpu

Lowers this process to below-normal priority unless the environment opts
out. Returns the priority now in force, or null when the platform refused
(a check never fails over its own priority). priority is node:os's
getPriority/setPriority/constants, a parameter so the decision is tested
apart from the live process, whose priority other actors on a shared
machine can change (#1651).
================
*/
export function yieldCpu( env = process.env, priority = os ) {
	if ( (env[PRIORITY_ENV] ?? "").trim().toLowerCase() === "normal" ) return null;
	try {
		if ( priority.getPriority() < priority.constants.priority.PRIORITY_BELOW_NORMAL ) {
			priority.setPriority( priority.constants.priority.PRIORITY_BELOW_NORMAL );
		}
		return priority.getPriority();
	} catch {
		return null;
	}
}
