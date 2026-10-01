# ===========================================================================
#
# native_ui_font_atlas.py - deterministic native Arial glyph publication
#
# Win32 GDI owns glyph shape and metrics, matching the v1.150 text renderer
# (FreeType stands in on other hosts, see render_style_freetype).
# This generator owns coverage, packing and the descriptor/pixel digest pair.
# fontResources.mjs supplies temporary destinations and publishes both files.
#
# ===========================================================================

import ctypes
import ctypes.wintypes as win
import hashlib
import json
import os
import sys
from io import BytesIO
from pathlib import Path

from PIL import Image, ImageFont


GDI_ERROR = 0xFFFFFFFF
GGO_BITMAP = 1
GGI_MARK_NONEXISTING_GLYPHS = 1
MISSING_GLYPH = 0xFFFF
DEFAULT_CHARSET = 1
ATLAS_WIDTH = 512
GLYPH_PADDING = 1
FACE_NAME = "Arial"
FONT_NAME_CAPACITY = 32
BITMAP_ROW_ALIGNMENT = 32
BITS_PER_BYTE = 8
CODEPOINTS = range(32, 127)
EXTRA_CODEPOINTS = (
	0x2026, 0x2019, 0x203B, 0x25C8, 0x201C, 0x201D, 0x2013,
	0x2460, 0x2461, 0x2462, 0x2463, 0xFF05,
)
# Latin-1 and Latin Extended-A include Turkish, Western and Central European
# alphabets. Coverage is a separate packing batch: existing ASCII and authored
# punctuation must keep their pixel coordinates in every font and style.
LATIN_CODEPOINTS = range(0xA0, 0x180)
COVERAGE_BATCHES = (CODEPOINTS, EXTRA_CODEPOINTS, LATIN_CODEPOINTS)
FONT_RECORDS = (
	{"fontIndex": 0, "role": "ui-default", "recordHeight": 9},
	{"fontIndex": 1, "role": "ui-small", "recordHeight": 8},
	{"fontIndex": 2, "role": "ui-title", "recordHeight": 12},
	{"fontIndex": 3, "role": "ui-medium", "recordHeight": 11},
	{"fontIndex": 4, "role": "ui-large", "recordHeight": 15},
)
STYLE_SLOTS = (
	{"slot": 0, "name": "normal", "weight": 0, "italic": 0, "underline": 0},
	{"slot": 2, "name": "bold", "weight": 0x258, "italic": 0, "underline": 0},
)


# ================
# LOGFONTW
# Win32 font-selection ABI; zero-initialization supplies the native defaults.
# ================
class LOGFONTW(ctypes.Structure):
	_fields_ = [
		("lfHeight", ctypes.c_long), ("lfWidth", ctypes.c_long),
		("lfEscapement", ctypes.c_long), ("lfOrientation", ctypes.c_long),
		("lfWeight", ctypes.c_long), ("lfItalic", ctypes.c_ubyte),
		("lfUnderline", ctypes.c_ubyte), ("lfStrikeOut", ctypes.c_ubyte),
		("lfCharSet", ctypes.c_ubyte), ("lfOutPrecision", ctypes.c_ubyte),
		("lfClipPrecision", ctypes.c_ubyte), ("lfQuality", ctypes.c_ubyte),
		("lfPitchAndFamily", ctypes.c_ubyte), ("lfFaceName", ctypes.c_wchar * FONT_NAME_CAPACITY),
	]


# ================
# POINT
# Signed glyph origin in the selected font's device coordinates.
# ================
class POINT(ctypes.Structure):
	_fields_ = [("x", ctypes.c_long), ("y", ctypes.c_long)]


# ================
# GLYPHMETRICS
# Black-box dimensions and advances returned by GetGlyphOutlineW.
# ================
class GLYPHMETRICS(ctypes.Structure):
	_fields_ = [
		("gmBlackBoxX", win.UINT), ("gmBlackBoxY", win.UINT),
		("gmptGlyphOrigin", POINT), ("gmCellIncX", ctypes.c_short), ("gmCellIncY", ctypes.c_short),
	]


# ================
# FIXED
# GDI's 16.16 fixed-point matrix element, not a floating-point approximation.
# ================
class FIXED(ctypes.Structure):
	_fields_ = [("fract", win.WORD), ("value", ctypes.c_short)]


# ================
# MAT2
# Two-dimensional outline transform; glyph capture uses the identity matrix.
# ================
class MAT2(ctypes.Structure):
	_fields_ = [("eM11", FIXED), ("eM12", FIXED), ("eM21", FIXED), ("eM22", FIXED)]


# ================
# TEXTMETRICW
# Complete Win32 structure: omitting an unused field would corrupt the ABI.
# ================
class TEXTMETRICW(ctypes.Structure):
	_fields_ = [
		("tmHeight", ctypes.c_long), ("tmAscent", ctypes.c_long), ("tmDescent", ctypes.c_long),
		("tmInternalLeading", ctypes.c_long), ("tmExternalLeading", ctypes.c_long),
		("tmAveCharWidth", ctypes.c_long), ("tmMaxCharWidth", ctypes.c_long),
		("tmWeight", ctypes.c_long), ("tmOverhang", ctypes.c_long),
		("tmDigitizedAspectX", ctypes.c_long), ("tmDigitizedAspectY", ctypes.c_long),
		("tmFirstChar", ctypes.c_wchar), ("tmLastChar", ctypes.c_wchar),
		("tmDefaultChar", ctypes.c_wchar), ("tmBreakChar", ctypes.c_wchar),
		("tmItalic", ctypes.c_ubyte), ("tmUnderlined", ctypes.c_ubyte),
		("tmStruckOut", ctypes.c_ubyte), ("tmPitchAndFamily", ctypes.c_ubyte), ("tmCharSet", ctypes.c_ubyte),
	]


# ================
# configure_gdi
# Declare pointer-sized handles explicitly so 64-bit Python cannot truncate them.
# ================
def configure_gdi():
	gdi = ctypes.WinDLL("gdi32", use_last_error=True)
	gdi.CreateCompatibleDC.argtypes = [win.HDC]
	gdi.CreateCompatibleDC.restype = win.HDC
	gdi.CreateFontIndirectW.argtypes = [ctypes.POINTER(LOGFONTW)]
	gdi.CreateFontIndirectW.restype = win.HFONT
	gdi.SelectObject.argtypes = [win.HDC, win.HGDIOBJ]
	gdi.SelectObject.restype = win.HGDIOBJ
	gdi.GetTextMetricsW.argtypes = [win.HDC, ctypes.POINTER(TEXTMETRICW)]
	gdi.GetTextMetricsW.restype = win.BOOL
	gdi.GetGlyphIndicesW.argtypes = [win.HDC, win.LPCWSTR, ctypes.c_int, ctypes.POINTER(win.WORD), win.DWORD]
	gdi.GetGlyphIndicesW.restype = win.DWORD
	gdi.GetGlyphOutlineW.argtypes = [
		win.HDC, win.UINT, win.UINT, ctypes.POINTER(GLYPHMETRICS),
		win.DWORD, ctypes.c_void_p, ctypes.POINTER(MAT2),
	]
	gdi.GetGlyphOutlineW.restype = win.DWORD
	gdi.DeleteObject.argtypes = [win.HGDIOBJ]
	gdi.DeleteObject.restype = win.BOOL
	gdi.DeleteDC.argtypes = [win.HDC]
	gdi.DeleteDC.restype = win.BOOL
	return gdi


# ================
# native_gdi_pixel_height
# A17690 uses MulDiv(recordHeight, 96, 72) before negating LOGFONT height.
# ================
def native_gdi_pixel_height(record_height):
	return round((record_height * 96) / 72)


# ================
# create_font
# Native A17690 selects DEFAULT_CHARSET and normal/bold CTextBoard style slots.
# ================
def create_font(gdi, record_height, style):
	font = LOGFONTW()
	font.lfHeight = -native_gdi_pixel_height(record_height)
	font.lfWeight = style["weight"]
	font.lfItalic = style["italic"]
	font.lfUnderline = style["underline"]
	font.lfCharSet = DEFAULT_CHARSET
	font.lfFaceName = FACE_NAME
	handle = gdi.CreateFontIndirectW(ctypes.byref(font))
	if not handle:
		raise ctypes.WinError(ctypes.get_last_error())
	return handle


# ================
# read_glyph
# Require real glyphs for the new Latin coverage: a successful outline can
# be a fallback box. Existing authored punctuation retains the native GDI
# substitution behavior. Whitespace may have no pixels but has an advance.
# ================
def read_glyph(gdi, hdc, codepoint):
	index = win.WORD()
	result = gdi.GetGlyphIndicesW(hdc, chr(codepoint), 1, ctypes.byref(index), GGI_MARK_NONEXISTING_GLYPHS)
	if result == GDI_ERROR or index.value == MISSING_GLYPH and codepoint in LATIN_CODEPOINTS:
		raise ValueError(f"{FACE_NAME} has no glyph for U+{codepoint:04X}")
	matrix = MAT2()
	matrix.eM11.value = 1
	matrix.eM22.value = 1
	metrics = GLYPHMETRICS()
	byte_count = gdi.GetGlyphOutlineW(hdc, codepoint, GGO_BITMAP, ctypes.byref(metrics), 0, None, ctypes.byref(matrix))
	if byte_count == GDI_ERROR:
		raise ctypes.WinError(ctypes.get_last_error())
	buffer = (ctypes.c_ubyte * max(1, byte_count))()
	if byte_count:
		result = gdi.GetGlyphOutlineW(
			hdc, codepoint, GGO_BITMAP, ctypes.byref(metrics), byte_count, ctypes.byref(buffer), ctypes.byref(matrix),
		)
		if result == GDI_ERROR:
			raise ctypes.WinError(ctypes.get_last_error())
	width = int(metrics.gmBlackBoxX)
	height = int(metrics.gmBlackBoxY)
	row_bytes = ((width + BITMAP_ROW_ALIGNMENT - 1) // BITMAP_ROW_ALIGNMENT) * (BITMAP_ROW_ALIGNMENT // BITS_PER_BYTE)
	pixels = []
	for y in range(height):
		for x in range(width):
			if buffer[y * row_bytes + x // BITS_PER_BYTE] & (0x80 >> (x % BITS_PER_BYTE)):
				pixels.append((x, y))
	return {
		"width": max(1, width), "height": max(1, height),
		"originX": int(metrics.gmptGlyphOrigin.x), "originY": int(metrics.gmptGlyphOrigin.y),
		"advanceX": int(metrics.gmCellIncX), "pixels": pixels,
	}


# ================
# render_style
# One explicit GDI lifetime per style. Restore the selected object before
# releasing the font, including when coverage validation fails.
# ================
def render_style(gdi, font_record, style):
	hdc = gdi.CreateCompatibleDC(None)
	if not hdc:
		raise ctypes.WinError(ctypes.get_last_error())
	hfont = None
	previous = None
	try:
		hfont = create_font(gdi, font_record["recordHeight"], style)
		previous = gdi.SelectObject(hdc, hfont)
		if not previous:
			raise ctypes.WinError(ctypes.get_last_error())
		metrics = TEXTMETRICW()
		if not gdi.GetTextMetricsW(hdc, ctypes.byref(metrics)):
			raise ctypes.WinError(ctypes.get_last_error())
		glyphs = {}
		for batch in COVERAGE_BATCHES:
			for codepoint in batch:
				glyphs[str(codepoint)] = read_glyph(gdi, hdc, codepoint)
		return {
			"recordHeight": font_record["recordHeight"],
			"pixelHeight": native_gdi_pixel_height(font_record["recordHeight"]),
			"metricsHeight": int(metrics.tmHeight), "ascent": int(metrics.tmAscent),
			"descent": int(metrics.tmDescent), "aveCharWidth": int(metrics.tmAveCharWidth),
			"maxCharWidth": int(metrics.tmMaxCharWidth), "weight": int(metrics.tmWeight),
			"styleSlot": style["slot"], "styleName": style["name"], "glyphs": glyphs,
		}
	finally:
		if previous:
			gdi.SelectObject(hdc, previous)
		if hfont:
			gdi.DeleteObject(hfont)
		gdi.DeleteDC(hdc)


# ================
# render_style_freetype
# Non-Windows hosts have no GDI: FreeType (through Pillow) rasterizes the same
# Arial faces monochrome and reports GDI-style metrics. Glyph pixels follow
# FreeType hinting, so they are close to, not identical with, the GDI atlas.
# SRO_FONT_ARIAL / SRO_FONT_ARIAL_BOLD override the face files.
# ================
# GDI FW_NORMAL / FW_BOLD, reported as TEXTMETRIC tmWeight.
FW_NORMAL = 400
FW_BOLD = 700
FREETYPE_FACES = {
	"normal": ("SRO_FONT_ARIAL", (
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/usr/share/fonts/truetype/msttcorefonts/Arial.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
	)),
	"bold": ("SRO_FONT_ARIAL_BOLD", (
		"/System/Library/Fonts/Supplemental/Arial Bold.ttf",
		"/usr/share/fonts/truetype/msttcorefonts/Arial_Bold.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
	)),
}


# ================
# freetype_face
# The first existing face file for a style, the environment override first.
# ================
def freetype_face(style_name):
	variable, candidates = FREETYPE_FACES[style_name]
	override = os.environ.get(variable)
	for candidate in ((override,) if override else ()) + candidates:
		if Path(candidate).is_file():
			return candidate
	raise SystemExit(f"No Arial face for style {style_name!r}; set {variable} to a .ttf file.")


def render_style_freetype(font_record, style):
	pixel_height = native_gdi_pixel_height(font_record["recordHeight"])
	font = ImageFont.truetype(freetype_face(style["name"]), pixel_height)
	font.fontmode = "1"
	ascent, descent = font.getmetrics()
	glyphs = {}
	for batch in COVERAGE_BATCHES:
		for codepoint in batch:
			char = chr(codepoint)
			mask, (left, top) = font.getmask2(char, mode="1", anchor="ls")
			width, height = mask.size
			pixels = [(x, y) for y in range(height) for x in range(width) if mask.getpixel((x, y))]
			if pixels:
				xs = [x for x, _ in pixels]
				ys = [y for _, y in pixels]
				x0, y0 = min(xs), min(ys)
				pixels = [(x - x0, y - y0) for x, y in pixels]
				width, height = max(xs) - x0 + 1, max(ys) - y0 + 1
				left, top = left + x0, top + y0
			else:
				width = height = 1
				left, top = 0, 0
			glyphs[str(codepoint)] = {
				"width": width, "height": height, "originX": left, "originY": -top,
				"advanceX": round(font.getlength(char)), "pixels": pixels,
			}
	advances = [glyph["advanceX"] for glyph in glyphs.values()]
	return {
		"recordHeight": font_record["recordHeight"], "pixelHeight": pixel_height,
		"metricsHeight": ascent + descent, "ascent": ascent, "descent": descent,
		"aveCharWidth": glyphs[str(ord("x"))]["advanceX"], "maxCharWidth": max(advances),
		"weight": FW_BOLD if style["name"] == "bold" else FW_NORMAL,
		"styleSlot": style["slot"], "styleName": style["name"], "glyphs": glyphs,
	}


# ================
# pack_glyphs
# Append coverage by whole-font batches. Appending glyphs inside each style
# would shift the following style's old coordinates, defeating stable packing.
# ================
def pack_glyphs(fonts):
	x = GLYPH_PADDING
	y = GLYPH_PADDING
	row_height = 0
	for batch in COVERAGE_BATCHES:
		for font in fonts.values():
			for style in font["styles"].values():
				for codepoint in batch:
					glyph = style["glyphs"][str(codepoint)]
					if x + glyph["width"] + GLYPH_PADDING > ATLAS_WIDTH:
						x = GLYPH_PADDING
						y += row_height + GLYPH_PADDING
						row_height = 0
					glyph["x"] = x
					glyph["y"] = y
					x += glyph["width"] + GLYPH_PADDING
					row_height = max(row_height, glyph["height"])
	image = Image.new("RGBA", (ATLAS_WIDTH, max(1, y + row_height + GLYPH_PADDING)), (0, 0, 0, 0))
	for font in fonts.values():
		for style in font["styles"].values():
			for glyph in style["glyphs"].values():
				for px, py in glyph.pop("pixels"):
					image.putpixel((glyph["x"] + px, glyph["y"] + py), (255, 255, 255, 255))
	return image


# ================
# write_if_changed
# Stable mtimes keep pack caches useful. The publisher normally supplies
# temporary siblings, then atomically replaces the descriptor and image.
# ================
def write_if_changed(target, data):
	try:
		if target.read_bytes() == data:
			return False
	except FileNotFoundError:
		pass
	target.parent.mkdir(parents=True, exist_ok=True)
	target.write_bytes(data)
	return True


# ================
# main
# Build every style, pack once, and bind the descriptor to the exact PNG bytes.
# ================
def main():
	if len(sys.argv) != 3:
		raise SystemExit("usage: native_ui_font_atlas.py <atlas.json> <atlas.png>")
	json_path = Path(sys.argv[1])
	image_path = Path(sys.argv[2])
	gdi = configure_gdi() if sys.platform == "win32" else None
	fonts = {}
	for record in FONT_RECORDS:
		styles = {}
		for style in STYLE_SLOTS:
			styles[str(style["slot"])] = (
				render_style(gdi, record, style) if gdi else render_style_freetype(record, style)
			)
		normal = styles["0"]
		fonts[str(record["fontIndex"])] = {
			key: value for key, value in normal.items() if key not in ("styleSlot", "styleName")
		}
		fonts[str(record["fontIndex"])].update({
			"role": record["role"], "availableStyleSlots": [style["slot"] for style in STYLE_SLOTS], "styles": styles,
		})
	image = pack_glyphs(fonts)
	png_buffer = BytesIO()
	image.save(png_buffer, format="PNG")
	png_bytes = png_buffer.getvalue()
	atlas = {
		"source": "SRO_Client.exe v1.150 native font path: sub_a122b0 -> sub_a17690 -> GetGlyphOutlineW(GGO_BITMAP)",
		"image": "/assets/fonts/native-ui-font-atlas.png", "face": FACE_NAME,
		"styleSlots": {"0": "normal", "2": "bold"}, "atlasWidth": image.width, "atlasHeight": image.height,
		"fonts": fonts, "imageSha256": hashlib.sha256(png_bytes).hexdigest(),
	}
	json_bytes = json.dumps(atlas, separators=(",", ":")).encode("utf-8")
	changed = []
	if write_if_changed(json_path, json_bytes):
		changed.append("json")
	if write_if_changed(image_path, png_bytes):
		changed.append("png")
	status = "+".join(changed) if changed else "unchanged"
	print(f"Built native UI font atlas ({status}): {json_path} + {image_path} ({image.width}x{image.height})")


if __name__ == "__main__":
	main()
