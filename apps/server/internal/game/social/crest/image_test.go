/*
===========================================================================

image_test.go - the crest upload re-encode: accepted formats round-trip,
everything else is refused and produces nothing

#2282 (register half) under the owner's 2026-11-07 contract: BMP, PNG or
JPG in, one canonical 256-byte palette payload out; a renamed file, a
truncated file, an oversized file, a bomb header and a polyglot are all
refused before anything is stored.

===========================================================================
*/
package crest

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
)

/*
================
solidImage

A w x h image of one colour.
================
*/
func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

/*
================
encodePNG
================
*/
func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

/*
================
allSame

True when every payload byte is the same palette index.
================
*/
func allSame(payload []byte) bool {
	for _, b := range payload[1:] {
		if b != payload[0] {
			return false
		}
	}
	return true
}

func TestReencodeAcceptsEachFormat(t *testing.T) {
	red := color.RGBA{0xC0, 0x10, 0x10, 0xFF}
	pngBytes := encodePNG(t, solidImage(16, 16, red))

	var bmpBuf bytes.Buffer
	if err := bmp.Encode(&bmpBuf, solidImage(16, 16, red)); err != nil {
		t.Fatalf("bmp encode: %v", err)
	}
	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, solidImage(16, 16, red), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}

	var reference []byte
	for name, data := range map[string][]byte{"png": pngBytes, "bmp": bmpBuf.Bytes(), "jpeg": jpgBuf.Bytes()} {
		payload, err := Reencode(data)
		if err != nil {
			t.Fatalf("%s refused: %v", name, err)
		}
		if len(payload) != CrestPayloadBytes {
			t.Fatalf("%s payload = %d bytes, want %d", name, len(payload), CrestPayloadBytes)
		}
		if !allSame(payload) {
			t.Fatalf("%s payload not uniform for a solid image: %v...", name, payload[:8])
		}
		if reference == nil {
			reference = payload
		} else if !bytes.Equal(reference, payload) {
			t.Fatalf("%s payload differs from the first format's: formats must land on one canonical crest", name)
		}
	}
}

func TestReencodeKeepsNativeBottomUpOrder(t *testing.T) {
	// Top half red, bottom half blue at 32x32, downscaled to the crest.
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if y < 16 {
				img.SetRGBA(x, y, color.RGBA{0xC0, 0x10, 0x10, 0xFF})
			} else {
				img.SetRGBA(x, y, color.RGBA{0x10, 0x10, 0xC0, 0xFF})
			}
		}
	}
	payload, err := Reencode(encodePNG(t, img))
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	// The .crb payload is bottom-up (the client's decode reads
	// bytes[(15-y)*16+x] for display row y): the last row is the top.
	top, bottom := payload[240], payload[0]
	if top == bottom {
		t.Fatalf("top and bottom rows share palette index %d; the halves were lost", top)
	}
	for x := 0; x < 16; x++ {
		if payload[240+x] != top || payload[x] != bottom {
			t.Fatalf("row order wrong at x=%d: top=%d bottom=%d", x, payload[240+x], payload[x])
		}
	}
}

func TestReencodeRefusals(t *testing.T) {
	valid := encodePNG(t, solidImage(16, 16, color.RGBA{0x20, 0x80, 0x20, 0xFF}))

	oversized := make([]byte, MaxCrestImageBytes+1)
	copy(oversized, valid)

	bomb := encodePNG(t, solidImage(16, 16, color.RGBA{0x20, 0x80, 0x20, 0xFF}))
	// Rewrite the IHDR dimensions to claim a giant canvas and repair
	// the chunk CRC, so only the dimension guard can stop it; the body
	// stays a 16x16 image's.
	bomb[16], bomb[17], bomb[18], bomb[19] = 0x00, 0x01, 0x00, 0x00 // width 65536
	bomb[20], bomb[21], bomb[22], bomb[23] = 0x00, 0x01, 0x00, 0x00 // height 65536
	crc := crc32.ChecksumIEEE(bomb[12:29])
	binary.BigEndian.PutUint32(bomb[29:33], crc)

	cases := map[string][]byte{
		"empty":           {},
		"plain text":      []byte("BM this is not really an image at all, just words"),
		"gif magic":       append([]byte("GIF89a"), valid...),
		"truncated png":   valid[:len(valid)/2],
		"oversized":       oversized,
		"bomb header":     bomb,
		"polyglot script": append(append([]byte{}, valid...), []byte("<script>alert(1)</script>")...),
	}
	for name, data := range cases {
		if payload, err := Reencode(data); err == nil {
			t.Fatalf("%s accepted (%d payload bytes), want refusal", name, len(payload))
		}
	}
}
