"""
===========================================================================

module_inputs.py - the server module's deployer inputs, replaced and undone

A deployment replaces the module's inputs (the binaries, go.mod and the
Nomad job specs, bundle.FILES) before it validates anything: the staged
sro-nomad is the verdict on its own arguments. copy_inputs installs a
release's set; snapshot_inputs and restore_inputs undo it when the
deployment refuses before the fleet is touched, so the module never holds
one release's bytes under another release's release.json (2026-10-27).
deploy.py owns when each runs; this module owns only the files.

===========================================================================
"""

import os
from pathlib import Path
import shutil
import tempfile

from bundle import FILES


# ================
# copy_inputs
#
# Task binaries already live in Nomad's immutable release tree. Replacing
# deployer inputs therefore does not modify a running executable. A file the
# deployed release does not carry (a revert to a release built before it was
# added) is removed, so the module holds exactly that release's inputs.
# ================
def copy_inputs(source, module, names):
	for name in names:
		target = module / name
		target.parent.mkdir(parents=True, exist_ok=True)
		temporary = target.with_name(target.name + ".incoming")
		shutil.copyfile(source / name, temporary)
		temporary.chmod(0o755 if FILES[name].startswith("bin/") else 0o644)
		os.replace(temporary, target)
	for name in set(FILES) - set(names):
		(module / name).unlink(missing_ok=True)


# ================
# snapshot_inputs
#
# The module's deployer inputs as they are before copy_inputs replaces
# them: each FILES name present is copied, with its mode, into a private
# directory beside the module (the same filesystem); a name that is absent
# is recorded as absent. restore_inputs puts exactly this set back.
# ================
def snapshot_inputs(module):
	saved = Path(tempfile.mkdtemp(prefix=".inputs-", dir=module.parent))
	saved.chmod(0o700)
	present = []
	for name in FILES:
		source = module / name
		if source.is_file():
			destination = saved / name
			destination.parent.mkdir(parents=True, exist_ok=True)
			shutil.copy2(source, destination)
			present.append(name)
	return saved, present


# ================
# restore_inputs
#
# Undo copy_inputs from a snapshot_inputs result: the module ends with the
# inputs it had before, bytes and modes, and nothing the deployment added.
# release.json is written only after a deployment succeeds, so the inputs
# match the release it records again (retain_server, 2026-10-27).
# ================
def restore_inputs(module, snapshot):
	saved, present = snapshot
	for name in FILES:
		target = module / name
		if name in present:
			temporary = target.with_name(target.name + ".restoring")
			shutil.copy2(saved / name, temporary)
			os.replace(temporary, target)
		else:
			target.unlink(missing_ok=True)
