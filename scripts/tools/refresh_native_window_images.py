"""
===========================================================================

refresh_native_window_images.py - republish native-window RGB16 textures

Re-decodes the native-window DDJ families whose RGB16 payloads the generic
converter cannot express and republishes only images that already exist
(plus the dynamically created controls). Called by the pack refresh.

===========================================================================
"""
from pathlib import Path
import json
import sys
from io import BytesIO
from PIL import Image
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from convert_images import decode_native_rgb16, extract_ddj_payload
from sro_paths import EXTRACTED_ROOT, PUBLIC_ROOT
from rebuild_lock import generated_assets_lock

with generated_assets_lock('Native window RGB16 expansion'):
    changed = []
    public = PUBLIC_ROOT
    for family in ('frame', 'ifcommon', 'system', 'messagebox', 'inventory', 'equipment', 'mainpopup', 'character', 'party', 'option', 'alchemy', 'guild', 'pet', 'icon'):
        source_root = EXTRACTED_ROOT / 'Media_extracted' / ('icon' if family == 'icon' else 'interface/' + family)
        for source in sorted(source_root.glob('**/*.ddj')):
            if family == 'icon' and source.stem not in ('icon_disable', 'icon_item_broken', 'icon_item_warning'):
                continue
            relative = source.relative_to(EXTRACTED_ROOT).with_suffix('.png')
            target = public / 'assets/images' / relative
            # 5B4C30/620450/620180 create these controls outside resinfo.
            dynamic = (family in ('alchemy', 'icon') or source.stem.startswith('com_short_tab_'))
            if not target.exists() and not dynamic:
                continue
            payload = extract_ddj_payload(source)
            image = decode_native_rgb16(payload)
            if image is None and dynamic:
                image = Image.open(BytesIO(payload))
            if image is None:
                continue
            data = BytesIO()
            image.save(data, 'PNG')
            content = data.getvalue()
            # Include corrected files on repeat runs so an interrupted pack refresh recovers.
            if not target.exists() or target.read_bytes() != content:
                target.parent.mkdir(parents=True, exist_ok=True)
                temporary = target.with_suffix('.png.native-window-tmp')
                temporary.write_bytes(content)
                temporary.replace(target)
            changed.append('/' + target.relative_to(public).as_posix())
    print(json.dumps(changed))
