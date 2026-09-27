# ===========================================================================
#
# native_ui_font_atlas.py - the native UI font atlas, rendered with Win32 GDI
#
# The v1.150 client draws UI text with GDI Arial glyphs. This script renders
# the same glyphs through GDI (GetGlyphOutlineW bitmaps) for every font record
# and CTextBoard style slot, packs them into one atlas image and writes the
# glyph metrics as JSON. fontResources.mjs runs it during `pnpm assets build`
# and publishes the result; it only runs on Windows.
#
#     py -3 native_ui_font_atlas.py <atlas.json> <atlas.png>
#
# ===========================================================================

import ctypes
import ctypes.wintypes
import json
import sys
from io import BytesIO
from pathlib import Path

from PIL import Image


GDI_ERROR = 0xFFFFFFFF
GGO_BITMAP = 1
ANSI_CHARSET = 0
OUT_DEFAULT_PRECIS = 0
CLIP_DEFAULT_PRECIS = 0
DEFAULT_QUALITY = 0
DEFAULT_PITCH = 0

ATLAS_WIDTH = 512
GLYPH_PADDING = 1
FACE_NAME = "Arial"
CODEPOINTS = range(32, 127)
# Native draws authored Unicode punctuation as its own Arial glyph (one glyph
# run per wchar, no text remap). Keep additions append-only: the extra pass
# packs these after the basic ASCII range, and append-only ordering keeps every
# pre-existing glyph at the same atlas coordinates.
#
# U+2026: localized UI ellipsis (GDI advance 12 at font0 vs 9 for "...").
# U+2019: localized right apostrophe used by world-map labels including
#         "Witch’s Lighthouse" and "Traveler’s Hill".
# U+203B: reference mark opening the game-guide start text
#         (UIIT_STT_GAMEGUIDE_START_2); all locales author it.
# U+25C8: white-diamond bullet used across English guide articles.
# U+201C/U+201D: double quotation marks used in English guide text.
# U+2013: en dash used in English guide text.
# U+2460-U+2463: circled digits used in English guide text.
# U+FF05: fullwidth percent sign used in English guide text.
# Every entry below was verified present (non-blank GDI outline) in Arial
# before admission; never add a codepoint the rasterizer cannot draw.
EXTRA_CODEPOINTS = [0x2026, 0x2019, 0x203B, 0x25C8, 0x201C, 0x201D, 0x2013,
                    0x2460, 0x2461, 0x2462, 0x2463, 0xFF05]
FONT_RECORDS = [
    {"fontIndex": 0, "role": "ui-default", "recordHeight": 9},
    {"fontIndex": 1, "role": "ui-small", "recordHeight": 8},
    {"fontIndex": 2, "role": "ui-title", "recordHeight": 12},
    {"fontIndex": 3, "role": "ui-medium", "recordHeight": 11},
    {"fontIndex": 4, "role": "ui-large", "recordHeight": 15},
]
STYLE_SLOTS = [
    {"slot": 0, "name": "normal", "weight": 0, "italic": 0, "underline": 0},
    {"slot": 2, "name": "bold", "weight": 0x258, "italic": 0, "underline": 0},
]


class LOGFONTW(ctypes.Structure):
    _fields_ = [
        ("lfHeight", ctypes.c_long),
        ("lfWidth", ctypes.c_long),
        ("lfEscapement", ctypes.c_long),
        ("lfOrientation", ctypes.c_long),
        ("lfWeight", ctypes.c_long),
        ("lfItalic", ctypes.c_ubyte),
        ("lfUnderline", ctypes.c_ubyte),
        ("lfStrikeOut", ctypes.c_ubyte),
        ("lfCharSet", ctypes.c_ubyte),
        ("lfOutPrecision", ctypes.c_ubyte),
        ("lfClipPrecision", ctypes.c_ubyte),
        ("lfQuality", ctypes.c_ubyte),
        ("lfPitchAndFamily", ctypes.c_ubyte),
        ("lfFaceName", ctypes.c_wchar * 32),
    ]


class POINT(ctypes.Structure):
    _fields_ = [("x", ctypes.c_long), ("y", ctypes.c_long)]


class GLYPHMETRICS(ctypes.Structure):
    _fields_ = [
        ("gmBlackBoxX", ctypes.wintypes.UINT),
        ("gmBlackBoxY", ctypes.wintypes.UINT),
        ("gmptGlyphOrigin", POINT),
        ("gmCellIncX", ctypes.c_short),
        ("gmCellIncY", ctypes.c_short),
    ]


class FIXED(ctypes.Structure):
    _fields_ = [("fract", ctypes.wintypes.WORD), ("value", ctypes.c_short)]


class MAT2(ctypes.Structure):
    _fields_ = [("eM11", FIXED), ("eM12", FIXED), ("eM21", FIXED), ("eM22", FIXED)]


class TEXTMETRICW(ctypes.Structure):
    _fields_ = [
        ("tmHeight", ctypes.c_long),
        ("tmAscent", ctypes.c_long),
        ("tmDescent", ctypes.c_long),
        ("tmInternalLeading", ctypes.c_long),
        ("tmExternalLeading", ctypes.c_long),
        ("tmAveCharWidth", ctypes.c_long),
        ("tmMaxCharWidth", ctypes.c_long),
        ("tmWeight", ctypes.c_long),
        ("tmOverhang", ctypes.c_long),
        ("tmDigitizedAspectX", ctypes.c_long),
        ("tmDigitizedAspectY", ctypes.c_long),
        ("tmFirstChar", ctypes.c_wchar),
        ("tmLastChar", ctypes.c_wchar),
        ("tmDefaultChar", ctypes.c_wchar),
        ("tmBreakChar", ctypes.c_wchar),
        ("tmItalic", ctypes.c_ubyte),
        ("tmUnderlined", ctypes.c_ubyte),
        ("tmStruckOut", ctypes.c_ubyte),
        ("tmPitchAndFamily", ctypes.c_ubyte),
        ("tmCharSet", ctypes.c_ubyte),
    ]


# ================
# configure_gdi
# ================
def configure_gdi():
    gdi32 = ctypes.WinDLL("gdi32", use_last_error=True)
    gdi32.CreateCompatibleDC.argtypes = [ctypes.wintypes.HDC]
    gdi32.CreateCompatibleDC.restype = ctypes.wintypes.HDC
    gdi32.CreateFontIndirectW.argtypes = [ctypes.POINTER(LOGFONTW)]
    gdi32.CreateFontIndirectW.restype = ctypes.wintypes.HFONT
    gdi32.SelectObject.argtypes = [ctypes.wintypes.HDC, ctypes.wintypes.HGDIOBJ]
    gdi32.SelectObject.restype = ctypes.wintypes.HGDIOBJ
    gdi32.GetTextMetricsW.argtypes = [ctypes.wintypes.HDC, ctypes.POINTER(TEXTMETRICW)]
    gdi32.GetTextMetricsW.restype = ctypes.wintypes.BOOL
    gdi32.GetGlyphOutlineW.argtypes = [
        ctypes.wintypes.HDC,
        ctypes.wintypes.UINT,
        ctypes.wintypes.UINT,
        ctypes.POINTER(GLYPHMETRICS),
        ctypes.wintypes.DWORD,
        ctypes.c_void_p,
        ctypes.POINTER(MAT2),
    ]
    gdi32.GetGlyphOutlineW.restype = ctypes.wintypes.DWORD
    gdi32.DeleteObject.argtypes = [ctypes.wintypes.HGDIOBJ]
    gdi32.DeleteObject.restype = ctypes.wintypes.BOOL
    gdi32.DeleteDC.argtypes = [ctypes.wintypes.HDC]
    gdi32.DeleteDC.restype = ctypes.wintypes.BOOL
    return gdi32


# ================
# native_gdi_pixel_height
# ================
def native_gdi_pixel_height(record_height):
    return round((record_height * 0x60) / 0x48)


# ================
# create_font
# ================
def create_font(gdi32, record_height, style):
    logfont = LOGFONTW()
    logfont.lfHeight = -native_gdi_pixel_height(record_height)
    logfont.lfWidth = 0
    logfont.lfEscapement = 0
    logfont.lfOrientation = 0
    logfont.lfWeight = style["weight"]
    logfont.lfItalic = style["italic"]
    logfont.lfUnderline = style["underline"]
    logfont.lfStrikeOut = 0
    logfont.lfCharSet = ANSI_CHARSET
    logfont.lfOutPrecision = OUT_DEFAULT_PRECIS
    logfont.lfClipPrecision = CLIP_DEFAULT_PRECIS
    logfont.lfQuality = DEFAULT_QUALITY
    logfont.lfPitchAndFamily = DEFAULT_PITCH
    logfont.lfFaceName = FACE_NAME

    handle = gdi32.CreateFontIndirectW(ctypes.byref(logfont))
    if not handle:
        raise ctypes.WinError(ctypes.get_last_error())
    return handle


# ================
# identity_mat2
# ================
def identity_mat2():
    mat = MAT2()
    mat.eM11.value = 1
    mat.eM22.value = 1
    return mat


# ================
# packed_glyph_bit_is_set
# ================
def packed_glyph_bit_is_set(buffer, row_bytes, x, y):
    byte_value = buffer[y * row_bytes + x // 8]
    return (byte_value & (0x80 >> (x % 8))) != 0


# ================
# read_glyph
# ================
def read_glyph(gdi32, hdc, codepoint):
    mat = identity_mat2()
    metrics = GLYPHMETRICS()
    byte_count = gdi32.GetGlyphOutlineW(hdc, codepoint, GGO_BITMAP, ctypes.byref(metrics), 0, None, ctypes.byref(mat))
    if byte_count == GDI_ERROR:
        raise ctypes.WinError(ctypes.get_last_error())

    buffer = (ctypes.c_ubyte * max(1, byte_count))()
    if byte_count:
        result = gdi32.GetGlyphOutlineW(
            hdc,
            codepoint,
            GGO_BITMAP,
            ctypes.byref(metrics),
            byte_count,
            ctypes.byref(buffer),
            ctypes.byref(mat),
        )
        if result == GDI_ERROR:
            raise ctypes.WinError(ctypes.get_last_error())

    black_box_width = int(metrics.gmBlackBoxX)
    black_box_height = int(metrics.gmBlackBoxY)
    width = max(1, black_box_width)
    height = max(1, black_box_height)
    pixels = []

    if black_box_width > 0 and black_box_height > 0:
        row_bytes = ((black_box_width + 31) // 32) * 4
        for y in range(black_box_height):
            for x in range(black_box_width):
                if packed_glyph_bit_is_set(buffer, row_bytes, x, y):
                    pixels.append((x, y))

    return {
        "codepoint": codepoint,
        "width": width,
        "height": height,
        "originX": int(metrics.gmptGlyphOrigin.x),
        "originY": int(metrics.gmptGlyphOrigin.y),
        "advanceX": int(metrics.gmCellIncX),
        "pixels": pixels,
    }


# ================
# render_style
# ================
def render_style(gdi32, font_record, style):
    hdc = gdi32.CreateCompatibleDC(None)
    if not hdc:
        raise ctypes.WinError(ctypes.get_last_error())

    hfont = create_font(gdi32, font_record["recordHeight"], style)
    old_font = gdi32.SelectObject(hdc, hfont)
    try:
        metrics = TEXTMETRICW()
        if not gdi32.GetTextMetricsW(hdc, ctypes.byref(metrics)):
            raise ctypes.WinError(ctypes.get_last_error())

        glyphs = [read_glyph(gdi32, hdc, codepoint) for codepoint in [*CODEPOINTS, *EXTRA_CODEPOINTS]]
        return {
            "recordHeight": font_record["recordHeight"],
            "pixelHeight": native_gdi_pixel_height(font_record["recordHeight"]),
            "metricsHeight": int(metrics.tmHeight),
            "ascent": int(metrics.tmAscent),
            "descent": int(metrics.tmDescent),
            "aveCharWidth": int(metrics.tmAveCharWidth),
            "maxCharWidth": int(metrics.tmMaxCharWidth),
            "weight": int(metrics.tmWeight),
            "styleSlot": style["slot"],
            "styleName": style["name"],
            "glyphs": glyphs,
        }
    finally:
        if old_font:
            gdi32.SelectObject(hdc, old_font)
        gdi32.DeleteObject(hfont)
        gdi32.DeleteDC(hdc)


# ================
# pack_glyphs
# ================
def pack_glyphs(fonts):
    x = GLYPH_PADDING
    y = GLYPH_PADDING
    row_height = 0

    # Two passes: the basic range first, EXTRA_CODEPOINTS after all of it, so
    # adding a codepoint never shifts a pre-existing glyph's packed position.
    for extra_pass in (False, True):
        for font in fonts.values():
            for style in font["styles"].values():
                for codepoint_key, glyph in style["glyphs"].items():
                    if (int(codepoint_key) in EXTRA_CODEPOINTS) != extra_pass:
                        continue
                    if x + glyph["width"] + GLYPH_PADDING > ATLAS_WIDTH:
                        x = GLYPH_PADDING
                        y += row_height + GLYPH_PADDING
                        row_height = 0

                    glyph["x"] = x
                    glyph["y"] = y
                    x += glyph["width"] + GLYPH_PADDING
                    row_height = max(row_height, glyph["height"])

    atlas_height = max(1, y + row_height + GLYPH_PADDING)
    image = Image.new("RGBA", (ATLAS_WIDTH, atlas_height), (0, 0, 0, 0))

    for font in fonts.values():
        for style in font["styles"].values():
            for glyph in style["glyphs"].values():
                for px, py in glyph.pop("pixels"):
                    image.putpixel((glyph["x"] + px, glyph["y"] + py), (255, 255, 255, 255))

    return image


# ================
# style_to_json
# ================
def style_to_json(style):
    return {
        "recordHeight": style["recordHeight"],
        "pixelHeight": style["pixelHeight"],
        "metricsHeight": style["metricsHeight"],
        "ascent": style["ascent"],
        "descent": style["descent"],
        "aveCharWidth": style["aveCharWidth"],
        "maxCharWidth": style["maxCharWidth"],
        "weight": style["weight"],
        "styleSlot": style["styleSlot"],
        "styleName": style["styleName"],
        "glyphs": style["glyphs"],
    }


# ================
# main
# ================
def main():
    if sys.platform != "win32":
        raise SystemExit("native_ui_font_atlas.py must run on Windows so it can use Win32 GDI.")
    if len(sys.argv) != 3:
        raise SystemExit("usage: native_ui_font_atlas.py <atlas.json> <atlas.png>")

    json_path = Path(sys.argv[1])
    image_path = Path(sys.argv[2])
    gdi32 = configure_gdi()
    fonts = {}

    for font_record in FONT_RECORDS:
        styles = {}
        for style in STYLE_SLOTS:
            rendered = render_style(gdi32, font_record, style)
            rendered["glyphs"] = {str(glyph.pop("codepoint")): glyph for glyph in rendered["glyphs"]}
            styles[str(style["slot"])] = rendered
        normal = styles["0"]
        fonts[str(font_record["fontIndex"])] = {
            "recordHeight": normal["recordHeight"],
            "pixelHeight": normal["pixelHeight"],
            "metricsHeight": normal["metricsHeight"],
            "ascent": normal["ascent"],
            "descent": normal["descent"],
            "aveCharWidth": normal["aveCharWidth"],
            "maxCharWidth": normal["maxCharWidth"],
            "weight": normal["weight"],
            "role": font_record["role"],
            "glyphs": normal["glyphs"],
            "availableStyleSlots": [style["slot"] for style in STYLE_SLOTS],
            "styles": styles,
        }

    image = pack_glyphs(fonts)
    atlas = {
        "source": "SRO_Client.exe v1.150 native font path: sub_a122b0 -> sub_a17690 -> GetGlyphOutlineW(GGO_BITMAP)",
        "image": "/assets/fonts/native-ui-font-atlas.png",
        "face": FACE_NAME,
        "styleSlots": {
            "0": "normal",
            "2": "bold",
        },
        "atlasWidth": image.width,
        "atlasHeight": image.height,
        "fonts": {},
    }

    for key, font in fonts.items():
        atlas["fonts"][key] = {
            "recordHeight": font["recordHeight"],
            "pixelHeight": font["pixelHeight"],
            "metricsHeight": font["metricsHeight"],
            "ascent": font["ascent"],
            "descent": font["descent"],
            "aveCharWidth": font["aveCharWidth"],
            "maxCharWidth": font["maxCharWidth"],
            "weight": font["weight"],
            "role": font["role"],
            "glyphs": font["glyphs"],
            "availableStyleSlots": font["availableStyleSlots"],
            "styles": {style_key: style_to_json(style) for style_key, style in font["styles"].items()},
        }

    json_path.parent.mkdir(parents=True, exist_ok=True)
    image_path.parent.mkdir(parents=True, exist_ok=True)

    # Write-if-changed (no trailing newline: the JSON optimizer strips it in place anyway,
    # so writing it back would defeat the byte comparison forever). Stable mtimes keep the
    # sidecar/pack caches from recompressing these files on every rebuild. The build
    # pipeline (fontResources.mjs buildNativeUiFontAtlas) passes .tmp sibling paths and
    # publishes via publishFileFromTemp, so this script never opens a dev-server-held
    # served file for write; run standalone with the final paths, this comparison is
    # what keeps the served mtimes stable instead.
    png_buffer = BytesIO()
    image.save(png_buffer, format="PNG")
    png_bytes = png_buffer.getvalue()
    # Bind coordinates to exact pixels. Group refreshes must validate this
    # dependency before replacing the complete pack index, even at equal size.
    import hashlib
    atlas["imageSha256"] = hashlib.sha256(png_bytes).hexdigest()
    json_bytes = json.dumps(atlas, separators=(",", ":")).encode("utf-8")

    wrote = []
    if not _file_matches(json_path, json_bytes):
        json_path.write_bytes(json_bytes)
        wrote.append("json")
    if not _file_matches(image_path, png_bytes):
        image_path.write_bytes(png_bytes)
        wrote.append("png")
    status = "+".join(wrote) if wrote else "unchanged"
    print(f"Built native UI font atlas ({status}): {json_path} + {image_path} ({image.width}x{image.height})")


# ================
# _file_matches
# ================
def _file_matches(target: Path, data: bytes) -> bool:
    try:
        return target.read_bytes() == data
    except OSError:
        return False


if __name__ == "__main__":
    main()
