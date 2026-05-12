# cert-manager-webhook-vegasystems

A [cert-manager](https://cert-manager.io) ACME `DNS01` webhook solver for the
custom DNS REST API exposed by German hosting provider
[VegaSystems](https://kunden.vegasystems.de).

It lets cert-manager solve `DNS01` challenges for domains hosted at VegaSystems
and therefore issue Let's Encrypt (or any other ACME) certificates — including
wildcards.

## How it works

cert-manager calls the webhook for each ACME challenge:

| Step    | Webhook action                                                                |
|---------|-------------------------------------------------------------------------------|
| Present | Look up the domain ID, then `POST` (well, `GET mode=create`) a `TXT` record  |
| CleanUp | List records, delete the `TXT` whose value matches `ch.Key`                   |

## Installation

### Prerequisites

- Kubernetes ≥ 1.20
- [cert-manager](https://cert-manager.io/docs/installation/) installed in the
  cluster (the chart assumes the default `cert-manager` namespace)
- A VegaSystems API user, key and customer ID

### Install via Helm

```sh
helm install cert-manager-webhook-vegasystems \
  oci://ghcr.io/rescaled/charts/cert-manager-webhook-vegasystems \
  --version 0.1.0-beta \
  -n cert-manager
```

Or from a local checkout:

```sh
helm install cert-manager-webhook-vegasystems \
  ./deploy/cert-manager-webhook-vegasystems \
  -n cert-manager
```

### Configure an Issuer

1. Create the credentials Secret in the same namespace as your Issuer (or in
   `cert-manager` for a `ClusterIssuer`):

   ```sh
   kubectl apply -f example/secret.yaml
   ```

2. Create the `ClusterIssuer`:

   ```sh
   kubectl apply -f example/issuer.yaml
   ```

3. Request a certificate:

   ```sh
   kubectl apply -f example/certificate.yaml
   ```

The `webhook.config` block accepts:

| Field              | Type   | Required | Default | Notes                                        |
|--------------------|--------|----------|---------|----------------------------------------------|
| `customerId`       | int    | yes      | —       | VegaSystems customer ID                      |
| `apiUserSecretRef` | object | yes      | —       | `{name, key}` pointing at the API user value |
| `apiKeySecretRef`  | object | yes      | —       | `{name, key}` pointing at the API key value  |
| `ttl`              | int    | no       | `300`   | TTL of the TXT record in seconds (API minimum is 300) |
| `baseURL`          | string | no       | upstream API | Override for testing                    |

## Development

```sh
make vet          # static analysis
make test         # unit tests (REST client)
make build        # local binary
make image        # docker build
make helm-lint    # lint the Helm chart
```

Run the full cert-manager DNS01 conformance suite against a real zone:

```sh
cp testdata/vegasystems/secret.yaml.example testdata/vegasystems/secret.yaml
$EDITOR testdata/vegasystems/secret.yaml   # fill in real creds
$EDITOR testdata/vegasystems/config.json   # set your customerId
TEST_ZONE_NAME=yourzone.example. make conformance
```

The conformance suite is gated behind the `conformance` build tag so the
default `go test ./...` does not pull in the heavy `envtest` machinery.

## Releasing

Push a `vX.Y.Z` tag. The `release` workflow:

1. Builds and pushes a multi-arch image to
   `ghcr.io/<owner>/cert-manager-webhook-vegasystems:<tag>`.
2. Packages the Helm chart and attaches the `.tgz` to the GitHub Release.

## License

MIT — see [LICENSE](LICENSE).
