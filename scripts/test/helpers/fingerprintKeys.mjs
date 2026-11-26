/*
===========================================================================

fingerprintKeys.mjs - the resource fingerprint's key roots, stubbed

computeResourceBuildFingerprint hashes the server's sro-evidence exports
by running them (serverExportsKey). A test about another root, a knob or
a switch passes these keys instead, so it builds no Go tool and runs
against a fixture server tree that has no Go code.

===========================================================================
*/

/** A fixed stand-in for every key root. */
export const STUB_FINGERPRINT_KEYS = Object.freeze( { serverExports: () => "stub-server-exports" } );
