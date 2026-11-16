/*
===========================================================================

degradation-record.ts - a bounded, once-per-message record of skipped steps

An owner that survives a data gap (one cosmetic step skipped, never the
session) records it here and drains the list into degraded client
reports. Each message is recorded once and an owner records at most
MAX_RECORDED_DEGRADATIONS, so one bad catalogue row cannot flood the
Agent (network.ts DEGRADED_CODES_PER_SESSION bounds codes the same way).

===========================================================================
*/
import type { Degradation } from "@/engine/contracts/network";

// At most this many distinct degradations per owner lifetime.
export const MAX_RECORDED_DEGRADATIONS = 16;

/*
================
DegradationRecord

The owner's pending degradations and every message it has recorded.
================
*/
export interface DegradationRecord {
	degradations: Degradation[];
	degradedKeys: Set<string>;
}

/*
================
recordDegradation

Records one skipped step once. A presentation data gap - an action whose
phase timeline the model lacks, a knockdown without a recovery duration -
costs that one cosmetic step, never the session: the server owns the
outcome and the rest of the world keeps drawing. The original client plays
no motion for an animation it cannot find; it does not stop.
================
*/
export function recordDegradation( record: DegradationRecord, code: string, message: string ) {
	if ( record.degradedKeys.has( message ) || record.degradedKeys.size >= MAX_RECORDED_DEGRADATIONS ) return;
	record.degradedKeys.add( message );
	record.degradations.push( { code, message } );
}
