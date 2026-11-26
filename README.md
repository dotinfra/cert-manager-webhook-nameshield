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
  `<major>.<minor>`, `<major>`, `latest` and `sha-<short-sha>`);
- Helm chart repository on GitHub Pages:
  `https://dotinfra.github.io/cert-manager-webhook-nameshield`.

### 1. Install the Helm chart

```sh
helm repo add cert-manager-webhook-nameshield https://dotinfra.github.io/cert-manager-webhook-nameshield
helm repo update

helm install cert-manager-webhook-nameshield cert-manager-webhook-nameshield/cert-manager-webhook-nameshield \
  --namespace cert-manager \
  --create-namespace \
  --set groupName=cert-manager-webhook-nameshield \
  --set image.repository=ghcr.io/dotinfra/cert-manager-webhook-nameshield \
  --set image.tag=<version>
```

`image.tag` is optional (the chart defaults to `latest`); pin it to a release
version, e.g. `0.1.0` without the leading `v`. To install from a local
checkout instead:

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
make build-image IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=v0.1.0
make push-image  IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=v0.1.0
```

then install the chart from the local checkout as shown above.

## CI and releases

Two GitHub Actions workflows are defined in `.github/workflows/`:

- **`ci.yaml`** — runs on every push and pull request on `main`:
  - `gofmt` check, `go vet ./...` and unit tests (`go test ./...`; the
    cert-manager DNS01 conformance suite is skipped unless `NAMESHIELD_ZONE`
    and `NAMESHIELD_TOKEN` are set);
  - `helm lint` and `helm template` on `deploy/cert-manager-webhook-nameshield`;
  - a Docker image build (load only, no push) to validate the `Dockerfile`.
- **`release.yaml`**:
  - on push of a `v*` tag, builds the image for `linux/amd64` and
    `linux/arm64` and pushes it to GHCR as
    `ghcr.io/dotinfra/cert-manager-webhook-nameshield` with the tags
    `<version>`, `<major>.<minor>`, `<major>` (semver derived from the tag),
    `latest` and `sha-<short-sha>`;
  - on push to `main` and on `v*` tags, packages the Helm chart with
    [helm/chart-releaser-action](https://github.com/helm/chart-releaser-action)
    and publishes it to the `gh-pages` branch (served by GitHub Pages): one
    GitHub release per chart version, and `index.yaml` regenerated on every
    chart change.

Both workflows rely on the default `GITHUB_TOKEN` only — no extra secrets are
required (`packages: write` for GHCR, `contents: write` for the chart
releases and the `gh-pages` branch).

### Cutting a release

1. Bump `version` and `appVersion` in
   `deploy/cert-manager-webhook-nameshield/Chart.yaml` to the new version
   `X.Y.Z` and merge on `main`.
2. Tag and push the release:

   ```sh
   git tag vX.Y.Z
   git push origin main vX.Y.Z
   ```

3. The `release.yaml` workflow builds and pushes the Docker image and
   publishes the Helm chart.

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
