"""
===========================================================================

rz_function_index.py - the rizin function index revtool's evidence reads

Writes function-index.json (schema sro-native-exact-function-index-v1)
from a saved rizin project, through exact_rebuild's own export and
normalisation, without running its byte-reconstruction pipeline.

===========================================================================
"""
import argparse
import hashlib
import json
import struct
import sys
from pathlib import Path


def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--exact-rebuild", type=Path, required=True)
	parser.add_argument("--rizin", type=Path, required=True)
	parser.add_argument("--project", type=Path, required=True)
	parser.add_argument("--binary", type=Path, required=True)
	parser.add_argument("--out", type=Path, required=True)
	args = parser.parse_args()
	sys.path.insert(0, str(args.exact_rebuild))
	import exact_rebuild as er
	original = args.binary.read_bytes()
	target = hashlib.sha256(original).hexdigest()
	pe = struct.unpack_from("<I", original, 0x3C)[0]
	count = struct.unpack_from("<H", original, pe + 6)[0]
	optional = struct.unpack_from("<H", original, pe + 20)[0]
	base = struct.unpack_from("<I", original, pe + 24 + 28)[0]
	for index in range(count):
		entry = pe + 24 + optional + index * 40
		if original[entry:entry + 8].rstrip(b"\0") == b".text":
			virtual_size, rva, raw_size, raw_offset = struct.unpack_from("<IIII", original, entry + 8)
			break
	else:
		raise SystemExit("no .text section")
	text_va = base + rva
	text_size = raw_size
	text = original[raw_offset:raw_offset + raw_size]
	raw = er.run_rizin_function_export(args.rizin, args.project)
	functions, summary = er.normalize_functions(raw, text_va, text_size, text)
	index = {
		"schema": "sro-native-exact-function-index-v1",
		"target_sha256": target,
		"rizin_project": str(args.project),
		"rizin_project_sha256": hashlib.sha256(args.project.read_bytes()).hexdigest(),
		"text_va": text_va,
		"text_size": text_size,
		"summary": summary,
		"functions": functions,
	}
	args.out.write_text(json.dumps(index, indent=1), encoding="utf-8")
	print(json.dumps(summary))


if __name__ == "__main__":
	main()
