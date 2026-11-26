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

## Build and installation

### 1. Build and push the container image

```sh
make build-image IMAGE=registry.example.com/infra/cert-manager-webhook-nameshield TAG=v0.1.0
make push-image  IMAGE=registry.example.com/infra/cert-manager-webhook-nameshield TAG=v0.1.0
```

### 2. Create the API token Secret

The token must have DNS rights on the zones involved. For ClusterIssuers, the
Secret must exist in the cert-manager cluster resource namespace (default
`cert-manager`):

```sh
kubectl -n cert-manager create secret generic nameshield-api-token \
  --from-literal=token=<NAMESHIELD_TOKEN>
```

### 3. Install the Helm chart

```sh
helm install cert-manager-webhook-nameshield deploy/cert-manager-webhook-nameshield \
  --namespace cert-manager \
  --set groupName=cert-manager-webhook-nameshield \
  --set image.repository=registry.example.com/infra/cert-manager-webhook-nameshield \
  --set image.tag=v0.1.0
```

The `groupName` value names the Kubernetes APIService group of the webhook.
Here it is set to `cert-manager-webhook-nameshield`; it must be unique in the
cluster and stable once deployed.

By default the chart installs a Role granting the webhook read access to
Secrets in its own namespace only. If Issuers live in other namespaces and
`apiTokenSecretRef` does not specify a namespace, set
`rbac.allowClusterWideSecretAccess=true`.

### 4. Reference the solver in an Issuer

The webhook is now deployed. The `webhook` stanza of the Issuer must reference
the `groupName` configured above and the solver name `nameshield`. The Issuer
manifest itself is intentionally not provided here for now; it will be
documented when the issuer strategy is defined.

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
