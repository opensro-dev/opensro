# Accepted vulnerability-scanner advisories

Dispositions for advisories that vulnerability scanners (osv-scanner,
govulncheck, Dependabot-style tooling) will keep reporting against this
module and that are **accepted, with an enforceable guard** — so the next
scanner run is answered by this file instead of a re-investigation.

Scope discipline: an entry here must be (a) narrowly scoped — never a
blanket suppression of a whole dependency, (b) backed by a guard that FAILS
the test suite if the accepting condition stops holding, and (c) dated.
Anything else is a real finding; fix it, don't list it.

---

## GO-2026-5932 — `golang.org/x/crypto/openpgp` (accepted 2026-07-29)

**What scanners report:** `golang.org/x/crypto/openpgp` is unmaintained and
unsafe by design. The advisory matches **every version** of
`golang.org/x/crypto`; there is **no fixed version to upgrade to** — the
subpackage is frozen and deprecated, not patched.

**Why it does not apply here:**

- `golang.org/x/crypto` is a direct module dependency because the account and
  provisioning paths use its maintained `bcrypt` package. The advisory is
  specifically about the separate `openpgp` package; depending on one package
  in a Go module does not compile every sibling package into the application.
- Nothing in this module or its build closure imports
  `golang.org/x/crypto/openpgp` or any subpackage of it. Go compiles and
  links per-package: an un-imported subpackage contributes zero code to the
  binary. The advisory is module-level scanner granularity, not exposure.

**Why it stays answered (the guard):** `internal/gates/openpgp_guard_test.go` runs
with the normal suite and fails if the openpgp subtree ever appears, via two
layers. It lives in the import-free `internal/gates` package, so the guard keeps
running even while the module itself does not compile:

1. a source walk parsing the imports of every `.go` file in the module
   (test files included) — catches a direct import;
2. `go list -deps ./...` over the full build closure — catches a transitive
   introduction through a dependency.

The guard is scoped to `golang.org/x/crypto/openpgp[/...]` **only**. It is
not a suppression of `x/crypto`: a genuine future advisory against, say,
`x/crypto/ssh` must and will still flag in scans and must be handled on its
own merits.

**Re-open when:** the guard fires (someone actually needs openpgp — pick a
maintained replacement such as ProtonMail's openpgp fork instead), or a
scanner reports an x/crypto advisory that is *not* GO-2026-5932.
