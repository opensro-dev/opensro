/*
===========================================================================

image.go - an uploaded crest image becomes the native crest payload

The browser port's crest source (#2282, owner decision 2026-11-07): a
guild or union master uploads a BMP, PNG or JPG and the server - never
the client - turns it into the one canonical crest the game already
speaks: 256 palette indices, 16x16, bottom-up, the payload a .crb
carries and the client decodes in foundation/ui/guild-crest.ts.

The acceptance is deliberately narrow, and every refusal happens
before anything is stored:

  - the format is read from the magic bytes, never a file name or a
    claimed MIME type;
  - the body is capped at 256 KB before it is read at all;
  - the image header's dimensions are judged before any decode, so a
    bomb header (a giant canvas over a tiny body) dies on its config;
  - the decode must consume the whole body - a polyglot (a real image
    with bytes appended) is refused, so nothing smuggled can ride in;
  - the decoded pixels are resized to 16x16 and quantized to the silk
    palette by the native metric (Manhattan distance, last index
    winning a tie). Only those 256 re-encoded bytes ever reach
    storage, which strips metadata, profiles and trailing data by
    construction.

===========================================================================
*/
package crest

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/bmp"
)

// CrestSize is the native crest's edge in pixels (guild-crest.ts
// decodes exactly 16x16 from 256 palette indices).
const CrestSize = 16

// CrestPayloadBytes is one crest payload: 16x16 palette indices.
const CrestPayloadBytes = CrestSize * CrestSize

// MaxCrestImageBytes caps an upload body before it is read (owner
// contract, 2026-11-07): the largest crest source ever needed is a
// small bitmap, and the cap makes a memory flood through this lane
// impossible.
const MaxCrestImageBytes = 256 << 10

// maxCrestDimension bounds a source image's edge as read from its
// header, before decode: far above the 16x16 target, far below any
// decompression bomb.
const maxCrestDimension = 1024

// ErrRefused names every acceptance refusal: the caller maps them all
// to the one "not a usable crest image" answer, and nothing is
// written for any of them.
var ErrRefused = errors.New("crest: not a usable crest image")

func init() {
	// bmp is not in the standard library's registry; enlist its
	// decoder for DecodeConfig/Decode beside png and jpeg.
	image.RegisterFormat("bmp", "BM", bmp.Decode, bmp.DecodeConfig)
}

/*
================
Reencode

The canonical 256-byte crest payload for an uploaded BMP, PNG or
JPG, or ErrRefused. The payload's first row is the image's bottom
row, the .crb order the client decodes.
================
*/
func Reencode(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxCrestImageBytes {
		return nil, ErrRefused
	}
	if !crestMagic(data) {
		return nil, ErrRefused
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 ||
		config.Width > maxCrestDimension || config.Height > maxCrestDimension {
		return nil, ErrRefused
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrRefused
	}
	if !crestConsumed(data) {
		return nil, ErrRefused
	}
	return quantizeCrest(img), nil
}

/*
================
crestMagic

The format from the body's first bytes: BMP "BM", the PNG signature,
or JPEG's FF D8 FF. Anything else is not a crest source, whatever it
is named.
================
*/
func crestMagic(data []byte) bool {
	switch {
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return true
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return true
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return true
	}
	return false
}

/*
================
crestConsumed

The decode must account for the whole body; trailing bytes after the
image proper refuse the upload (the polyglot case). BMP speaks for
itself through its header's file size; PNG must end at its IEND
chunk; JPEG at its end-of-image marker.
================
*/
func crestConsumed(data []byte) bool {
	switch {
	case data[0] == 'B' && data[1] == 'M':
		return len(data) >= 6 && int(binary.LittleEndian.Uint32(data[2:6])) == len(data)
	case data[0] == 0x89:
		iend := []byte{0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}
		return bytes.HasSuffix(data, iend)
	default:
		return len(data) >= 2 && data[len(data)-2] == 0xff && data[len(data)-1] == 0xd9
	}
}

/*
================
quantizeCrest

The resized, quantized payload: each of the 16x16 cells averages its
source box (a single sampled pixel when the source is smaller than
the crest), then takes the nearest palette colour by the native
Manhattan metric, the last index winning a tie. Rows are
written bottom-up, the .crb order.
================
*/
func quantizeCrest(img image.Image) []byte {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	payload := make([]byte, CrestPayloadBytes)
	for y := 0; y < CrestSize; y++ {
		for x := 0; x < CrestSize; x++ {
			r, g, b := crestCellAverage(img, bounds, width, height, x, y)
			payload[(CrestSize-1-y)*CrestSize+x] = nearestPaletteIndex(r, g, b)
		}
	}
	return payload
}

/*
================
crestCellAverage

One crest cell's mean colour over its box of the source. When the
source is smaller than the crest the box is empty on that axis and
the cell takes the nearest source pixel instead.
================
*/
func crestCellAverage(img image.Image, bounds image.Rectangle, width, height, x, y int) (uint32, uint32, uint32) {
	x0, x1 := x*width/CrestSize, (x+1)*width/CrestSize
	y0, y1 := y*height/CrestSize, (y+1)*height/CrestSize
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var r, g, b, n uint32
	for sy := y0; sy < y1; sy++ {
		for sx := x0; sx < x1; sx++ {
			pr, pg, pb, _ := img.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
			r += pr >> 8
			g += pg >> 8
			b += pb >> 8
			n++
		}
	}
	return r / n, g / n, b / n
}

/*
================
nearestPaletteIndex

The silk palette's nearest colour to (r, g, b) by Manhattan
distance; on a tie the later index wins, as the native loop's >=
update makes it.
================
*/
func nearestPaletteIndex(r, g, b uint32) byte {
	best, index := uint32(1)<<31, 0
	for i, colour := range silkPalette {
		distance := absDiff(r, uint32(colour[0])) + absDiff(g, uint32(colour[1])) + absDiff(b, uint32(colour[2]))
		if distance <= best {
			best, index = distance, i
		}
	}
	return byte(index)
}

/*
================
absDiff
================
*/
func absDiff(a, b uint32) uint32 {
	if a >= b {
		return a - b
	}
	return b - a
}
