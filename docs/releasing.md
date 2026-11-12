# Releasing

A release is a version tag. Pushing `vX.Y.Z` runs `.github/workflows/release.yml`,
which tests the source, builds all seven targets, packages archives with
SHA-256 checksums, and publishes a GitHub release. It uses the repository's
ordinary Actions token, and it only runs in a public repository.

## Cutting a release

1. Set `VERSION` to the new version, without the leading `v`, and merge it.
   The tag must match it, or the workflow stops.
2. If `go.mod` or `go.sum` changed since the last release, recompute
   `vendorHash` in `nix/source.nix`. The hash covers the Go vendor tree, not
   the release binary.
3. Tag the merge commit and push the tag:

   ```sh
   git tag v1.0.0
   git push origin v1.0.0
   ```

4. When the release is published, update the two package channels below with
   the release's generated `evado.rb` and `release.json`.

The same packaging can be run locally:

```sh
EVADO_BUILD_VERSION=1.0.0 mise run build-all
mise exec -- python3 scripts/third-party-notices.py > dist/THIRD_PARTY_NOTICES.txt
python3 scripts/package-release.py 1.0.0 \
  --repository nerdlabz/evado-tui \
  --notices dist/THIRD_PARTY_NOTICES.txt --output dist/release-1.0.0
```

The packager takes already-built binaries; it never invokes a compiler. It
refuses an existing output directory, requires all seven binaries, and writes
only the executable, the license and dependency notices into each archive.
Archives use fixed timestamps, so the same binaries give the same archives.
`SHA256SUMS` covers the archives, the generated Homebrew formula and the Nix
release manifest.

## Homebrew

The formula lives in [nerdlabz/homebrew-tap](https://github.com/nerdlabz/homebrew-tap)
at `Formula/evado.rb`. Replace it with the release's generated `evado.rb`
through a PR. `packaging/homebrew/evado.rb.in` is the template it comes from.

```sh
brew install nerdlabz/tap/evado
brew test nerdlabz/tap/evado
evado -version
```

## Nix

Commit the release's generated `release.json` to `packaging/nix/release.json`
through a PR. It binds each platform to its archive URL and SHA-256. The flake
in `packaging/nix` installs those binaries; the root flake builds from source.

```sh
nix run 'github:nerdlabz/evado-tui?dir=packaging/nix' -- -version
nix build github:nerdlabz/evado-tui#evado
```

The lock files pin nixpkgs 26.05, which still supports Intel Macs. Before
moving to a newer nixpkgs, check all four platforms. Evaluate without building
using `nix flake check --all-systems --no-build`.
