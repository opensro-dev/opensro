/*
===========================================================================

processPriority.mjs - checks and probe browsers yield the CPU

Several agents run full client checks, server gates and probe browsers on
the one machine that also hosts the shared local Agent and GameWorld and
the owner's own desktop. A check saturates every core, and the
GameWorld's tick workers then sit runnable but unscheduled: on 2026-10-25
its watchdog restarted it twice for a tick that was only starved (#1578).
A check lowers its own priority to below normal before it starts its
gate; every process it spawns inherits that class (Windows: a child of a
BELOW_NORMAL process defaults to it; POSIX: the nice value is inherited),
so the servers and the player's browser win the CPU while the check still
uses whatever is left.

Inheritance holds only while no process raises itself, and Chrome does:
on Windows it starts its GPU process at ABOVE_NORMAL. A probe holder
started at idle on 2026-11-26 still had a software-WebGPU GPU process
above the desktop on every core. A probe browser's tree is therefore held
in a Windows job (processTreeJob.ps1) whose priority limit no member can
raise and whose hard cap leaves the rest of the machine to everyone else.

===========================================================================
*/
import { execFile } from "node:child_process";
import os from "node:os";
import { basename } from "node:path";
import { fileURLToPath } from "node:url";

// SRO_CHECK_PRIORITY=normal keeps a check at normal priority (a machine
// that runs nothing else).
const PRIORITY_ENV = "SRO_CHECK_PRIORITY";
// SRO_PROBE_CPU_PERCENT holds a probe browser's whole tree to that share
// of the machine, 1 to 100 (100 lifts the cap). Half leaves the servers,
// the other agents and the desktop a machine's worth of cores between
// them, whatever a software renderer asks for.
const CPU_PERCENT_ENV = "SRO_PROBE_CPU_PERCENT";
const DEFAULT_CPU_PERCENT = 50;
const MAX_CPU_PERCENT = 100;
const JOB_HELPER = fileURLToPath( new URL( "./processTreeJob.ps1", import.meta.url ) );
// The helper compiles its P/Invoke shim on start: seconds on a busy
// machine. A bound that cannot be set in time is reported, never fatal.
const JOB_HELPER_TIMEOUT_MS = 60000;
// Chrome's image name when the launch names no executable (channel "chrome").
const DEFAULT_BROWSER_IMAGE = "chrome.exe";

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

/*
================
childTreeBound

The bound a probe browser's tree runs under: no higher priority class
than its caller's (idle stays idle, anything above becomes below normal)
and the CPU share SRO_PROBE_CPU_PERCENT names. SRO_CHECK_PRIORITY=normal
lifts the priority limit as it does for a check; the CPU cap still holds.
env and priority are parameters, as for yieldCpu, so the decision is
tested apart from the live process.
================
*/
export function childTreeBound( env = process.env, priority = os ) {
	const asked = Number( (env[CPU_PERCENT_ENV] ?? "").trim() );
	const cpuPercent = Number.isInteger( asked ) && asked >= 1 && asked <= MAX_CPU_PERCENT ?
		asked :
		DEFAULT_CPU_PERCENT;
	if ( (env[PRIORITY_ENV] ?? "").trim().toLowerCase() === "normal" ) return { priorityClass: "None", cpuPercent };
	let own = priority.constants.priority.PRIORITY_NORMAL;
	try {
		own = priority.getPriority();
	} catch {
		// An unreadable class is treated as normal: the bound still applies.
	}
	const priorityClass = own >= priority.constants.priority.PRIORITY_LOW ? "Idle" : "BelowNormal";
	return { priorityClass, cpuPercent };
}

/*
================
browserImage

The process image name a launch starts: the named executable's file, or
Chrome's own.
================
*/
export function browserImage( executablePath ) {
	return executablePath ? basename( executablePath ) : DEFAULT_BROWSER_IMAGE;
}

/*
================
boundChildren

Holds every child of this process named imageName, and everything those
start, inside this process's bounded job. Windows only: elsewhere no
unprivileged process can raise itself above the class it inherited, so
inheritance already holds. Resolves the helper's report, or null when
there is nothing to do or the bound could not be set (reported on stderr;
a probe never fails over its own priority). The helper never holds its
caller open: a caller that is done exits, and the helper finishes alone.
================
*/
export function boundChildren( imageName, bound = childTreeBound(), platform = process.platform ) {
	if ( platform !== "win32" ) return Promise.resolve( null );
	const args = [
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-File",
		JOB_HELPER,
		"-ParentPid",
		String( process.pid ),
		"-ImageName",
		imageName,
		"-PriorityClass",
		bound.priorityClass,
		"-CpuPercent",
		String( bound.cpuPercent === MAX_CPU_PERCENT ? 0 : bound.cpuPercent )
	];
	return new Promise( resolve => {
		const helper = execFile(
			"powershell.exe",
			args,
			{ timeout: JOB_HELPER_TIMEOUT_MS, windowsHide: true },
			( error, stdout, stderr ) => {
				if ( error ) {
					process.stderr.write(
						`processPriority: probe browser runs unbounded: ${(stderr || error.message).trim()}\n`
					);
					resolve( null );
					return;
				}
				try {
					resolve( JSON.parse( stdout.trim().split( /\r?\n/ ).pop() ) );
				} catch {
					process.stderr.write( `processPriority: unreadable job report: ${stdout.trim()}\n` );
					resolve( null );
				}
			}
		);
		helper.unref();
		// The pipes are sockets, which unref; the stream types do not say so.
		for ( const stream of [ helper.stdin, helper.stdout, helper.stderr ] ) {
			if ( stream && "unref" in stream && typeof stream.unref === "function" ) stream.unref();
		}
	} );
}
