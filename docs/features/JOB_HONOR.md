# Job honor (port-only, not native)

`SRO_JOB_HONOR` (Nomad `job_honor`), off by default. Owner-approved spec:
opensro#3530. The later reference server has no job honor; its only honor
system is the Academy's (the native check on #3530).

## What scores (slice 1)

| Event | Points | Owner |
| --- | --- | --- |
| A trader's trade sale | its profit / 10,000 (the native sale's profit, never the goods' value) | `game/action/jobhonor.go` |
| A thief's sale at the thieves' merchant | its profit / 5,000 | the same |

- A run scores at most 250 points. A run is the sales between two loads of goods: a trader's purchase or a thief's pickup opens a new one. A trader's own pickup never does, so dropping and picking up goods can't reset the cap.
- A thief's steal needs a real loss: the thief's own account must have killed the trader, or the trader's transport, whose death scattered the goods. Goods dropped any other way (a monster's kill, a logout, another player's kill) score nothing and spend none of the victim's decay, and one such pickup spoils the whole unsold haul it joins. So a player's trader alt can't hand goods to the player's thief for free: the trader must die to the thief. Kill marks are kept in memory, so goods restored after a restart score nothing.
- A thief's steal decays per victim account (resolved from the goods' owner alias, so one account's several trader characters are one victim): that account's goods score in full the first time in an honor day, at half the second, and not at all after. The count is per thief account, so a second thief on the same account shares it. A thieves' merchant pays the goods' whole value, so without the decay a player's own trader alt could feed that player's thief. Several stacks of one load are one steal. Goods of the thief's own account are no steal and score nothing (one account can run a trader and a thief at once). A thief's sale with no haul behind it scores nothing.
- An account scores at most 1000 points per honor day, shared by all its characters (`platform/jobhonor`).
- The rates are an inference recorded in `jobhonor.go`: one good level-cap run reaches the run cap, and five runs reach the day's cap.

## What scores (slice 2)

| Event | Points | Owner |
| --- | --- | --- |
| A hunter's damage on a marked thief | applied damage / 1,000, paid when the attacked caravan's trader sells | `game/action/jobhonor.go` |
| A job kill with goods or points at stake | 20, then 10 against the same victim account that day, then 0 | the same |

- A dressed thief's landed hit on a dressed trader (or its trade transport) marks the thief for 120 seconds; a dressed hunter's applied damage on the marked thief accrues against that caravan and pays out, at most 250 per hunter, when the trader next sells trade goods. Standing near a caravan scores nothing, and damage outside the window scores nothing. The escrow is memory-only: a restart forgets unpaid damage.
- A job kill stakes when the victim carried trade goods (on its transport or in its bag) or kept honor points today. An account never scores off itself, and both killer and victim must be dressed. The rates are the same recorded inference: a kill is worth a fifth of a run's cap, a fought-off robbery like a small run.

## What scores (slice 3): the hourly hot route

- Each UTC hour names one hot route: an unordered pair of distinct specialty towns (Jangan, Donwhang, Hotan, Samarkand, Constantinople - ten pairs). The pair is a splitmix64 of the hour index, so every division and every restart agrees with no stored state, and the announcement and the scorer read the same selector (`game/action/hotroute.go`).
- At the hour's first sight - and each time the hour turns - every entered player gets one server notice (0x3667): the hour's towns and the x1.5 rate.
- On the route, a trader's foreign-sale points and the escort payout that sale releases are counted at 150%, still under the 250-point run cap and the account's daily cap. A thief's den sale ends at no town pair and a job kill is tied to no route, so neither is multiplied (inference from the issue's "traders reroute, thieves and hunters follow").

## The tier's honor buff

- A tier grants, for the day, the native Academy honor buff of that tier: Silver the copper+silver set, Gold adds the gold buff, King the full crown set (the 20160..20163 skills the Training Camp honor already uses). No gold, no items.
- The buff is the dressed job's honor: the tier a character holds for the job it is wearing. Another job's suit - or no suit - carries no job buff (inference: tiers are ranked per job, so the grant belongs to that job's suit).
- One reconcile owns the buff set for both honors: the camp's native rank and the job tier's rank fold together, strongest wins, so a camp master keeps the stronger of the two and neither lane strips the other's grant. A convergence pass (first sight of a session, a dress change, the 05:00 change, and a 15-second sweep) keeps every entered character's buffs on its current want.

## The coloured title

- A tier holder wears its tier's word as a coloured title on the name board, in front of the name: Silver in pale silver, Gold in gold, King in amber - the word the Job Honor window's `tierName` speaks, coloured by its metal.
- The server pushes a tier roster (wire 0x3FC7) to the division: every entered character holding a tier for the job it is dressed as, rebuilt on a 5-second sweep and sent when the bytes change. The nameplate matches by character name, the way the Contributor roster (0x3FC1) does, so the title shows in the suit whose tier earned it and comes off with it.
- A malformed roster can only cost the roster: the client lane decodes contained and records a degradation, never the session.

## The ambush

- A run - goods bought, or a thief's fresh haul - rolls a one-in-ten chance of an NPC bandit ambush when it opens (inference: the issue's "small chance" gives no number). The roll has its own domain, so the native bandit draws fall exactly where they fall natively.
- The ambush meets the caravan on the road: at the caravan timer's fire - the carrier alive, its vehicle loaded, standing in a battlefield region, the native pack's own gates - two more bandits of the caravan's own tables spawn beside the native pack and must be fought off. They are ordinary monsters of that fight: if they kill the carrier or its vehicle, the cargo is lost the native way (the death drop). The ledger pays nothing for them: the ambush is a run's risk, never a score.
- The mark fires once per run; a run whose cargo sold or emptied first never meets its bandits.

## Days and tiers

- An honor day opens at 05:00 UTC (server time). The daily cap and the tier change share that boundary.
- At each boundary the last 7 honor days are ranked per job: #1 is King, the top 10% Gold and the top 30% Silver (each rounded up). The tiers are written as one snapshot.
- Ties break by character id, so the order never reshuffles between reads.

## Storage

`job-honor.db` sits beside the shard's authority state. Points are kept in memory and flushed every minute and at shutdown, so a stuck disk never stalls a trade sale. Points and snapshots are kept for 35 days.

## Public board

`GET /public/v1/leaderboards/job-honor?job=all|trader|thief|hunter&period=current|previous`

- **current** is the window the next change ranks: each row has its tier now (`tier`) and after tonight's change (`tonight`).
- **previous** is the snapshot that set today's tiers.
- Rows name the **job alias**, never the character: the alias is what keeps a thief or hunter anonymous, as in the native job ranking and the seal board. Public-hidden characters and characters with no alias have no row.
- Off, the route answers 404.

## In-game window

The Job Honor Rank window (Experimental row "Job honor ranking", off by default; System-menu entry while on) reads a snapshot feed the server sends only with `SRO_JOB_HONOR` on: both periods' boards (the same composition as the public board, capped at 256 rows) plus the viewer's own standing - place, tiers, the account's cap left and its kept points today and this month per job (`game/honorfeed`, wire 0x3FC6). Tabs All, Trader and Thief re-run the board's composition over one job; Current and Previous switch periods; a viewer with no visible row reads "You are not in this ranking." Off on either side, nothing is sent and the window never opens.
