# Development

## Build and test

Build the webhook image from source:

```sh
make build-image IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
make push-image  IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
```

Unit tests (no external dependency):

```sh
make unit-test
```

The cert-manager DNS01 conformance suite requires a real Nameshield zone and
API token, plus the envtest binaries (downloaded automatically):

```sh
export NAMESHIELD_ZONE=example.com       # a zone hosted by Nameshield
export NAMESHIELD_TOKEN=<api-token>       # token with DNS rights on that zone
make test
```

All DNS webhooks deployed publicly are expected to run and pass this suite.

Render the chart locally:

```sh
make rendered-manifest IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
```

## Documentation

This site is built with [MkDocs](https://www.mkdocs.org/) from the `docs/`
folder (`mkdocs.yml` at the repository root). To preview it locally:

```sh
pip install mkdocs==1.6.1
mkdocs serve
```

The `docs.yaml` workflow builds the site on every push/PR touching `docs/`,
`mkdocs.yml` or the workflow, and publishes it on `main` to the `docs/` folder
of the `gh-pages` branch — the same branch (and the same GitHub Pages site)
that serves the Helm chart repository from its root.

## CI and releases

Three GitHub Actions workflows are defined in `.github/workflows/`:

- **`ci.yaml`** — runs on every push and pull request on `main`:
  `gofmt` check, `go vet ./...`, unit tests (`go test ./nameshield/...`), `helm lint`
  and `helm template` on `deploy/cert-manager-webhook-nameshield`, and a Docker
  image build (load only, no push).
- **`docs.yaml`** — builds the MkDocs site and publishes it to `gh-pages` under
  `docs/` (see above).
- **`release.yaml`** — runs on push of a `v*` tag only: validates the semver tag,
  creates a GitHub release on the tag, builds and pushes the multi-arch image to
  GHCR (`<version>`, `<major>.<minor>`, `<major>`, `latest` on stable releases,
  `sha-<short-sha>`), sets the chart `version`/`appVersion` to the tag version
  and publishes the chart to the `gh-pages` branch with
  [helm/chart-releaser-action](https://github.com/helm/chart-releaser-action)
  (`index.yaml` at the branch root, one GitHub release per chart version carrying
  the `.tgz` asset).

All workflows rely on the default `GITHUB_TOKEN` only — no extra secrets are
required.

### Cutting a release

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

Everything is aligned on the same release version: the Docker tag `X.Y.Z`, the
chart `version`/`appVersion` `X.Y.Z`, and the chart's default `image.tag`
(which equals `appVersion`, i.e. `X.Y.Z`).

### GitHub Pages setup

In *Settings → Pages*, set *Build and deployment* to *Deploy from a branch* and
select the `gh-pages` branch with the `/ (root)` folder. The site then serves,
from the same branch:

- the Helm chart repository at `https://dotinfra.github.io/cert-manager-webhook-nameshield/index.yaml`;
- the documentation at `https://dotinfra.github.io/cert-manager-webhook-nameshield/docs/`.

The `gh-pages` branch itself is created automatically (as an empty orphan
branch) by the release workflow on the first chart publication.

## Project layout

```
.github/workflows/           CI, docs and release workflows
main.go                      Entrypoint, registers the solver
nameshield/solver.go         webhook.Solver: Present / CleanUp / Initialize
nameshield/client.go         Nameshield DNS API v4 client (records, validation)
nameshield/config.go         Solver configuration schema
nameshield/solver_test.go    Unit tests
main_test.go                 cert-manager DNS01 conformance suite
testdata/nameshield/         Conformance suite solver configuration
docs/                        MkDocs documentation sources
mkdocs.yml                   MkDocs site configuration
deploy/...                   Helm chart
Dockerfile                   Multi-stage build
```
