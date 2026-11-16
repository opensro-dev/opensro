# Referrals

Port-only, not native. `SRO_REFERRALS` (shard setting, Nomad variable `referrals`), off by default. Off serves no
referral route and pays nothing, which is the original game.

## What a player sees

The website's account area shows the player's referral code and invite link, each invited friend's status (joined,
waiting for staff, qualified) and the Silk earned. Friends are never named: only when each joined and how far they got.
A new player signs up with `?ref=CODE` or types the code; the sign-up page names the inviter by their highest character
that is not hidden.

## Rules (game/referral/referral.go)

| Rule                 | Value    | Constant          |
| -------------------- | -------- | ----------------- |
| Qualifying level     | 30       | `QualifyingLevel` |
| Inviter reward       | 500 gift Silk | `ReferrerSilk`    |
| Invited player gift  | 250 gift Silk | `RefereeSilk`     |
| Paid invitations cap | 10       | `RewardCap`       |

The owner set these on 2026-11-16 (#3518). The website reads them from the referral route and writes no numbers of its
own.

Rewards are gift Silk. The Item Mall prices every offer in gift Silk too, but the market (opensro#3686) never takes or
pays it, so a levelled alt cannot turn its reward into gold.

## How it works

- `referral_codes` and `referrals` are created when the store opens (additive, no schema stamp), like the economy
  buckets.
- A link is written once, at sign-up: refused for an unknown code, the account's own code, an account that already
  names a referrer, or one that already has a character.
- The website marks a link held when both accounts signed in from the same connection. A held referral pays nothing
  until staff approve it.
- Every five minutes the GameWorld sweeps pending referrals. One whose invited account has a character at the
  qualifying level is paid in one store transaction: the row is marked and both wallets are credited with their
  `silk_history` rows.

## Routes (token-guarded, publicstats/referrals.go)

| Route                                    | Does                          |
| ---------------------------------------- | ----------------------------- |
| `GET /v1/accounts/{id}/referrals`        | own code, rules, invites      |
| `PUT /v1/accounts/{id}/referrer`         | link at sign-up               |
| `GET /v1/referral-codes/{code}` | whose invitation it is; the inviter's account for the site only |
| `GET /v1/referrals/held`                 | staff list of held referrals  |
| `PUT /v1/referrals/{referee}/approved`   | staff approval                |
