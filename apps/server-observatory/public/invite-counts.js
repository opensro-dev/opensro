/*
===========================================================================
invite-counts.js - what the invite list's counts mean

The website answers counts per filter (accounts.ts queryInvites): the
total under every filter, and each facet under every filter except its
own. These two readings turn that answer into the numbers the filter
form shows; both are pinned by tests/invite-counts.test.mjs.
===========================================================================
*/

/*
================
allStatusCount

How many keys choosing "All" statuses would show: the status facet
summed. The website counts each status class under every filter except
status, so the sum is the list without the status filter. (The answer's
total counts under the status filter too and would label All with the
filtered number.)
================
*/
export function allStatusCount( counts ) {
	return Object.values( counts.status ).reduce( ( sum, count ) => sum + count, 0 );
}

/*
================
openCount

The keys that can still be redeemed, as the status line says: with no
status chosen, the facet's unused plus partly used; under Unused or
Partly used, every key that matched is open; under a closed class, none
is.
================
*/
export function openCount( counts, statusFilter ) {
	if ( statusFilter === "unused" || statusFilter === "partial" ) return counts.total;
	if ( statusFilter ) return 0;
	return counts.status.unused + counts.status.partial;
}
