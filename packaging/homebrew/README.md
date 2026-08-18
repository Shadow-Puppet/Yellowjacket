# YellowJacket Homebrew formula

The formula builds YellowJacket from source on macOS and Linuxbrew, mirroring
the Arch `PKGBUILD`: the Wails toolchain resolves from `go.mod`'s tool
directives and drives the frontend build itself, so the only build inputs are
Go, Node, and pnpm.

```
packaging/homebrew/
└── Formula/
    └── yellowjacket.rb   ← canonical source; CI syncs it to the tap repo
```

## Installing

```bash
brew install shadow-puppet/yellowjacket/yellowjacket
```

`shadow-puppet/yellowjacket` is Homebrew shorthand for the tap repo
`github.com/Shadow-Puppet/homebrew-yellowjacket`. Brew auto-taps it, so there's
no separate `brew tap` step. To build the tip of `main` instead of the latest
release, add `--HEAD`.

### Upgrading from 1.x needs a reinstall, once

Releases became automatic and restarted at **0.0.1** (plan 017), which is
*lower* than the `1.3.0` this tap last published. Homebrew compares versions
and has no equivalent of pacman's `epoch`, so `brew upgrade` sees a downgrade
and offers **nothing at all** — silently, which is indistinguishable from the
tap having gone stale.

```bash
brew uninstall yellowjacket && brew install shadow-puppet/yellowjacket/yellowjacket
```

Nothing is stored inside the Cellar, so this costs a rebuild and no data. It is
a one-time step: 0.0.2 onwards upgrade normally.

## How publishing works

This directory holds the **canonical** formula. The tap users install from lives
in a **separate** repo — `homebrew-yellowjacket` — because Homebrew only
discovers formulae from a repo whose name starts with `homebrew-`, with the
formula at a top-level `Formula/`. Keeping it separate is also why nothing has
to live in this repo's root.

On every version tag (`v*`), `.gitea/workflows/homebrew-formula.yml`:

1. downloads the GitHub release tarball for that tag,
2. computes its `sha256`,
3. rewrites the `version` and `sha256` lines in the formula, and
4. commits the result to `homebrew-yellowjacket`'s `Formula/yellowjacket.rb`.

So a normal release needs **no manual formula edits** — tag, and the tap updates
itself. (This is the Homebrew equivalent of the Arch package's publish workflow.)

### About the `sha256`

Homebrew re-downloads the source tarball on each install and refuses to build
unless its checksum matches `sha256` — integrity/tamper detection. The committed
value here is a `REPLACE_WITH_...` placeholder on purpose; the real checksum is
computed and injected by CI at release time, so it never has to be maintained by
hand. (The Arch `PKGBUILD` sidesteps this with `SKIP` because it clones over git
rather than downloading a tarball.)

## One-time setup

1. **Create the tap repo:** `Shadow-Puppet/homebrew-yellowjacket` on GitHub,
   with a `main` branch. It can start empty — the first tagged release seeds
   `Formula/yellowjacket.rb`.
2. **Add a CI secret:** `HOMEBREW_TAP_TOKEN` — a GitHub token with write access
   to that repo (a fine-grained PAT scoped to `homebrew-yellowjacket`, Contents:
   read/write, is enough).

That's it. The source repo must be public (or the tap private with an
authenticated `brew install`) for brew to fetch the release tarball.

## Notes

- **License**: declared as `license :cannot_represent` (custom license). Replace
  with the correct SPDX identifier once the license is finalized.
- **macOS vs Linux**: under Wails v3 the bundling is a separate step from the
  build. `wails3 task build` produces a bare `yellowjacket` binary in `bin/` on
  both platforms; on macOS the formula runs `wails3 task package` instead, which
  wraps that binary in a `yellowjacket.app` (installed under the Cellar with an
  `exec` shim in `bin`).
- **Linuxbrew**: building on Linux additionally needs the system WebKitGTK/GTK
  stack (`webkitgtk-6.0`, `gtk4`, `alsa-lib`) — OS packages, not Homebrew deps.
  v2's `webkit2gtk-4.1`/`gtk3` is now only the `-tags gtk3` escape hatch. macOS
  needs only the Xcode Command Line Tools.
- **Cask alternative**: if you later ship prebuilt macOS `.dmg`/`.zip` artifacts,
  a Homebrew *cask* pointing at those installs faster than this source build.
  This formula is the source-build path.
