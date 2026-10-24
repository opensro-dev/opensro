/*
===========================================================================

textdata_catalogs.go - immutable textdata shared by creation and gameplay

Loads authoritative gameplay tables and the native name filter before
network admission, so request handlers never discover missing media.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/game/namefilter"
)

/*
================
TextdataCatalogs

The immutable, server-authoritative tables shared by character creation,
EnterWorld and gameplay. Browser presentation paths belong to its manifests.
Construct through LoadTextdataCatalogs before admitting network requests.
================
*/
type TextdataCatalogs struct {
	Items        *TextdataItems
	Levels       *TextdataLevels
	Skills       *TextdataSkills
	MagicOptions *TextdataMagicOptions
	NameFilter   *namefilter.Filter
}

/*
================
LoadTextdataCatalogs

Materializes live authority tables before network admission. Individual
table types remain lazy-capable for small unit fixtures; production fails
closed here instead of discovering absent media during a request.
================
*/
func LoadTextdataCatalogs(dir string) (*TextdataCatalogs, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("textdata readiness: directory is required")
	}

	catalogs := &TextdataCatalogs{
		Items:        NewTextdataItems(dir),
		Levels:       NewTextdataLevels(dir),
		Skills:       NewTextdataSkills(dir),
		MagicOptions: NewTextdataMagicOptions(dir),
	}
	if catalogs.Items.Len() == 0 {
		return nil, fmt.Errorf("textdata readiness: no itemdata rows loaded from %s", dir)
	}
	if catalogs.Levels.Len() == 0 {
		return nil, fmt.Errorf("textdata readiness: no leveldata rows loaded from %s", dir)
	}
	if err := catalogs.Skills.Load(); err != nil {
		return nil, fmt.Errorf("textdata readiness: %w", err)
	}
	if catalogs.MagicOptions.Len() == 0 {
		return nil, fmt.Errorf("textdata readiness: no magicoption rows loaded from %s", dir)
	}
	filter, err := namefilter.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("textdata readiness: %w", err)
	}
	catalogs.NameFilter = filter
	return catalogs, nil
}
