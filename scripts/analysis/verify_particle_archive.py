"""
===========================================================================

verify_particle_archive.py - certify Particles.pk2 against the extraction

Read-only v1.150 PK2 directory and extracted-EFP verification. No basename
guessing: a complete chained directory walk with range, cycle and duplicate
guards (scripts/sro_pk2.py), archive and entry hashes, and a byte comparison
with every extracted EFP. The certificate it prints must equal the committed
scripts/build/reference/particle-archive.json; --write re-freezes it. It never
executes the original client or modifies its PK2. Paths come from
scripts/sro_paths.py.

===========================================================================
"""
import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import sro_pk2  # noqa: E402
from sro_paths import EXTRACTED_ROOT, GAME_ROOT, REPO_ROOT as ROOT  # noqa: E402

# ================
# inventory
#
# Files keyed by ASCII-folded archive path, and the directory blocks.
# ================
def inventory(data):
    blocks = []
    entries = sro_pk2.read_directory(data, blocks)
    files = {sro_pk2.fold_ascii(e.path): {'offset': e.offset, 'size': e.size,
        'sha256': sro_pk2.digest(sro_pk2.payload(data, e))} for e in entries}
    return files, blocks

# ================
# main
# ================
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--write', action='store_true')
    args = parser.parse_args()
    archive = GAME_ROOT/'Particles.pk2'
    data = archive.read_bytes()
    files, blocks = inventory(data)
    extracted = EXTRACTED_ROOT/'Particles_extracted'
    mismatches = []
    for name, row in files.items():
        if not name.endswith('.efp'):
            continue
        target = extracted/name
        if not target.exists() or sro_pk2.digest(target.read_bytes()) != row['sha256']:
            mismatches.append(name)
    if mismatches:
        raise ValueError('Extraction differs from archive: '+repr(mismatches))
    programs = json.loads((ROOT/'.generated/client-public/assets/effects/programs.json').read_text())
    referenced = {r['effectPath'] for r in programs['reachability']['entityParticleReferences']}
    absent = sorted(name for name in referenced if name not in files)
    certificate = {'format': 'sro-particle-archive-v1', 'archive': archive.name,
        'sha256': sro_pk2.digest(data), 'bytes': len(data), 'directoryBlocks': len(blocks),
        'directoryHash': sro_pk2.digest(json.dumps(blocks, sort_keys=True).encode()),
        'files': len(files), 'verifiedExtractedEfp': sum(name.endswith('.efp') for name in files),
        'absent': absent}
    target = ROOT/'scripts/build/reference/particle-archive.json'
    if args.write:
        target.write_text(json.dumps(certificate, indent=2)+'\n')
    elif json.loads(target.read_text()) != certificate:
        raise ValueError('Archive certificate drift; investigate before regenerating')
    print(json.dumps(certificate, indent=2))

if __name__ == '__main__':
    main()
