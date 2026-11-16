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
- An account scores at most 1000 points per honor day, shared by all its characters (`platform/jobhonor`).
- The rates are an inference recorded in `jobhonor.go`: one good level-cap run reaches the run cap, and five runs reach the day's cap.

Later slices of #3530: the hunter's escort damage on arrival, the job kill decay per victim, the hourly hot route, the ambush, the tier's Academy honor buff and coloured title, and the in-game window.

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
