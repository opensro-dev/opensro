/*
===========================================================================

events.go - C5 events.json: the timed events the shard runs

An event is shown only while the shard runs it: the entry carries the
switch's state and the configured window, so a page names the event's
terms before, during and after it runs, and nothing when it is off or has
no usable window. The rows come from the same progression readers the
GameWorld runs (halloween.go), so the page cannot promise a rate the
server does not pay.

===========================================================================
*/
package catalogue

import (
	"time"

	"opensro.online/server/internal/game/progression"
)

/*
================
EventDrop

One kill's event drop: the item and its per-kill chance in percent.
================
*/
type EventDrop struct {
	ItemID uint32 `json:"itemId"`
	// Codename is the drop's item codename, for a reader without the
	// items table.
	Codename string `json:"codename"`
	// ChancePercent is the per-kill chance in percent (5 is 5%).
	ChancePercent int `json:"chancePercent"`
	// DailyCap is how many of the item one character may mint per day
	// (0: no cap). The day resets at 04:00 UTC.
	DailyCap int `json:"dailyCap,omitempty"`
}

/*
================
EventGift

One exchange row: the item, how many of it a gift pays, its weight on the
exchange's roll, and the boxes one gift costs.
================
*/
type EventGift struct {
	ItemID uint32 `json:"itemId"`
	// Codename is the gift's item codename, for a reader without the
	// items table.
	Codename string `json:"codename"`
	// Count is how many of the item one gift pays.
	Count uint32 `json:"count"`
	// Weight is the row's weight on the exchange roll's scale; a flat-price
	// row carries no weight.
	Weight float64 `json:"weight,omitempty"`
	// BoxPrice is how many event boxes the row costs (0: a rolled gift).
	BoxPrice int `json:"boxPrice,omitempty"`
}

/*
================
EventRow

One timed event the shard runs.
================
*/
type EventRow struct {
	// ID is the event's stable catalogue key.
	ID string `json:"id"`
	// Name is the event's display name.
	Name string `json:"name"`
	// StartsAt is the window's start (UTC, RFC3339); the event runs from
	// here up to EndsAt.
	StartsAt string `json:"startsAt"`
	// EndsAt is the window's end (UTC, RFC3339); the event does not run
	// at or after here.
	EndsAt string `json:"endsAt"`
	// Active says whether the export ran inside the window.
	Active bool `json:"active"`
	// Drops are the event's per-kill drops.
	Drops []EventDrop `json:"drops,omitempty"`
	// Gifts are the exchange's rolled rows.
	Gifts []EventGift `json:"gifts,omitempty"`
	// Prizes are the exchange's flat-price rows.
	Prizes []EventGift `json:"prizes,omitempty"`
}

/*
================
EventsFile
================
*/
type EventsFile struct {
	Header
	Events []EventRow `json:"events"`
}

/*
================
BuildEvents

Every timed event the shard runs, in id order. An event switched off, or
on without a usable window, contributes no row: nothing is shown for an
event that cannot run.
================
*/
func BuildEvents(header Header, itemID func(string) uint32, now time.Time) EventsFile {
	file := EventsFile{Header: header, Events: []EventRow{}}
	if event := halloweenEventRow(itemID, now); event != nil {
		file.Events = append(file.Events, *event)
	}
	return file
}

/*
================
halloweenEventRow

The Halloween Day event's row, or nil while the shard does not run it.
The box is a per-kill drop on ordinary kills (a Gourd kill always pays
its box); the exchange pays one weighted roll per box and sells its eight
helmets at a flat box price.
================
*/
func halloweenEventRow(itemID func(string) uint32, now time.Time) *EventRow {
	event := progression.HalloweenEventFromEnv()
	if !event.Enabled() || !event.WindowValid() {
		return nil
	}
	startsAt, endsAt, valid := event.Window()
	if !valid {
		return nil
	}
	row := EventRow{ID: "halloween", Name: "Halloween Day",
		StartsAt: startsAt.Format(time.RFC3339), EndsAt: endsAt.Format(time.RFC3339),
		Active: event.ActiveAt(now)}
	if boxID := itemID(progression.HalloweenBoxCodename); boxID != 0 {
		row.Drops = []EventDrop{{ItemID: boxID, Codename: progression.HalloweenBoxCodename,
			ChancePercent: progression.HalloweenBoxDropChancePercent,
			DailyCap:      progression.HalloweenBoxDailyCap}}
	}
	for _, gift := range progression.HalloweenGifts() {
		id := itemID(gift.Codename)
		if id == 0 {
			continue
		}
		row.Gifts = append(row.Gifts, EventGift{ItemID: id, Codename: gift.Codename,
			Count: gift.Count, Weight: gift.Weight})
	}
	for _, helmet := range progression.HalloweenHelmets() {
		id := itemID(helmet.Codename)
		if id == 0 {
			continue
		}
		row.Prizes = append(row.Prizes, EventGift{ItemID: id, Codename: helmet.Codename,
			Count: 1, BoxPrice: progression.HalloweenHelmetPrice})
	}
	return &row
}
