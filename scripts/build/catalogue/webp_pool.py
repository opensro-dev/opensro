#
# ===========================================================================
#
# webp_pool.py - the catalogue's WebP encodes: in parallel, each image once
#
# The catalogue writes thousands of WebP files (item and skill icons, atlas
# tiles, monster portraits), nearly all identical from one release to the
# next, and many identical to each other (open-sea atlas tiles).
# encode_pairs groups [source, target] pairs by content, encodes each
# distinct image once on SRO_BUILD_JOBS processes, and keeps the result in
# a content cache keyed by the source's bytes, the encoder's settings and
# Pillow's version: an unchanged image is a file copy. A cache entry is
# written through a temporary file and a rename, so a killed build never
# leaves a partial entry behind.
#
# ===========================================================================
#
import hashlib, os, shutil, sys, tempfile
from multiprocessing import Pool
from pathlib import Path

import PIL

# scripts/ holds the build's shared Python (build_parallelism.py).
sys.path.insert( 0, str( Path( __file__ ).resolve().parents[2] ) )
from build_parallelism import build_jobs

# CHUNK_PAIRS hands each worker this many pairs at a time: small enough to
# balance tiles and portraits of very different cost, large enough that the
# hand-off is not the work.
CHUNK_PAIRS = 16

#
# ================
# cache_key
#
# The cache entry's name: the encoder's settings, Pillow's version and the
# source's bytes, so a change to any of them makes a new entry.
# ================
#
def cache_key( settings, payload ):
	digest = hashlib.sha256()
	digest.update( ( settings + "\n" + PIL.__version__ + "\n" ).encode( "utf-8" ) )
	digest.update( payload )
	return digest.hexdigest()

#
# ================
# encode_one
#
# One distinct image, in a worker: the cached result when there is one,
# else the encode, stored in the cache, then copied to every target that
# shares its bytes. encode_pairs hands each cache entry to exactly one
# worker, so no two workers ever write or read the same entry at once
# (Windows refuses a rename onto a file another process holds open).
# Returns whether the cache answered.
# ================
#
def encode_one( job ):
	encode, cache_dir, key, source, targets = job
	for target in targets:
		os.makedirs( os.path.dirname( target ), exist_ok=True )
	entry = os.path.join( cache_dir, key + ".webp" ) if cache_dir else None
	hit = entry is not None and os.path.isfile( entry )
	if not hit:
		with open( source, "rb" ) as stream:
			payload = stream.read()
		entry = entry or targets[0]
		handle, temporary = tempfile.mkstemp( suffix=".webp", dir=os.path.dirname( entry ) )
		os.close( handle )
		try:
			encode( payload, temporary )
			os.replace( temporary, entry )
		except BaseException:
			if os.path.exists( temporary ):
				os.remove( temporary )
			raise
	for target in targets:
		if target != entry:
			shutil.copyfile( entry, target )
	return hit

#
# ================
# encode_pairs
#
# Encodes every [source, target] pair with encode(payload, path), a
# module-level function (workers import it), on SRO_BUILD_JOBS processes. Sources with
# the same bytes are encoded once (many atlas tiles are identical sea).
# Returns the written and cached counts.
# ================
#
def encode_pairs( pairs, encode, settings, cache_dir ):
	if cache_dir:
		os.makedirs( cache_dir, exist_ok=True )
	groups = {}
	for source, target in pairs:
		with open( source, "rb" ) as stream:
			key = cache_key( settings, stream.read() )
		groups.setdefault( key, [ source, [] ] )[1].append( target )
	jobs = [ ( encode, cache_dir, key, source, targets ) for key, ( source, targets ) in groups.items() ]
	if not jobs:
		return { "written": 0, "cached": 0 }
	with Pool( min( build_jobs(), len( jobs ) ) ) as pool:
		hits = pool.map( encode_one, jobs, chunksize=CHUNK_PAIRS )
	cached = sum( len( job[4] ) for job, hit in zip( jobs, hits ) if hit )
	return { "written": len( pairs ), "encoded": len( jobs ) - sum( hits ), "cached": cached }
