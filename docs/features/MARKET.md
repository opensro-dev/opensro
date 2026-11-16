# Market escrow

Port-only, not native. `SRO_CONSIGNMENT` (shard setting, Nomad variable `consignment`), off by default. Off serves no
market route and writes no market row, which is the original game: v1.150 has no consignment.

One escrow serves two front ends, the in-game window (#3529) and the website market (opensro-web#148). The design and
the owner's numbers are in the market design doc (2026-11-16).

## Rules (game/market/market.go)

| Rule                       | Value                  | Constant          |
| -------------------------- | ---------------------- | ----------------- |
| Sale fee, from the seller  | 1%, rounded down       | `FeePercent`      |
| Listing duration           | 24 hours               | `ListingDuration` |
| Open listings per account  | 20                     | `OpenListingCap`  |
| Expiry pass                | every minute, 200 max  | `ExpiryInterval`  |

No make-an-offer and no minimum price in the first version.

## Where items and money go (data/store/market.go)

| Door      | Item                                                      | Money                                                                 |
| --------- | --------------------------------------------------------- | --------------------------------------------------------------------- |
| List      | bag (game) or account warehouse (site) -> listing          | none                                                                  |
| Buy       | listing -> the buyer's warehouse                          | Silk: buyer's purchased Silk -> seller's Silk. Gold: a buyer's character -> seller's warehouse gold |
| Cancel    | listing -> seller's warehouse, or held while it is full   | none                                                                  |
| Expire    | the same as cancel                                        | none                                                                  |
| Take back | held listing -> seller's warehouse                        | none                                                                  |

- Every door is one store transaction: the item leaves its source and reaches its destination together with the money,
  or nothing changes. `market_commitfail_test.go` fails every door at its commit, after all of its writes, and checks
  that every table a door writes and every live record is unchanged.
- A buy names the listing's version. A listing that changed, sold or expired meanwhile refuses the buy and charges
  nothing.
- Gold proceeds go to the seller's account warehouse, never onto a character, because an online character's record
  belongs to its action lane.
- A gold buy from the website must name an offline character. The buy runs under the gate world entry admits under
  (`action.Runtime.WhileOffline`), so an entry either comes first and the buy answers `online`, or follows and reads the
  paid gold. The published session list is display only: it can lag an entry.
- Gift Silk never pays and never receives, so gift Silk cannot become gold.
- Every sale is a `market_sales` row naming both accounts, and a gold sale is an economy ledger row (`market_sale`,
  a transfer).

## Website routes (token-guarded, publicstats/market.go and game/market/service.go)

| Route                                              | Does                                                                   |
| -------------------------------------------------- | ---------------------------------------------------------------------- |
| `GET /v1/market/categories`                        | the category picker (`slug`, `name`)                                   |
| `GET /v1/market/listings`                          | search: `q`, `category`, `degree` 1-11, `currency`, `sort`, `page` (15 a page, 1-100) |
| `GET /v1/accounts/{id}/market`                     | own listings, purchases, warehouse, characters, Silk, rules            |
| `POST /v1/accounts/{id}/market/list`               | `{slot, currency, price}` from the warehouse -> 201                    |
| `POST /v1/accounts/{id}/market/buy`                | `{listing, version, character}` -> 201                                 |
| `POST /v1/accounts/{id}/market/{listing}/cancel`   | -> 204                                                                 |
| `POST /v1/accounts/{id}/market/{listing}/take-back`| -> 204                                                                 |

- An unknown search value is 400, never an empty page.
- A refusal answers `{"error": code}`: `not-found`, `no-item`, `no-character`, `changed`, `online`, `warehouse-full`,
  `not-held`, `cap`, `own-listing`, `not-tradeable`, `funds`, `price` or `unavailable`. None charges anything.
- The website lists only from the warehouse and pays gold only from an offline character (`online` otherwise).
- A listing never names its seller.
- Categories come from the item TypeIDs (`market.Category`); the item name, degree and trade rule from the item
  references and `action.Runtime.Tradeable`, the exchange's own rule.

## Tables

`market_listings` and `market_sales` are created when the store opens (additive, no schema stamp), like the economy
buckets. A listing keeps its item after it is sold or returned, as the record of what moved.

An open or held listing is a container: an offline item tool that rewrites stored items across the shard (such as
`sro-swap-elixir`) has to cover `market_listings` too once the market is on.
