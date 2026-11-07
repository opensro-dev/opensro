/*
===========================================================================

clusters_test.go - the clustering rules on the four pinned cases

NAT (many strangers, one address: no merge), a shared household (one
address, different clients: no merge), the same device (merge, with
its evidence) and a VPN exit (weak) are the cases opensro#2717 pins,
plus the network merge, the chain rule and the sort.

===========================================================================
*/
package census

import (
	"fmt"
	"testing"
)

// findLink returns the pair's link in a list, or false.
func findLink(links []Link, a, b string) (Link, bool) {
	for _, link := range links {
		if len(link.Accounts) == 2 && link.Accounts[0] == a && link.Accounts[1] == b {
			return link, true
		}
	}
	return Link{}, false
}

// evidenceKinds is a link's evidence kinds in order.
func evidenceKinds(link Link) []string {
	kinds := []string{}
	for _, evidence := range link.Evidence {
		kinds = append(kinds, evidence.Kind)
	}
	return kinds
}

/*
================
TestNATNeverMerges

Forty strangers behind one address (a carrier NAT) with forty
clients: no clusters, and every one of them is their own person.
================
*/
func TestNATNeverMerges(t *testing.T) {
	now := nowMs()
	spans := []Span{}
	for i := 0; i < 40; i++ {
		account := fmt.Sprintf("acct-%02d", i)
		spans = append(spans, span("s-"+account, account, "Hero"+account,
			now-2*hourMs, now-hourMs, "ip-nat", "net-nat", "client-"+account))
	}
	service := testService(spans, nil)
	clusters, err := service.Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 0 {
		t.Fatalf("a NAT merged strangers: %+v", clusters.Clusters)
	}
	if !clusters.PossibleTruncated {
		t.Fatal("the NAT's pair flood should report itself truncated")
	}
	uniques, err := service.Uniques()
	if err != nil {
		t.Fatal(err)
	}
	if got := uniques.Windows["24h"]; got.People != 40 || got.Accounts != 40 {
		t.Fatalf("24h uniques over a NAT: got %+v, want 40 people of 40 accounts", got)
	}
}

/*
================
TestHouseholdDoesNotMerge

Two accounts on one address with different clients (a household):
an address alone is one signal and never merges. The pair stays a
low possible link carrying the address's fanout.
================
*/
func TestHouseholdDoesNotMerge(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-3*hourMs, now, "ip-home", "net-home", "client-a"),
		span("s2", "acct-b", "Bert", now-2*hourMs, now, "ip-home", "net-home", "client-b"),
	}
	clusters, err := testService(spans, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 0 {
		t.Fatalf("a household merged: %+v", clusters.Clusters)
	}
	link, found := findLink(clusters.PossibleLinks, "acct-a", "acct-b")
	if !found {
		t.Fatal("the household pair is not even a possible link")
	}
	if link.Merged || link.Confidence != "low" {
		t.Fatalf("household link: got merged=%v confidence=%q, want unmerged low", link.Merged, link.Confidence)
	}
	if kinds := evidenceKinds(link); len(kinds) != 1 || kinds[0] != "shared-address" {
		t.Fatalf("household evidence: got %v, want only shared-address", kinds)
	}
	if link.Evidence[0].AccountsSharing != 2 {
		t.Fatalf("household fanout: got %d, want 2", link.Evidence[0].AccountsSharing)
	}
}

/*
================
TestSameDeviceMerges

One address, one client, two accounts: the same device. The merge
carries both evidence kinds and high confidence.
================
*/
func TestSameDeviceMerges(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-3*hourMs, now, "ip-home", "net-home", "client-same"),
		span("s2", "acct-b", "Bert", now-2*hourMs, now, "ip-home", "net-home", "client-same"),
	}
	clusters, err := testService(spans, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 1 {
		t.Fatalf("the same device did not merge: %+v", clusters)
	}
	cluster := clusters.Clusters[0]
	if len(cluster.Accounts) != 2 || cluster.Confidence != "high" {
		t.Fatalf("device cluster: got %+v, want two accounts at high confidence", cluster)
	}
	if len(cluster.Links) != 1 {
		t.Fatalf("device cluster links: got %d, want 1", len(cluster.Links))
	}
	link := cluster.Links[0]
	if !link.Merged || link.Confidence != "high" {
		t.Fatalf("device link: got merged=%v confidence=%q, want merged high", link.Merged, link.Confidence)
	}
	kinds := evidenceKinds(link)
	if len(kinds) != 2 || kinds[0] != "shared-address" || kinds[1] != "same-client" {
		t.Fatalf("device evidence: got %v, want shared-address then same-client", kinds)
	}
}

/*
================
TestVPNExitIsWeak

Twenty accounts out of one VPN exit; two of them even share a
client. Past the fanout bound the address is infrastructure: nothing
merges, and the pair that shares a client stays a low possible link.
================
*/
func TestVPNExitIsWeak(t *testing.T) {
	now := nowMs()
	spans := []Span{}
	for i := 0; i < 20; i++ {
		account := fmt.Sprintf("acct-%02d", i)
		client := "client-" + account
		if i < 2 {
			client = "client-shared"
		}
		spans = append(spans, span("s-"+account, account, "Hero"+account,
			now-2*hourMs, now-hourMs, "ip-vpn", "net-vpn", client))
	}
	clusters, err := testService(spans, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 0 {
		t.Fatalf("a VPN exit merged strangers: %+v", clusters.Clusters)
	}
	link, found := findLink(clusters.PossibleLinks, "acct-00", "acct-01")
	if !found {
		t.Fatal("the VPN pair sharing a client is not a possible link")
	}
	if link.Merged || link.Confidence != "low" {
		t.Fatalf("VPN link: got merged=%v confidence=%q, want unmerged low", link.Merged, link.Confidence)
	}
	if link.Evidence[0].AccountsSharing != 20 {
		t.Fatalf("VPN fanout: got %d, want 20", link.Evidence[0].AccountsSharing)
	}
}

/*
================
TestNetworkPlusClientMerges

Different addresses on one network with one client (a dynamic
address, one device): the network-plus-second-signal merge, medium.
================
*/
func TestNetworkPlusClientMerges(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-9*hourMs, now-8*hourMs, "ip-a", "net-one", "client-same"),
		span("s2", "acct-b", "Bert", now-2*hourMs, now-hourMs, "ip-b", "net-one", "client-same"),
	}
	clusters, err := testService(spans, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 1 {
		t.Fatalf("network plus client did not merge: %+v", clusters)
	}
	link := clusters.Clusters[0].Links[0]
	if link.Confidence != "medium" {
		t.Fatalf("network link confidence: got %q, want medium", link.Confidence)
	}
	kinds := evidenceKinds(link)
	if len(kinds) != 2 || kinds[0] != "shared-network" || kinds[1] != "same-client" {
		t.Fatalf("network evidence: got %v, want shared-network then same-client", kinds)
	}
}

/*
================
TestAddressFarApartIsNotShared

The same address five days apart is a reassignment, not a sharing:
no address evidence. With different clients the pair earns nothing
at all - the network alone is one signal.
================
*/
func TestAddressFarApartIsNotShared(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-5*24*hourMs, now-5*24*hourMs+hourMs, "ip-dyn", "net-dyn", "client-a"),
		span("s2", "acct-b", "Bert", now-2*hourMs, now-hourMs, "ip-dyn", "net-dyn", "client-b"),
	}
	clusters, err := testService(spans, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 0 {
		t.Fatalf("a reassigned address merged: %+v", clusters.Clusters)
	}
	link, found := findLink(clusters.PossibleLinks, "acct-a", "acct-b")
	if found {
		for _, kind := range evidenceKinds(link) {
			if kind == "shared-address" {
				t.Fatalf("an address five days apart counted as shared: %+v", link)
			}
		}
	}
}

/*
================
TestClusterChainAndSort

A merge chain is one cluster at its weakest link's confidence, and
clusters sort largest first.
================
*/
func TestClusterChainAndSort(t *testing.T) {
	now := nowMs()
	spans := []Span{
		// The chain: a-b on one device (high), b-c network+client (medium).
		span("s1", "acct-a", "Anna", now-3*hourMs, now, "ip-1", "net-1", "client-x"),
		span("s2", "acct-b", "Bert", now-3*hourMs, now, "ip-1", "net-2", "client-x"),
		span("s3", "acct-b", "Bert", now-2*hourMs, now, "ip-3", "net-3", "client-y"),
		span("s4", "acct-c", "Cora", now-2*hourMs, now, "ip-4", "net-3", "client-y"),
		// A smaller device pair.
		span("s5", "acct-d", "Dana", now-hourMs, now, "ip-5", "net-5", "client-z"),
		span("s6", "acct-e", "Elif", now-hourMs, now, "ip-5", "net-5", "client-z"),
	}
	clusters, err := testService(spans, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters.Clusters) != 2 {
		t.Fatalf("clusters: got %d, want 2 (%+v)", len(clusters.Clusters), clusters.Clusters)
	}
	big, small := clusters.Clusters[0], clusters.Clusters[1]
	if len(big.Accounts) != 3 || len(small.Accounts) != 2 {
		t.Fatalf("sort: got sizes %d then %d, want 3 then 2", len(big.Accounts), len(small.Accounts))
	}
	if big.Confidence != "medium" {
		t.Fatalf("chain confidence: got %q, want medium (the weakest merged link)", big.Confidence)
	}
	if len(big.Links) != 2 {
		t.Fatalf("chain links: got %d, want 2", len(big.Links))
	}
}
