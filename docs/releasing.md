# Releasing

GitHub Actions publishes binaries and the container image from a semantic version tag.

1. Ensure `make verify` passes on `main`.
2. Create and push an annotated tag such as `v0.1.0`.
3. The release workflow runs GoReleaser and attaches platform archives plus `checksums.txt` to the GitHub Release.
4. The image job publishes Linux amd64 and arm64 manifests to `ghcr.io/${github.repository}` with full, major/minor, and major semantic-version tags.

The workflow uses the repository-scoped `GITHUB_TOKEN`; it requires `contents: write` for release assets and `packages: write` for GHCR. Package visibility and access inherit repository/package settings on GitHub.

The canonical module and remote are `github.com/rkthtrifork/gitops-local-render` and `git@github.com:rkthtrifork/gitops-local-render.git`. Changing the module path after consumers import public contracts under `pkg/api` is a breaking change.
