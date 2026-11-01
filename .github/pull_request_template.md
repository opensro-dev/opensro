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

## Old tab

<!--
Does an open browser tab on the live client still cope with this change?
A protocol-keeping release refuses a commit that changes what a tab
receives or decodes (server encoders and frames, the session worker, the
foundation/gameplay decoders, other server changes under internal/game,
internal/transport or internal/agent) unless its squash commit carries
"Old-tab: safe - <reason>". Put that trailer in a commit message of this
branch and repeat it here, write "Protocol bump" when old tabs cannot cope,
or "Not affected". See .github/CONTRIBUTING.md section 6.
-->

## Native claims

| Claim | Native evidence | Checked by | Status |
| --- | --- | --- | --- |

## Verification

<!-- The commands that ran, with their exit status. -->
