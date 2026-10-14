/*
===========================================================================

tabs.go - a shop tab's offers by its authored name

Port-only rules that read one authored tab (STORE_<town>_SPECIAL_TAB1 for
the trade-tier routes) look it up here instead of reaching into the
catalogue's name index.

===========================================================================
*/
package commerce

/*
================
Catalog.TabOffers

The offers of the tab refshopgoods names, or nil when no such tab loaded.
================
*/
func (c *Catalog) TabOffers(name string) []Offer {
	id, found := c.tabIDs[name]
	if !found {
		return nil
	}
	return c.Tabs[id]
}
