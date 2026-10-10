/*
===========================================================================

regions.go - C8 regions.json: each world region's area and name

The rows come from the client's own region tables, so the Node stage fills
them: regioncode.txt names a region RN_<AREA>_<PLACE> (the area prefix is
the game's own grouping: ch, wc, oa, tk, kk, rm, ca, am, eu), and
textzonename gives the English name the minimap shows. This file owns the
row type and its schema.

===========================================================================
*/
package catalogue

/*
================
Region
================
*/
type Region struct {
	ID uint16 `json:"id"`
	// Area is the region code's area prefix, lower case ("wc").
	Area string `json:"area,omitempty"`
	// Place is the region code itself ("RN_WC_DUNHWANG").
	Place string `json:"place,omitempty"`
	Name  string `json:"name,omitempty"`
}

/*
================
RegionsFile
================
*/
type RegionsFile struct {
	Header
	Regions []Region `json:"regions"`
}

/*
================
AtlasFile

The world atlas: one WebP per region with minimap art, at
atlas/<x>x<z>.webp, where x is the region id's low byte and z its high
byte. A tile is TileSize pixels square and covers RegionSize world units,
north (higher z) up: a local position (x, z) is pixel
(x / RegionSize * TileSize, (RegionSize - z) / RegionSize * TileSize).
Filled by the Node stage from the client's minimap art.
================
*/
type AtlasFile struct {
	Header
	TileSize   int `json:"tileSize"`
	RegionSize int `json:"regionSize"`
	// Tiles are the [x, z] pairs that have a tile.
	Tiles [][2]int `json:"tiles"`
}

// atlasTileSize is the minimap art's size (every NTX1 tile is 256 square);
// atlasRegionSize the world units one region spans.
const (
	atlasTileSize   = 256
	atlasRegionSize = 1920
)

/*
================
NewAtlas

The atlas file before the Node stage lists its tiles.
================
*/
func NewAtlas(header Header) AtlasFile {
	return AtlasFile{Header: header, TileSize: atlasTileSize, RegionSize: atlasRegionSize, Tiles: [][2]int{}}
}
