# Discord audit and small UI/text repairs

All forty tracked reports were reviewed against their recorded evidence,
recent human thread replies (including forwarded snapshots), PR status and
the production client symlink. Production still serves `ff97dedf`; PR #26
is open. No unpublished repair was marked verified or closed.

The review posted one conditional, idempotent update to every report through
the bot's `/bugs/review` owner. Final audit states: nineteen In progress,
nine Ready to test, seven Needs information, four New and one Closed.
BUG-028 retains the staff closure. BUG-037 was corrected from Ready to test
to In progress because its completed implementation is only available locally.
BUG-036 and BUG-038 remain open umbrella reports as explicitly requested.

## BUG-012: matching filter controls

The name and level input borders were present in the asset catalog but
covered by the enclosing background. The UI flattened separate resource
creation calls, then sorted frames by control type. This lost native layer
ownership and drew later background tiles over the inset controls.

Client instructions at `637400` load `ifpartymatch.txt` and create `Create`,
`SearchInfo`, `SlotListButton`, then `SlotList` through `783F80`. Admission now
uses that order through the shared authored-section mechanism. Rendering
uses the resulting paint order without a second type-based sort. Separate
registration/join/auto dialogs are not admitted into the main page.

The native initialization at `6374CF` also installs the `~` level separator;
that caption is now restored. Authored gold filter-label colors remain intact.
The constructor was unlabeled; it is now `CIFPartyMatch_OnCreate`, saved and
verified in client database snapshot 196. No exposed unnamed helper remains.

The UI regression checks actual published quads: each input border must be
above its containing background, the range separator must render, and the
authored gold caption color must survive. The existing Chrome matching test
passed before and after; screenshots show the reproduced missing borders
and the repaired native controls. The behavioral regression passed 5/5.

## BUG-036: five verified v1.150 text corrections

The supplied [SROquests guide](https://github.com/AlighieriDemiurgs/SROquests)
was consulted first. It explicitly uses v1.188; actual corrections were then
checked against the v1.150 text rows. No quest count, reward or SP formula was
copied from the later version.

| Source | Corrected English |
| --- | --- |
| `Solder Sangnam [Teleport]` | `Soldier Sangnam [Teleport]` |
| `Karakoram South Sock` | `Karakoram South Dock` |
| `Blood Devil 's leaf` | `Blood Devil's leaf` |
| `Collect  Purification Seed (%d)` | `Collect Purification Seed (%d)` |
| `Collect  Purification Fruit (%d)` | `Collect Purification Fruit (%d)` |

The Korean Karakoram row means southern landing/dock. The reported Broken
Sheild, Niya Soldier double-space, storage capitalization and later quest
count examples were not established as matching defects in the inspected
v1.150 English rows. They are not claimed fixed by this pass.

Corrections live in a shared build policy keyed by source file, symbol and
original English. Both client catalogs/quest data and server text projection
apply it. Unexpected new wording fails as stale instead of silently receiving
the old correction; already-corrected projection is idempotent. Extracted
retail data and native oracle captures are unchanged. Regression tests cover
all five cells, UTF-8/UTF-16 projection, other language columns, record framing,
format placeholders, idempotence and unrelated symbols.

## Validation and release

Final source checks passed all nine gates in 70.0 seconds, including the full
server gate. Focused English tests passed 2/2; combined UI/service regressions
passed 8/8; the Chrome matching test and `verify:quick` passed. The new test's
ES2023 `findLastIndex` call was replaced with a compatible loop; the final
test-type gate passed with no new debt.

The full client run passed 1969/1970 tests. Its remaining behavioral failure
is `Unbound published item /assets/char/weapon/eu_m_darkstaff_01.glb`, the
shared generated-asset/source mismatch from concurrent character-material
work. Those files are preserved. Nine non-test-type gates passed in that
run; test types subsequently passed after the assertion correction. This is
not a complete passing client gate.

Corrected browser name/quest catalogs and their packed delivery were rebuilt
under the asset lock. Final `verify:delivery` passed. A full server projection
and archive were built and checked for all five corrected cells. That
development candidate also differs from the running old bundle in 495 world
navigation JSON files and previous English completions, so it was not
activated as a text-only local deployment. The existing healthy server stays
on its previous data artifact; the shared correction code is ready for the
next reviewed server-data build. Production publication remains separate.
