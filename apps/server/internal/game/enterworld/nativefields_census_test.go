/*
===========================================================================

nativefields_census_test.go - the client read census, run as a test

wireItemFieldNames is the server's copy of a client fact: which item
fields a browser build reads. A hand census drifts (the first one
missed the readers that alias item.tooltip.fields as f), so this test
re-runs the census over the client source and fails on any read the
wire list does not publish. Travel paths covered: the wire
nativeFields itself, the item.tooltip.fields copy itemTooltipReference
makes of it (direct reads, bracket reads, aliases bound from it -
including comma-continued bindings and destructuring), and the direct
nativeFields reads that bypass the tooltip (mall-use, cos-timer, the
macro rows). A computed-key read on a fields alias resolves to the
schema-named string literals in its file (item-tooltip-stats' read()
keys). Only schema names count as reads, so unrelated properties on
like-named locals never enter the census.
===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	clientNativeFieldsRead  = regexp.MustCompile(`nativeFields\??\.([A-Za-z_][A-Za-z0-9_]*)|nativeFields\[\s*"([A-Za-z0-9_]+)"\s*\]`)
	clientTooltipFieldsRead = regexp.MustCompile(`tooltip\?\.fields\??\.([A-Za-z_][A-Za-z0-9_]*)|tooltip\.fields\??\.([A-Za-z_][A-Za-z0-9_]*)|tooltip\?\.fields\[\s*"([A-Za-z0-9_]+)"\s*\]|tooltip\.fields\[\s*"([A-Za-z0-9_]+)"\s*\]`)
	clientFieldsBinding     = regexp.MustCompile(`(?:const|let|var|,)\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*([^,;\n]+)`)
	clientQuotedName        = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)"`)
	clientIdentifier        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type aliasRe struct {
	read     *regexp.Regexp
	destruct *regexp.Regexp
	computed *regexp.Regexp
	word     *regexp.Regexp
}

var aliasPatternCache = map[string]aliasRe{}

/*
================
aliasReFor
================
*/
func aliasReFor(name string) aliasRe {
	if re, ok := aliasPatternCache[name]; ok {
		return re
	}
	quoted := regexp.QuoteMeta(name)
	re := aliasRe{
		read:     regexp.MustCompile(`\b` + quoted + `\??\.([A-Za-z_][A-Za-z0-9_]*)|\b` + quoted + `\[\s*"([A-Za-z0-9_]+)"\s*\]`),
		destruct: regexp.MustCompile(`\{([^}]*)\}\s*=\s*[^;\n]*\b` + quoted + `\b`),
		computed: regexp.MustCompile(`\b` + quoted + `\s*\[\s*[A-Za-z_$]`),
		word:     regexp.MustCompile(`\b` + quoted + `\b`),
	}
	aliasPatternCache[name] = re
	return re
}

/*
================
TestWireItemFieldsCoverClientReads
================
*/
func TestWireItemFieldsCoverClientReads(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "client-next", "src")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("client source root %s: %v", root, err)
	}
	published := make(map[string]bool, len(wireItemFieldNames))
	for _, name := range wireItemFieldNames {
		published[name] = true
	}
	reads := map[string]string{}
	add := func(name, site string) {
		if _, ok := nativeFieldIDs[name]; !ok {
			return
		}
		if _, ok := reads[name]; !ok {
			reads[name] = site
		}
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx")) {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(blob)
		site := func(at int) string {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			return rel + ":" + strconv.Itoa(strings.Count(text[:at], "\n")+1)
		}
		collect := func(re *regexp.Regexp) {
			for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
				for g := 2; g+1 < len(m); g += 2 {
					if m[g] >= 0 {
						add(text[m[g]:m[g+1]], site(m[0]))
						break
					}
				}
			}
		}
		collect(clientNativeFieldsRead)
		collect(clientTooltipFieldsRead)
		// Aliases bound from a fields value, to a fixed point (an alias
		// may be bound from another alias).
		type binding struct{ name, rhs string }
		var bindings []binding
		for _, m := range clientFieldsBinding.FindAllStringSubmatch(text, -1) {
			bindings = append(bindings, binding{m[1], m[2]})
		}
		aliases := map[string]bool{}
		for changed := true; changed; {
			changed = false
			for _, b := range bindings {
				if aliases[b.name] {
					continue
				}
				fromFields := strings.Contains(b.rhs, "tooltip?.fields") ||
					strings.Contains(b.rhs, "tooltip.fields") ||
					strings.Contains(b.rhs, "nativeFields")
				if !fromFields {
					for alias := range aliases {
						if aliasReFor(alias).word.MatchString(b.rhs) {
							fromFields = true
							break
						}
					}
				}
				if fromFields {
					aliases[b.name] = true
					changed = true
				}
			}
		}
		computedKey := false
		for alias := range aliases {
			re := aliasReFor(alias)
			collect(re.read)
			for _, m := range re.destruct.FindAllStringSubmatchIndex(text, -1) {
				for _, part := range strings.Split(text[m[2]:m[3]], ",") {
					name := strings.TrimSpace(strings.SplitN(part, ":", 2)[0])
					if !strings.HasPrefix(name, "...") && clientIdentifier.MatchString(name) {
						add(name, site(m[0]))
					}
				}
			}
			if re.computed.MatchString(text) {
				computedKey = true
			}
		}
		if computedKey || filepath.Base(path) == "item-tooltip-stats.ts" {
			// Computed keys are fed literals: read("...") in the stats
			// tables, values[key] in the recovery rows.
			for _, m := range clientQuotedName.FindAllStringSubmatchIndex(text, -1) {
				add(text[m[2]:m[3]], site(m[0]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk client source: %v", err)
	}
	if len(reads) < 50 {
		t.Fatalf("census found only %d schema reads; the harvest is broken, not the client", len(reads))
	}
	var missing []string
	for name := range reads {
		if !published[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("client reads item field %q (%s) but wireItemFieldNames does not publish it", name, reads[name])
	}
}
