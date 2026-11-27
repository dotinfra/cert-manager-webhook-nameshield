# Configuration

Two levels of configuration exist:

- the **solver configuration**, set per-Issuer in the `webhook.config` stanza;
- the **Helm chart values**, set at install/upgrade time.

## Solver configuration

The webhook solver configuration, set in the Issuer's `webhook.config` stanza:

| Field | Type | Description |
|---|---|---|
| `apiUrl` | string | Nameshield DNS API base URL. Default: `https://api.nameshield.net/dns/v4`. Use `https://ote-api.nameshield.net/dns/v4` for the OTE environment. |
| `apiTokenSecretRef` | object | Required. `name` and `key` of the Secret holding the API token. Optional `namespace`; defaults to the challenge namespace (the cert-manager namespace for ClusterIssuers). |
| `zoneName` | string | Optional. Forces the Nameshield zone to update, bypassing zone resolution and discovery. |
| `ttl` | int | Optional. TTL of created TXT records. Provider default when unset. |
| `includeOtherUsersChanges` | bool | Optional, default false. Set to true to also apply pending zone changes made by other users when validating. |

## Helm chart values

Defaults are in `deploy/cert-manager-webhook-nameshield/values.yaml`:

| Value | Default | Description |
|---|---|---|
| `groupName` | `cert-manager-webhook-nameshield` | Kubernetes APIService group of the webhook. Referenced by the chart's APIService and by each Issuer's `groupName` field. Must be unique in the cluster and stable once deployed. |
| `certManager.namespace` | `cert-manager` | Namespace of the cert-manager controller. |
| `certManager.serviceAccountName` | `cert-manager` | ServiceAccount of the cert-manager controller. |
| `image.repository` | `ghcr.io/dotinfra/cert-manager-webhook-nameshield` | Webhook container image repository. |
| `image.tag` | `""` (falls back to `.Chart.AppVersion`) | Webhook container image tag. A released chart points at the Docker tag of the same release. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `nameOverride` / `fullnameOverride` | `""` | Standard Helm name overrides. |
| `replicaCount` | `1` | Number of webhook replicas. |
| `service.type` / `service.port` | `ClusterIP` / `443` | Webhook Service type and port. |
| `rbac.allowClusterWideSecretAccess` | `false` | Install a ClusterRole granting the webhook read access to Secrets cluster-wide, for Issuers in other namespaces without an explicit `apiTokenSecretRef.namespace`. |
| `resources` | `{}` | Container resource requests/limits. |
| `nodeSelector` / `tolerations` / `affinity` | `{}` | Standard pod scheduling knobs. |

## Limitations

- A single API token is used for all zones managed by the webhook.
- Zone discovery probes successive parent names; with many unrelated zones per
  token, set `zoneName` explicitly in each solver config.
- The `ns` records at the zone apex are never touched by this webhook; all
  changes concern `_acme-challenge` TXT records and the zone validation endpoint.
