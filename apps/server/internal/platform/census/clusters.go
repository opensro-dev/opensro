/*
===========================================================================

clusters.go - accounts into probable people, with the evidence shown

Accounts are nodes; a pair earns evidence, never a verdict:

  - shared-address: both accounts entered from the same address hash
    with spans overlapping or within a day. Addresses are reassigned,
    so a month apart is not sharing. The evidence carries how many
    distinct accounts that address saw: the same fact is one device at
    a household router and nothing at all at a carrier NAT.
  - shared-network: the same /24 (IPv4) or /48 (IPv6) hash, counted
    the same way.
  - same-client: both accounts presented the same client fingerprint.
    A fingerprint alone is no link at all - one browser build is shared
    by thousands - so it is only ever recorded beside an address or
    network fact.

A pair merges into one person only on two independent signals:
address plus client, or network plus client. An address alone never
merges, at any fanout: a household is not one person, and a NAT is
not a crowd of one. An address shared by more than
addressFanoutMergeMax accounts is infrastructure (a VPN exit, a
CGNAT): it merges nothing, even with a second signal, and the pair
stays a low-confidence possible link. Confidence is high for
address-plus-client at household fanout, medium for the other
merges, low for everything unmerged - and a cluster is only as
confident as its weakest merged link.

===========================================================================
*/
package census

import (
	"sort"
	"strings"
)

const (
	// addressWindowMs bounds "shared an address", above.
	addressWindowMs = windowDayMs
	// pairGroupMax bounds pair enumeration inside one hash group: a
	// group past it is a carrier NAT or a VPN exit, whose pairs say
	// nothing worth enumerating.
	pairGroupMax = 128
	// addressFanoutHousehold: at or under this many accounts on one
	// address, address plus client reads as one device (high).
	addressFanoutHousehold = 4
	// addressFanoutMergeMax: past this many accounts an address is
	// infrastructure and merges nothing.
	addressFanoutMergeMax = 16
	// networkFanoutMergeMax: past this many accounts a network is
	// infrastructure too and merges nothing on its own signal.
	networkFanoutMergeMax = 64
	// possibleLinksMax caps the unmerged pairs one answer lists.
	possibleLinksMax = 200
	// personHistoryMax caps one person view's spans.
	personHistoryMax = 200
)

/*
================
Evidence

One shared fact between two accounts. AccountsSharing counts the
distinct accounts the fact was seen with (an address's fanout), so
staff read the weight, not a hash: no hash value ever appears here.
================
*/
type Evidence struct {
	Kind            string `json:"kind"`
	AccountsSharing int    `json:"accountsSharing,omitempty"`
}

/*
================
Link

The evidence between two accounts and what it earned: a merge into
one person, or a possible link staff may weigh themselves.
================
*/
type Link struct {
	Accounts   []string   `json:"accounts"`
	Evidence   []Evidence `json:"evidence"`
	Confidence string     `json:"confidence"`
	Merged     bool       `json:"merged"`
}

/*
================
Cluster

One probable person: two or more accounts, their characters, and
the merged links that joined them.
================
*/
type Cluster struct {
	ID         string   `json:"id"`
	Accounts   []string `json:"accounts"`
	Characters []string `json:"characters"`
	Confidence string   `json:"confidence"`
	Links      []Link   `json:"links"`
}

/*
================
Clusters

Every cluster of two or more accounts, largest first, plus the
strongest unmerged pairs as possible links.
================
*/
type Clusters struct {
	SchemaVersion     int       `json:"schemaVersion"`
	GeneratedAt       int64     `json:"generatedAt"`
	Clusters          []Cluster `json:"clusters"`
	PossibleLinks     []Link    `json:"possibleLinks"`
	PossibleTruncated bool      `json:"possibleTruncated"`
}

/*
================
pairEvidence

The shared facts of one account pair while the index is built.
================
*/
type pairEvidence struct {
	a, b           string
	address        bool
	addressSharing int
	network        bool
	networkSharing int
	client         bool
	clientSharing  int
}

// link renders the pair as the API's Link, with its confidence and
// merge verdict (see the file banner for the rules).
func (p *pairEvidence) link() Link {
	link := Link{Accounts: []string{p.a, p.b}, Evidence: []Evidence{}}
	if p.address {
		link.Evidence = append(link.Evidence, Evidence{Kind: "shared-address", AccountsSharing: p.addressSharing})
	}
	if p.network && !p.address {
		link.Evidence = append(link.Evidence, Evidence{Kind: "shared-network", AccountsSharing: p.networkSharing})
	}
	if p.client {
		link.Evidence = append(link.Evidence, Evidence{Kind: "same-client", AccountsSharing: p.clientSharing})
	}
	link.Confidence, link.Merged = p.verdict()
	return link
}

// verdict is the pair's confidence and whether it merges.
func (p *pairEvidence) verdict() (string, bool) {
	switch {
	case p.address && p.addressSharing > addressFanoutMergeMax:
		// Infrastructure: a VPN exit or CGNAT merges nothing.
		return "low", false
	case p.address && p.client && p.addressSharing <= addressFanoutHousehold:
		return "high", true
	case p.address && p.client:
		return "medium", true
	case p.network && p.client && p.networkSharing <= networkFanoutMergeMax:
		return "medium", true
	default:
		return "low", false
	}
}

/*
================
accountFacts

What one account did across the snapshot.
================
*/
type accountFacts struct {
	characters map[string]string // folded name -> name as stored
	clients    map[string]bool
	spans      []Span
}

/*
================
index

The snapshot's spans with every derived answer structure: account
facts, pair evidence, clusters, and each account's person key.
================
*/
type index struct {
	spans         []Span
	now           int64
	accounts      map[string]*accountFacts
	clusters      []Cluster
	possible      []Link
	possibleCount int
	personOf      map[string]string
}

// openSpans is the spans still entered at now.
func (idx *index) openSpans(now int64) []Span {
	var open []Span
	for _, span := range idx.spans {
		if span.Ended == 0 {
			open = append(open, span)
		}
	}
	return open
}

// activeSpans is the spans entered at instant t: a span still open
// is entered at every instant from its start, now included.
func (idx *index) activeSpans(t int64) []Span {
	var active []Span
	for _, span := range idx.spans {
		if span.Started <= t && (span.Ended == 0 || t < span.Ended) {
			active = append(active, span)
		}
	}
	return active
}

// spansOverlapping is the spans touching [from, to).
func (idx *index) spansOverlapping(from, to, now int64) []Span {
	var spans []Span
	for _, span := range idx.spans {
		if span.overlaps(from, to, now) {
			spans = append(spans, span)
		}
	}
	return spans
}

// peopleCount counts distinct persons over an account set: a merged
// cluster is one person, an unmerged account is its own.
func (idx *index) peopleCount(accounts map[string]bool) int {
	people := make(map[string]bool, len(accounts))
	for account := range accounts {
		people[idx.personOf[account]] = true
	}
	return len(people)
}

/*
================
buildIndex

The whole derivation over one snapshot: account facts, pair
evidence from the address, network and client groups, then the
union-find over merged pairs.
================
*/
func buildIndex(spans []Span, now int64) *index {
	idx := &index{spans: spans, now: now, accounts: map[string]*accountFacts{}, personOf: map[string]string{}}
	byAddress := map[string]map[string][]Span{}
	byNetwork := map[string]map[string][]Span{}
	byClient := map[string]map[string]bool{}
	for _, span := range spans {
		facts := idx.accounts[span.Account]
		if facts == nil {
			facts = &accountFacts{characters: map[string]string{}, clients: map[string]bool{}}
			idx.accounts[span.Account] = facts
		}
		facts.characters[strings.ToLower(span.Character)] = span.Character
		facts.spans = append(facts.spans, span)
		if span.Client != "" {
			facts.clients[span.Client] = true
			group := byClient[span.Client]
			if group == nil {
				group = map[string]bool{}
				byClient[span.Client] = group
			}
			group[span.Account] = true
		}
		if span.IPKey != "" {
			group := byAddress[span.IPKey]
			if group == nil {
				group = map[string][]Span{}
				byAddress[span.IPKey] = group
			}
			group[span.Account] = append(group[span.Account], span)
		}
		if span.NetKey != "" {
			group := byNetwork[span.NetKey]
			if group == nil {
				group = map[string][]Span{}
				byNetwork[span.NetKey] = group
			}
			group[span.Account] = append(group[span.Account], span)
		}
	}

	pairs := map[[2]string]*pairEvidence{}
	pair := func(a, b string) *pairEvidence {
		key := [2]string{a, b}
		found := pairs[key]
		if found == nil {
			found = &pairEvidence{a: a, b: b}
			pairs[key] = found
		}
		return found
	}
	for _, group := range byAddress {
		accounts := sortedKeys(group)
		if len(accounts) > pairGroupMax {
			continue
		}
		for i := 0; i < len(accounts); i++ {
			for j := i + 1; j < len(accounts); j++ {
				if minGap(group[accounts[i]], group[accounts[j]], now) > addressWindowMs {
					continue
				}
				evidence := pair(accounts[i], accounts[j])
				evidence.address = true
				evidence.addressSharing = len(group)
			}
		}
	}
	for _, group := range byNetwork {
		accounts := sortedKeys(group)
		if len(accounts) > pairGroupMax {
			continue
		}
		for i := 0; i < len(accounts); i++ {
			for j := i + 1; j < len(accounts); j++ {
				evidence := pair(accounts[i], accounts[j])
				if !evidence.network || len(group) < evidence.networkSharing {
					evidence.network = true
					evidence.networkSharing = len(group)
				}
			}
		}
	}
	for key, evidence := range pairs {
		shared, sharing := sharedClient(idx.accounts[key[0]], idx.accounts[key[1]], byClient)
		evidence.client, evidence.clientSharing = shared, sharing
	}

	parent := map[string]string{}
	find := func(account string) string {
		root := account
		for parent[root] != "" && parent[root] != root {
			root = parent[root]
		}
		for parent[account] != "" && parent[account] != root {
			parent[account], account = root, parent[account]
		}
		return root
	}
	links := make([]Link, 0, len(pairs))
	for _, evidence := range pairs {
		link := evidence.link()
		links = append(links, link)
		if !link.Merged {
			continue
		}
		a, b := find(evidence.a), find(evidence.b)
		if a != b {
			parent[a] = b
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Accounts[0] != links[j].Accounts[0] {
			return links[i].Accounts[0] < links[j].Accounts[0]
		}
		return links[i].Accounts[1] < links[j].Accounts[1]
	})

	members := map[string][]string{}
	for account := range idx.accounts {
		root := find(account)
		members[root] = append(members[root], account)
	}
	confidenceRank := map[string]int{"low": 0, "medium": 1, "high": 2}
	for _, accounts := range members {
		sort.Strings(accounts)
		if len(accounts) < 2 {
			idx.personOf[accounts[0]] = "account:" + accounts[0]
			continue
		}
		cluster := Cluster{ID: accounts[0], Accounts: accounts, Confidence: "high", Links: []Link{}}
		inCluster := make(map[string]bool, len(accounts))
		characters := map[string]bool{}
		for _, account := range accounts {
			inCluster[account] = true
			idx.personOf[account] = "cluster:" + cluster.ID
			for _, name := range idx.accounts[account].characters {
				characters[name] = true
			}
		}
		for name := range characters {
			cluster.Characters = append(cluster.Characters, name)
		}
		sort.Strings(cluster.Characters)
		for _, link := range links {
			if link.Merged && inCluster[link.Accounts[0]] && inCluster[link.Accounts[1]] {
				cluster.Links = append(cluster.Links, link)
				if confidenceRank[link.Confidence] < confidenceRank[cluster.Confidence] {
					cluster.Confidence = link.Confidence
				}
			}
		}
		idx.clusters = append(idx.clusters, cluster)
	}
	sort.Slice(idx.clusters, func(i, j int) bool {
		if len(idx.clusters[i].Accounts) != len(idx.clusters[j].Accounts) {
			return len(idx.clusters[i].Accounts) > len(idx.clusters[j].Accounts)
		}
		return idx.clusters[i].ID < idx.clusters[j].ID
	})
	for _, link := range links {
		if link.Merged {
			continue
		}
		idx.possibleCount++
		if len(idx.possible) < possibleLinksMax {
			idx.possible = append(idx.possible, link)
		}
	}
	return idx
}

// sortedKeys returns a span group's account names in order.
func sortedKeys(group map[string][]Span) []string {
	keys := make([]string, 0, len(group))
	for key := range group {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// minGap is the smallest distance between any span of a and any of
// b: zero when they overlap.
func minGap(a, b []Span, now int64) int64 {
	var best int64 = -1
	for _, x := range a {
		for _, y := range b {
			gap := max(x.Started, y.Started) - min(x.end(now), y.end(now))
			if gap < 0 {
				gap = 0
			}
			if best < 0 || gap < best {
				best = gap
			}
		}
	}
	return best
}

// sharedClient reports whether two accounts presented a common
// client fingerprint, and how many accounts that fingerprint saw
// (the most specific shared one).
func sharedClient(a, b *accountFacts, byClient map[string]map[string]bool) (bool, int) {
	shared, sharing := false, 0
	for client := range a.clients {
		if !b.clients[client] {
			continue
		}
		if count := len(byClient[client]); !shared || count < sharing {
			shared, sharing = true, count
		}
	}
	return shared, sharing
}

/*
================
Service.Clusters
================
*/
func (s *Service) Clusters() (Clusters, error) {
	idx, err := s.snapshot()
	if err != nil {
		return Clusters{}, err
	}
	clusters := idx.clusters
	if clusters == nil {
		clusters = []Cluster{}
	}
	possible := idx.possible
	if possible == nil {
		possible = []Link{}
	}
	return Clusters{SchemaVersion: SchemaVersion, GeneratedAt: s.nowMs(),
		Clusters: clusters, PossibleLinks: possible,
		PossibleTruncated: idx.possibleCount > len(possible)}, nil
}

/*
================
Person

One character's probable person: the cluster (a solo account is a
cluster of one), the accounts with their characters and presence,
the unmerged pairs touching them, and the online history of every
account in the cluster. found is false when the character never
entered.
================
*/
type Person struct {
	SchemaVersion    int             `json:"schemaVersion"`
	GeneratedAt      int64           `json:"generatedAt"`
	Character        string          `json:"character"`
	Account          string          `json:"account"`
	Cluster          Cluster         `json:"cluster"`
	PossibleLinks    []Link          `json:"possibleLinks"`
	Accounts         []PersonAccount `json:"accounts"`
	History          []PersonSpan    `json:"history"`
	HistoryTruncated bool            `json:"historyTruncated"`
}

// PersonAccount is one account of the person.
type PersonAccount struct {
	Account    string   `json:"account"`
	Characters []string `json:"characters"`
	Online     bool     `json:"online"`
	Sessions   int      `json:"sessions"`
	FirstSeen  int64    `json:"firstSeen"`
	LastSeen   int64    `json:"lastSeen"`
}

// PersonSpan is one entered session of the person's history.
type PersonSpan struct {
	Session   string `json:"session"`
	Account   string `json:"account"`
	Character string `json:"character"`
	Transport string `json:"transport"`
	Started   int64  `json:"started"`
	Ended     int64  `json:"ended"`
	Open      bool   `json:"open"`
	Estimated bool   `json:"estimated"`
}

/*
================
Service.Person
================
*/
func (s *Service) Person(character string) (Person, bool, error) {
	idx, err := s.snapshot()
	if err != nil {
		return Person{}, false, err
	}
	now := s.nowMs()
	account, display := "", ""
	for i := len(idx.spans) - 1; i >= 0 && account == ""; i-- {
		if strings.EqualFold(idx.spans[i].Character, character) {
			account, display = idx.spans[i].Account, idx.spans[i].Character
		}
	}
	if account == "" {
		return Person{}, false, nil
	}
	// A solo account stands alone: its cluster carries no confidence,
	// because no link was ever weighed for it.
	cluster := Cluster{ID: account, Accounts: []string{account}, Links: []Link{}}
	for _, found := range idx.clusters {
		for _, member := range found.Accounts {
			if member == account {
				cluster = found
			}
		}
	}
	person := Person{SchemaVersion: SchemaVersion, GeneratedAt: now, Character: display, Account: account,
		Cluster: cluster, PossibleLinks: []Link{}}
	inCluster := make(map[string]bool, len(cluster.Accounts))
	for _, member := range cluster.Accounts {
		inCluster[member] = true
	}
	for _, link := range idx.possible {
		if inCluster[link.Accounts[0]] || inCluster[link.Accounts[1]] {
			person.PossibleLinks = append(person.PossibleLinks, link)
		}
	}
	var history []Span
	for _, member := range cluster.Accounts {
		facts := idx.accounts[member]
		entry := PersonAccount{Account: member, Sessions: len(facts.spans)}
		for _, name := range facts.characters {
			entry.Characters = append(entry.Characters, name)
		}
		sort.Strings(entry.Characters)
		for _, span := range facts.spans {
			history = append(history, span)
			if entry.FirstSeen == 0 || span.Started < entry.FirstSeen {
				entry.FirstSeen = span.Started
			}
			last := span.Ended
			if last == 0 {
				entry.Online = true
				last = span.Started
			}
			if last > entry.LastSeen {
				entry.LastSeen = last
			}
		}
		person.Accounts = append(person.Accounts, entry)
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Started > history[j].Started })
	person.HistoryTruncated = len(history) > personHistoryMax
	if person.HistoryTruncated {
		history = history[:personHistoryMax]
	}
	for _, span := range history {
		person.History = append(person.History, PersonSpan{Session: span.Session, Account: span.Account,
			Character: span.Character, Transport: span.Transport, Started: span.Started, Ended: span.Ended,
			Open: span.Ended == 0, Estimated: span.Estimated})
	}
	if person.History == nil {
		person.History = []PersonSpan{}
	}
	return person, true, nil
}
