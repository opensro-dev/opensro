## Summary

<!-- What changes and why. -->

## Player note

<!--
Put each change a player notices in a commit message of this branch as a
trailer line, "Player-note: Area: one plain sentence" (areas: UI, Party,
Combat, Items, Animation, Audio, World, Other). The squash merge keeps the
branch's commit messages, and the release's patch notes (catalogue
notes.json) are read from there word for word, so the note is reviewed
with the code and cannot change after the merge. Public, plain words: no
security details, no reporter names. Repeat the line here for reviewers,
or write "None". If this change makes an earlier pull request's note false,
add "Player-note-retract: #<pr>" too: that note is left out of the next
patch notes (an already published release's notes never change).
-->

## Old-tab

<!--
A release that keeps the browser protocol leaves every open tab on the live
client. If this change adds a byte or a new value to an existing packet,
removes a field, or changes a client decoder, put a trailer line in a commit
message of this branch, "Old-tab: safe - <reason>", saying why tabs that are
already open stay compatible; the squash merge carries it into the release
audit. If old tabs cannot stay compatible, say so here instead: the change
needs a protocol bump. Write "None" when the wire does not change.
-->

## Native claims

| Claim | Native evidence | Checked by | Status |
| --- | --- | --- | --- |

## Verification

<!-- The commands that ran, with their exit status. -->
