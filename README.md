# cert-manager-webhook-nameshield

A cert-manager ACME DNS01 solver webhook for the [Nameshield DNS API](https://docs.nameshield.net/).
It lets cert-manager complete Let's Encrypt (or any ACME) `dns-01` challenges by
creating and deleting `_acme-challenge` TXT records in zones managed by Nameshield.

Based on [cert-manager/webhook-example](https://github.com/cert-manager/webhook-example).

## How it works

For each challenge, cert-manager sends a `ChallengeRequest` to the webhook, which:

1. Resolves the Nameshield API token:
   - from the Kubernetes Secret referenced by `apiTokenSecretRef`, or
   - from the `NAMESHIELD_TOKEN` environment variable when ambient credentials are allowed (test suite).
2. Determines the Nameshield zone:
   1. explicit `zoneName` from the solver configuration if set;
   2. otherwise the zone resolved by cert-manager (`resolvedZone`), when it exists in the account;
   3. otherwise discovery by probing parent names, most specific first.
3. `Present`: creates the TXT record (idempotent), then calls the zone validation
   endpoint, because the Nameshield API keeps changes pending until validated.
4. `CleanUp`: finds the TXT record by name and value, deletes it, then validates the zone again.

Nameshield API specifics handled by this webhook:

- Authentication: `Authorization: Bearer <token>`.
- TXT record data must be wrapped in double quotes.
- Changes are only applied after `POST /zones/{zone}/validate`.
- Rate limit of 600 requests per 5 minutes per token, with `Retry-After` handling and retry on 429/5xx.
- Record deletion requires the record ID, obtained by listing records.

## Solver configuration

The webhook solver configuration, set in the Issuer's `webhook.config` stanza:

| Field | Type | Description |
|---|---|---|
| `apiUrl` | string | Nameshield DNS API base URL. Default: `https://api.nameshield.net/dns/v4`. Use `https://ote-api.nameshield.net/dns/v4` for the OTE environment. |
| `apiTokenSecretRef` | object | Required. `name` and `key` of the Secret holding the API token. Optional `namespace`; defaults to the challenge namespace (the cert-manager namespace for ClusterIssuers). |
| `zoneName` | string | Optional. Forces the Nameshield zone to update, bypassing zone resolution and discovery. |
| `ttl` | int | Optional. TTL of created TXT records. Provider default when unset. |
| `includeOtherUsersChanges` | bool | Optional, default false. Set to true to also apply pending zone changes made by other users when validating. |

## Installation

Pre-built artifacts are published automatically by the CI (see
[CI and releases](#ci-and-releases)):

- container image on GitHub Container Registry:
  `ghcr.io/dotinfra/cert-manager-webhook-nameshield` (tags `<version>`,
  `<major>.<minor>`, `<major>`, `latest` on stable releases, and
  `sha-<short-sha>` — `<version>` is the release semver `X.Y.Z`, i.e. the same
  value as the chart `version`/`appVersion` of that release);
- Helm chart repository on GitHub Pages:
  `https://dotinfra.github.io/cert-manager-webhook-nameshield`.

### 1. Install the Helm chart

```sh
helm repo add cert-manager-webhook-nameshield https://dotinfra.github.io/cert-manager-webhook-nameshield
helm repo update

helm install cert-manager-webhook-nameshield cert-manager-webhook-nameshield/cert-manager-webhook-nameshield \
  --namespace cert-manager \
  --create-namespace \
  --set groupName=cert-manager-webhook-nameshield
```

The chart defaults to the published image
`ghcr.io/dotinfra/cert-manager-webhook-nameshield:<appVersion>`: the chart's
`image.tag` defaults to `.Chart.AppVersion`, so a released chart always points
at the Docker tag published for the same release (`X.Y.Z`). Override it with
`--set image.tag=<tag>`. To install from a local checkout with a self-built
image instead:

```sh
helm install cert-manager-webhook-nameshield deploy/cert-manager-webhook-nameshield \
  --namespace cert-manager \
  --set groupName=cert-manager-webhook-nameshield \
  --set image.repository=<your-registry>/cert-manager-webhook-nameshield \
  --set image.tag=<tag>
```

### 2. Create the API token Secret

The token must have DNS rights on the zones involved. For ClusterIssuers, the
Secret must exist in the cert-manager cluster resource namespace (default
`cert-manager`):

```sh
kubectl -n cert-manager create secret generic nameshield-api-token \
  --from-literal=token=<NAMESHIELD_TOKEN>
```

The `groupName` value names the Kubernetes APIService group of the webhook.
Here it is set to `cert-manager-webhook-nameshield`; it must be unique in the
cluster and stable once deployed.

By default the chart installs a Role granting the webhook read access to
Secrets in its own namespace only. If Issuers live in other namespaces and
`apiTokenSecretRef` does not specify a namespace, set
`rbac.allowClusterWideSecretAccess=true`.

### 3. Reference the solver in an Issuer

The webhook is now deployed. The `webhook` stanza of the Issuer must reference
the `groupName` configured above and the solver name `nameshield`. The Issuer
manifest itself is intentionally not provided here for now; it will be
documented when the issuer strategy is defined.

### Building the image from source

```sh
make build-image IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
make push-image  IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
```

then install the chart from the local checkout as shown above.

## CI and releases

Two GitHub Actions workflows are defined in `.github/workflows/`:

- **`ci.yaml`** — runs on every push and pull request on `main`:
  - `gofmt` check, `go vet ./...` and unit tests (`go test ./nameshield/...`;
    the cert-manager DNS01 conformance suite in the root package needs the
    envtest binaries plus real `NAMESHIELD_ZONE`/`NAMESHIELD_TOKEN`, so it only
    runs via `make test`);
  - `helm lint` and `helm template` on `deploy/cert-manager-webhook-nameshield`;
  - a Docker image build (load only, no push) to validate the `Dockerfile`.
- **`release.yaml`** — runs on push of a `v*` tag only:
  - validates that the tag is a semantic version `vX.Y.Z` (prerelease suffix
    allowed);
  - builds the image for `linux/amd64` and `linux/arm64` and pushes it to GHCR
    as `ghcr.io/dotinfra/cert-manager-webhook-nameshield` with the semver tags
    `<version>`, `<major>.<minor>`, `<major>` derived from the tag, plus
    `latest` (stable releases only) and `sha-<short-sha>`;
  - sets the chart's `version` and `appVersion` to the tag version `<version>`,
    then packages the chart with
    [helm/chart-releaser-action](https://github.com/helm/chart-releaser-action)
    and publishes it to the `gh-pages` branch (served by GitHub Pages): one
    GitHub release per chart version, and `index.yaml` regenerated on every
    release.

Both workflows rely on the default `GITHUB_TOKEN` only — no extra secrets are
required (`packages: write` for GHCR, `contents: write` for the chart
releases and the `gh-pages` branch).

### Cutting a release

Tag the repository with a semantic version and push the tag:

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

The `release.yaml` workflow then:

1. validates the tag (`vX.Y.Z`, optional prerelease suffix);
2. sets the chart `version` and `appVersion` to `X.Y.Z` and publishes the
   chart to GitHub Pages (`https://dotinfra.github.io/cert-manager-webhook-nameshield`);
3. builds the image and pushes it to GHCR with the tags `X.Y.Z`, `X.Y`, `X`,
   `latest` (stable releases only) and `sha-<short-sha>`.

Everything is aligned on the same release version: the Docker tag `X.Y.Z`, the
chart `version`/`appVersion` `X.Y.Z`, and the chart's default `image.tag`
(which equals `appVersion`, i.e. `X.Y.Z`).

### Chart `version` vs `appVersion`

The chart and the webhook image are released together in a 1:1 relationship,
so `version` and `appVersion` are kept equal: both are set to the release
version `X.Y.Z` taken from the git tag. Helm allows them to differ, but for a
single-application chart like this one there is no benefit — every chart change
ships with an app release anyway.

### One-time repository setup

- **GitHub Pages**: in *Settings → Pages*, set *Build and deployment* to
  *Deploy from a branch*, then select the `gh-pages` branch with the `/ (root)`
  folder. The chart repository is then served at
  `https://dotinfra.github.io/cert-manager-webhook-nameshield/index.yaml`
  (the `gh-pages` branch is created automatically by the first chart release).
- **GHCR package**: the first image push creates the
  `cert-manager-webhook-nameshield` package under the repository owner. Link
  it to this repository (*Packages → package settings → Connect repository*)
  so that it inherits the repository visibility.

## Test

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

## Project layout

```
.github/workflows/           CI (tests, lint, image build) and release (GHCR image, Helm chart)
main.go                      Entrypoint, registers the solver
nameshield/solver.go         webhook.Solver: Present / CleanUp / Initialize
nameshield/client.go         Nameshield DNS API v4 client (records, validation)
nameshield/config.go         Solver configuration schema
nameshield/solver_test.go    Unit tests
main_test.go                 cert-manager DNS01 conformance suite
testdata/nameshield/         Conformance suite solver configuration
deploy/...                   Helm chart
Dockerfile                   Multi-stage build
```

## Limitations

- A single API token is used for all zones managed by the webhook.
- Zone discovery probes successive parent names; with many unrelated zones per
  token, set `zoneName` explicitly in each solver config.
- The `ns` records at the zone apex are never touched by this webhook; all
  changes concern `_acme-challenge` TXT records and the zone validation endpoint.
