# Installation

Pre-built artifacts are published automatically by the CI (see [Development](development.md)):

- container image on GitHub Container Registry:
  `ghcr.io/dotinfra/cert-manager-webhook-nameshield` (tags `<version>`,
  `<major>.<minor>`, `<major>`, `latest` on stable releases, and
  `sha-<short-sha>`);
- Helm chart repository on GitHub Pages:
  `https://dotinfra.github.io/cert-manager-webhook-nameshield`.

## 1. Install the Helm chart

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
at the Docker tag published for the same release. Override it with
`--set image.tag=<tag>`. To install from a local checkout with a self-built
image instead:

```sh
helm install cert-manager-webhook-nameshield deploy/cert-manager-webhook-nameshield \
  --namespace cert-manager \
  --set groupName=cert-manager-webhook-nameshield \
  --set image.repository=<your-registry>/cert-manager-webhook-nameshield \
  --set image.tag=<tag>
```

## 2. Create the API token Secret

The token must have DNS rights on the zones involved. For ClusterIssuers, the
Secret must exist in the cert-manager cluster resource namespace (default
`cert-manager`):

```sh
kubectl -n cert-manager create secret generic nameshield-api-token \
  --from-literal=token=<NAMESHIELD_TOKEN>
```

The `groupName` value names the Kubernetes APIService group of the webhook.
It must be unique in the cluster and stable once deployed.

By default the chart installs a Role granting the webhook read access to
Secrets in its own namespace only. If Issuers live in other namespaces and
`apiTokenSecretRef` does not specify a namespace, set
`rbac.allowClusterWideSecretAccess=true`.

## 3. Reference the solver in an Issuer

Create a `ClusterIssuer` (or `Issuer`) whose `dns01` stanza uses the `webhook`
solver with the `groupName` configured above and the solver name `nameshield`:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-nameshield
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-account-key
    solvers:
      - dns01:
          webhook:
            groupName: cert-manager-webhook-nameshield
            solverName: nameshield
            config:
              apiTokenSecretRef:
                name: nameshield-api-token
                key: token
              # namespace: cert-manager  # rarely needed, see below
              # zoneName: example.com    # optional: force the Nameshield zone
              # apiUrl: https://ote-api.nameshield.net/dns/v4  # OTE environment
              # ttl: 60                  # optional TXT record TTL
```

The `config` stanza accepts the fields documented in
[Configuration](configuration.md). For a `ClusterIssuer`, cert-manager resolves
`apiTokenSecretRef` in its own cluster resource namespace (`cert-manager` by
default), so `namespace` can usually be omitted and the Secret created in
step 2 is found as-is. Set `namespace` explicitly when it differs, or set
`rbac.allowClusterWideSecretAccess=true` on the chart.

Then request a certificate:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com
  namespace: default
spec:
  secretName: example-com-tls
  dnsNames:
    - example.com
    - www.example.com
  issuerRef:
    name: letsencrypt-nameshield
    kind: ClusterIssuer
```

## Building the image from source

```sh
make build-image IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
make push-image  IMAGE=ghcr.io/dotinfra/cert-manager-webhook-nameshield TAG=0.1.0
```

then install the chart from the local checkout as shown above.
