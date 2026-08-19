# YellowJacket Arch package

This directory holds the `PKGBUILD` and desktop entry used to build the
Arch Linux package. CI builds it on every **release tag** (see
`.gitea/workflows/arch-package.yml`) and publishes it to the Gitea Arch
package registry, from which pacman can install it directly.

Tags come from `.gitea/workflows/release.yml`, which is run by hand — it
used to fire on every push to `main`, which meant a new package per
merged PR (issue #115). A prerelease tag (`v0.4.0-beta.1`) is skipped:
the workflow's trigger is `v*` and matches one.

## Installing from the registry

The registry is public — no login required.

### 1. Import the registry signing key (one-time)

Gitea signs both the repo database and the packages with a per-instance
GPG key. Import its **public** key and locally sign it so pacman will
verify signatures:

```bash
curl -fSs "https://git.ljones.me/api/packages/yonlu/arch/repository.key" \
  | sudo pacman-key --add -
sudo pacman-key --lsign-key C061B6267CF9D820
```

- Key ID: `C061B6267CF9D820` — UID `(Arch Registry)`, RSA 2048.
- This is a public key; publishing it is expected. The private key stays
  on the Gitea server.
- The key is per-Gitea-instance, so you import it once regardless of how
  many registries on `git.ljones.me` you use.

### 2. Add the repo to `/etc/pacman.conf`

Append to the end of the file:

```ini
[stable]
SigLevel = Required
Server = https://git.ljones.me/api/packages/yonlu/arch/stable/$arch
```

- `$arch` is a literal pacman variable (expands to `x86_64`); leave it as-is.
- `[stable]` matches the `ARCH_REPO` value in the publish workflow.

### 3. Sync and install

```bash
sudo pacman -Sy yellowjacket
```

### Alternative: skip signature verification

If you'd rather not import the key, set `SigLevel = Never` instead of
`SigLevel = Required` in the repo block above. Simpler, but you lose
tamper detection, and the signing-key path is only a one-time step — so
this is not recommended.

## Notes

- Only the runtime package is published; the `-debug` package makepkg
  produces (detached symbols) is skipped by the workflow.
- Package versions come from `pkgver()` in the PKGBUILD, derived from git
  (e.g. `1.3.0.r173.g4ae5ffc-1`), so every release tag yields a new
  version.
- Repo priority: if another configured repo ever provides a package named
  `yellowjacket`, the repo listed **first** in `pacman.conf` wins. Force a
  source explicitly with `sudo pacman -S stable/yellowjacket`.
