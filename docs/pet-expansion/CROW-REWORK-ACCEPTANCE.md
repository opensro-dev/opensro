# Crow support rework and acceptance

The rework is port-only and remains behind `SRO_ATTACK_PET_EXPANSION`.
With the flag off, native pet care and combat remain unchanged. Higher books
retain earlier effects; existing book IDs, learned ranks and gear bonus IDs
are preserved. Values reach their maximum at the final rank available at pet 90.

| Family | Behavior |
| --- | --- |
| Rejuvenating Plume | Heals 10–30% maximum HP, half immediately and half over five pulses. Base cooldown 20–15 seconds. II also heals the crow and cleanses negative effects; III retains these and reaches nearby party members. |
| Stormbrood | Recharge 20–15 minutes. Hatched clones inherit gear blues but no learned skills and cannot heal. II adds stored charges; III retains the death burst. Ten live eggs/clones maximum. |
| Mana Spring | Restores 10–30% of actual crow damage as owner MP; II adds party recovery. III deals 5,000 true monster damage within 5 m per 10,000 actual MP restored, carrying excess progress. Full MP grants no progress. Replaces the former cooldown recovery. |
| Tailwind | One stack every five seconds, ten maximum; full-stack movement increases 20–50%. Outgoing actions do not clear it. II intercepts an incoming monster hit with a stun at full stacks, retaining stacks, once per recipient per 30 seconds. III extends this to party members. |
| Raven's Refuge | Replaces Guiding Feathers at IDs 58–60. Below a scaling 20–40% HP threshold, take 15% less monster damage. II heals 10% maximum HP on a surviving downward threshold crossing, with a 300–120 second progression cooldown. III extends both effects to party members within 20 m with independent cooldowns. |

Crow normal attacks now use the magic profile at 70% of the penguin profile,
before equipment and skills. Precision's former accuracy/skill-damage bonus is
removed. The recipient buff shows Refuge protection instead of Precision stacks.

## Raven's Refuge live cooldown checks

Completed with real elapsed time in a disposable database copy. No game clock
or active cooldown deadline was edited. Real gameplay commands learned the
next books, formed a party and used ordinary HP potions. A controlled monster
provided the incoming hits that training dummies cannot produce.

| Check | Result |
| --- | --- |
| Owner II rank 1 | Threshold heal started the expected 237.931-second cooldown. |
| Upgrade owner to III rank 10 | Existing deadline retained; upgrading did not reset or shorten it. |
| Party III rank 10 | Its heal started an independent 120-second cooldown. |
| First full waits | Both recipients healed again after their complete cooldowns elapsed. |
| Early owner retry | A new downward crossing about 73 seconds before expiry produced no heal and did not change the deadline. |
| Early party retry | A new downward crossing about 62 seconds before expiry produced no heal and did not change its deadline. |
| Another full 120-second cycle | Both recipients healed again after expiry. Their timers did not reset one another. |

Both subjects survived these completed cooldown trials. Protection remained
active below the threshold while healing was unavailable. One preliminary
attempt remained above the threshold and was not counted as a rejection.
These are natural expiry checks, not claims of landing a hit at the exact
expiry millisecond; automated boundary tests cover that edge. Book I has no
emergency heal, so II rank 1 is the first heal-capable progression point.

Earlier live checks also covered owner/party Plume pulses, Mana Spring's
restoration and threshold burst, cloned gear damage, Tailwind's pre-hit stun,
natural clone expiry and Guardian's Bond recovery. They do not establish that
every rank, item combination or visual was replayed on the final PR head.

## Automated coverage and release notes

Focused tests cover cumulative books, rank caps, direct and periodic damage,
strict HP threshold crossings, fatal and absorbed hits, independent recipients,
persisted recovery deadlines, flag-off behavior and updated HUD/descriptions.
Generated client design data is checked against its server source.

Raven's saved recipient cooldown requires the schema 43 to 44 offline store upgrade, even with pets disabled;
back up databases and stop writers before upgrading. Do not lower schema
metadata or bypass version readers. Live gameplay evidence predates the final
main integration; final source and migration checks are reported on the PR.

Tests used copies only. Temporary test access and protection were removed,
the original server state was restored, and existing character progression,
inventory, pet training and equipment were checked for preservation. Database
copies, generated assets, recordings and machine-specific operator notes are
not distributed with this change.

After integrating main, all 88 focused client tests and the client quick gate passed.
The server changed-scope checks passed except local race execution, which requires
a CGO compiler unavailable on this host; GitHub CI must confirm the race result.
