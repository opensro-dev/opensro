# Companion victim owner protection

Bounded follow-up to issue #219. This fixes ongoing companion combat and the
shared target resolver; it does not establish the complete AI callback graph.

## Native evidence

The rules below were read from the original machine code. Raw instructions
confirm that the common attack validator, after checking a COS target,
loads the target's owner, reads the owner's body mode, and compares
against 2, 3, and 4. Each match branches to refusal. The PC permission
validator also checks a COS before normalizing to its owner and checking
that owner.

The disassembly is retained in the research evidence.

## Port gap and coverage

`resolvePetCombatTarget` checked owner body protection when a player GID
resolved to a player or their mount, but its direct COS branch checked only
the companion's body. A prepared attack targeting a companion could remain
active when its owner entered protection.

The direct COS branch now applies the existing `petAttackBodyAllowed` helper
to the owner snapshot. Ongoing combat and area candidate selection use this
resolver; direct command admission through it is the separate change in PR
#491. Target identity and damage routing remain with the companion, and the
existing cancellation owner closes a prepared cast when target resolution fails.

The behavioral regression seeds an admitted AI target independently of command
routing, prepares a real cast against an initially unprotected companion, then
changes its owner's body mode. Modes 2, 3 and 4 retire combat, preserve the
companion's HP beyond the release deadline, and emit exactly one finalize.
Modes 0, 1 and 5 retain the prepared token. The unmodified production resolver
fails the three protected cases. Existing player-target protection and area
team tests cover the other resolver callers; this is not a complete native
spatial-query or area-expansion proof.
