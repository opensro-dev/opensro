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
In a protocol-keeping release, a commit that changes what a tab
receives or decodes (server encoders and frames, the session worker, the
foundation/gameplay decoders, other server changes under internal/game,
internal/transport or internal/agent) carries
"Old-tab: safe - <reason>" on its squash commit. Put that trailer in a commit message of this
branch and repeat it here, write "Protocol bump" when old tabs cannot cope,
or "Not affected". See .github/CONTRIBUTING.md section 6.
-->

## Evidence

<!--
Tick each line once it is done and shown in this PR, or end the line with
"n/a - <reason>" (a console-only change has no native trace, for example).
Keep each line's first word: the work board reads them.
-->

- [ ] rg: blast radius pasted
- [ ] callers: every caller and test of each changed function mapped
- [ ] edge cases: mapped, each with a test or a reason
- [ ] native client: traced in the v1.150 client
- [ ] native server: traced in the v1.188 server binary
- [ ] subs: no unlabelled subs left in what was read

## Native claims

| Claim | Native evidence | Checked by | Status |
| --- | --- | --- | --- |

## Verification

<!-- The commands that ran, with their exit status. -->
