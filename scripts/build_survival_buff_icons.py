"""
===========================================================================
build_survival_buff_icons.py - authored flat glyphs for the small arena HUD.

Pillow is already pinned by the asset-tool environment. Large silhouettes,
one accent color and a dark background remain legible at 24px.
===========================================================================
"""
from pathlib import Path
from PIL import Image, ImageDraw


# ================
# build_icons - draw at 4x resolution, then antialias the native 64px output.
# ================
def build_icons():
    root = Path(__file__).resolve().parents[1] / "docs/features/assets/survival"
    palettes = {
        "damage-surge": "#ff704a", "unstoppable": "#ffd86b",
        "unkillable": "#80edae", "arcane-spring": "#66caff", "fire-pit": "#ffa13b", "mirror-shell": "#d7b2ff",
        "chain-reaction": "#6ee7ec", "fleetfoot": "#ace57a",
    }
    for name, color in palettes.items():
        image = Image.new("RGBA", (256, 256), "#10151f")
        draw = ImageDraw.Draw(image)
        draw.rounded_rectangle((7, 7, 248, 248), 23, outline="#ab9262", width=10)
        if name == "damage-surge":
            draw.polygon([(144, 37), (72, 139), (117, 139), (101, 222), (187, 105), (140, 105), (159, 37)], fill=color)
        elif name == "unstoppable":
            draw.polygon([(103, 38), (154, 38), (142, 124), (198, 157), (204, 200), (64, 200), (64, 164), (91, 139)], fill=color)
            draw.line([(58, 76), (84, 76)], fill=color, width=11)
            draw.line([(47, 105), (78, 105)], fill=color, width=11)
        elif name == "unkillable":
            draw.polygon([(128, 37), (201, 63), (191, 151), (166, 187), (128, 218), (90, 187), (65, 151), (55, 63)], fill=color)
            draw.line([(128, 76), (128, 169)], fill="#10151f", width=16)
            draw.line([(93, 119), (163, 119)], fill="#10151f", width=16)
        elif name == "arcane-spring":
            draw.polygon([(128, 34), (70, 121), (58, 155), (66, 188), (94, 215), (128, 224), (162, 215), (190, 188), (198, 155), (186, 121)], fill=color)
            draw.arc((82, 119, 173, 204), 25, 155, fill="#d3f3ff", width=10)
        elif name == "chain-reaction":
            draw.line([(70, 155), (128, 84), (190, 155)], fill=color, width=17)
            for x, y in [(70, 155), (128, 84), (190, 155)]:
                draw.ellipse((x-26, y-26, x+26, y+26), fill=color)
                draw.ellipse((x-11, y-11, x+11, y+11), fill="#10151f")
        elif name == "fleetfoot":
            draw.polygon([(128, 72), (157, 92), (144, 145), (194, 176), (199, 200), (98, 200), (87, 179), (108, 141)], fill=color)
            for y in [77, 112, 147]:
                draw.line([(45, y), (87, y)], fill=color, width=12)
        elif name == "mirror-shell":
            draw.polygon([(128, 37), (201, 63), (191, 151), (166, 187), (128, 218), (90, 187), (65, 151), (55, 63)], fill=color)
            draw.line([(87, 133), (164, 133)], fill="#20162e", width=14)
            draw.line([(141, 107), (167, 133), (141, 160)], fill="#20162e", width=14)
            draw.line([(115, 82), (87, 109), (115, 136)], fill="#20162e", width=12)
        else:
            draw.polygon([(137, 31), (155, 94), (181, 71), (204, 149), (192, 193), (163, 219), (120, 224), (77, 209), (52, 173), (62, 124), (89, 93), (88, 140), (116, 107)], fill=color)
            draw.polygon([(130, 129), (159, 181), (149, 211), (117, 214), (99, 193), (109, 164)], fill="#fff0a3")
        image.resize((64, 64), Image.Resampling.LANCZOS).save(root / f"{name}.png")
        assert image.getbbox() == (0, 0, 256, 256)
    print("Built 8 survival HUD icons (64x64).")


if __name__ == "__main__":
    build_icons()
