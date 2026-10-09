# Companion victim owner protection

Bounded follow-up to issue #219. This fixes ongoing companion combat and the
shared target resolver; it does not establish the complete AI callback graph.

## Native evidence

The original v1.188 `SR_GameServer.exe` has SHA-256
`bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`
and image base `0x400000`. Addresses below are VAs.

Binary Ninja identified the common attack validator at `0x5291D0`. Raw
instructions in the hash-matched executable confirm that, after checking a
COS target, it loads the target's owner from `+0x1CD8` at `0x529304`, reads
the owner's body mode through virtual `+0x100`, and compares against 2, 3,
and 4 at `0x52931A..0x529324`. Each match branches to refusal at `0x52935D`.
The PC permission validator also checks a COS before normalizing to its owner
at `0x5293DB` and checking that owner at `0x5293F2`.

The 69 bytes `[0x5292FE,0x529343)` have SHA-256
`fdf0e6d651623eef5654697d945d3f72e42bed5664c5103e96b1ecff52abc7e9`.
The disassembly is saved in the canonical research target's notes as
`001292fe-companion-owner-protection-2026-10-10.txt`. No database labels
were changed; the encountered validators were already named.

## Port gap and coverage

`resolvePetCombatTarget` checked owner body protection when a player GID
resolved to a player or their mount, but its direct COS branch checked only
the companion's body. A prepared attack targeting a companion could remain
active when its owner entered protection.

The direct COS branch now applies the existing `petAttackBodyAllowed` helper
to the owner snapshot. Command admission, ongoing combat and area candidate
selection all use this resolver. Target identity and damage routing remain
with the companion, and the existing cancellation owner closes a prepared
cast when target resolution fails.

The behavioral regression seeds an admitted AI target independently of command
routing, prepares a real cast against an initially unprotected companion, then
changes its owner's body mode. Modes 2, 3 and 4 retire combat, preserve the
companion's HP beyond the release deadline, and emit exactly one finalize.
Modes 0, 1 and 5 retain the prepared token. The unmodified production resolver
fails the three protected cases. Existing player-target protection and area
team tests cover the other resolver callers; this is not a complete native
spatial-query or area-expansion proof.
