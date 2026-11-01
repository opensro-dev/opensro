# Client gates

- **gate** Design a new module against `ownership.json` and `execution-contract.json`, and run `verify:quick` after
  every new module or call edge.
- **gate** Shared module state is an immutable scalar or a readonly literal table.
- **review** A client contract change runs the whole client check before its PR.
- **review** A device- or flag-gated path needs a test that runs that path, not only one that picks it.
- **review** Removing an Experimental option, or changing its default, also resets what players saved.
- **review** Never destroy a GPU resource directly. Retire it through the device's retirement path.
- **review** Performance work that changes the picture needs the owner's approval. By default it changes nothing visible.
- **review** Do not write an asset format `main` cannot read into the shared `.generated`. Add fields compatibly, or
  build into a private `SRO_GENERATED_ROOT`.
- **review** After merging a PR that touches the asset build, rebuild the assets.
