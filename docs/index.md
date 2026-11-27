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

## Nameshield API specifics handled by the webhook

- Authentication: `Authorization: Bearer <token>`.
- TXT record data must be wrapped in double quotes.
- Changes are only applied after `POST /zones/{zone}/validate`.
- Rate limit of 600 requests per 5 minutes per token, with `Retry-After` handling and retry on 429/5xx.
- Record deletion requires the record ID, obtained by listing records.

## Published artifacts

- Container image on GitHub Container Registry: `ghcr.io/dotinfra/cert-manager-webhook-nameshield`.
- Helm chart repository on GitHub Pages, served from the root of this site:
  `https://dotinfra.github.io/cert-manager-webhook-nameshield`.

## Next steps

- [Installation](installation.md)
- [Configuration](configuration.md)
- [Development](development.md)
