/*
===========================================================================

censusWorkers.mjs - how many scene workers the action census may run

Each scene worker is a Node process holding a built UI fixture: 300-500 MB
apiece. One worker per scene ran eight at once and, with two client checks
on the machine, pushed it out of memory (#2766). The census now runs at
most DEFAULT_CENSUS_WORKERS workers unless SRO_CENSUS_JOBS says otherwise;
CI sets it high, the shared PC keeps the default.

===========================================================================
*/

export const DEFAULT_CENSUS_WORKERS = 4;

/*
================
censusWorkerCount

SRO_CENSUS_JOBS when it names a positive count, else the shared-PC
default. Never below one: a census with no workers builds nothing.
================
*/
export function censusWorkerCount( env = process.env ) {
	const fromEnv = Number.parseInt( env.SRO_CENSUS_JOBS ?? "", 10 );
	if ( Number.isInteger( fromEnv ) && fromEnv > 0 ) {
		return fromEnv;
	}
	return DEFAULT_CENSUS_WORKERS;
}
