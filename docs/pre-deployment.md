# Pre-Deployment

Everything that must be in place before deploying the DSX Event Bus. This covers infrastructure prerequisites, secrets provisioning, NKey generation, certificate management, and Gateway setup.

**Estimated time**: The production path (secrets pipeline + certificates) takes 4–6 hours for a first-time deployment across 1 CSC + 2 CPCs. The evaluation path (`local/` Makefile) takes ~10 minutes. See [Deployment — Evaluation Install](getting-started.md) for the quick-start option.

## Host Prerequisites

A multi-cluster deployment (CSC + CPCs) creates enough kubelets, containerd shims, gateway controllers, and fsnotify watchers to exhaust default Linux inotify limits. Symptoms include `too many open files` from `kubectl logs -f`, silent fsnotify watcher failures, and sporadic `kubectl exec` errors.

Verify these sysctl parameters on each node before creating clusters:

```bash
sudo sysctl -w fs.inotify.max_user_instances=8192
sudo sysctl -w fs.inotify.max_user_watches=524288
```

To persist across reboots, add to `/etc/sysctl.d/` or equivalent for your OS. For Kind-based local evaluation, see `local/README.md` for additional macOS-specific setup (MetalLB networking).

## Infrastructure Prerequisites

The following must be installed in each Kubernetes cluster before deploying the event bus. Components are version-pinned where there is a known API or compatibility break; unpinned components work with any recent release.

| Component | Version | Purpose |
|-----------|---------|---------|
| Kubernetes | 1.27+ | Gateway API CRDs require this minimum |
| Helm | 3.x or 4.x | Helm 4 required only for `local/` evaluation (uses `--force`); Helm 3 works for production |
| Gateway API controller | Envoy Gateway 1.5+ | TCPRoute/TLSRoute (`v1alpha2` APIs); older versions lack these CRDs |
| MetalLB or cloud LB | MetalLB 0.13+ | CRD-based config (`IPAddressPool`, `L2Advertisement`); older versions use incompatible ConfigMap API |
| cert-manager | — | TLS certificate lifecycle (server certs, mTLS certs) |
| Prometheus Operator | — | ServiceMonitor CRD (required by Surveyor) |
| Secrets pipeline | — | Must materialize Kubernetes Secrets (e.g., OpenBao/Vault + VSO, sealed-secrets, external-secrets) |
| [`nsc`](https://github.com/nats-io/nsc/releases) | — | NATS NKey generation (required by `generate-nkeys.sh`) |
| `nk` | — | NATS NKey inspection (required by `generate-nkeys.sh`) |

Keycloak or another OIDC provider is required if using OAuth2 authentication.

## Auth-Callout Container Image

The auth-callout container image is not published to a public registry. Operators must build the image from source and push it to their own container registry before deploying the Helm chart.

```bash
make -C auth-callout docker-build
# Produces auth-callout:latest

# Tag and push to your registry
docker tag auth-callout:latest registry.example.com/dsx-exchange/auth-callout:latest
docker push registry.example.com/dsx-exchange/auth-callout:latest
```

Then set the image in your Helm values:

```yaml
auth-callout:
  image:
    repository: registry.example.com/dsx-exchange/auth-callout
    tag: latest
```

## Required Secrets

All secrets must be provisioned before `helm install`. Secret names and keys are overridable in Helm values; these are the defaults.

### NATS Server Auth

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-auth-signing` | pubkey | AUTH account signing key |
| `nats-xkey` | pubkey | Encryption XKey |

### Auth-Callout Service

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-authx-user` | pubkey | Auth-callout NATS connection user |
| `auth-callout-keys` | nkey-seed, issuer-seed, xkey-seed | Auth-callout signing and encryption keys (seeds only — public keys are derived at runtime) |

### NACK Controller

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-nack-user` | nack-user.nk | NACK NKey file |
|                  | pubkey | NACK user pubkey (for auth-callout permissions) |

### Surveyor

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-surveyor` | seed, pubkey | Surveyor NKey for SYS account access |

### Cross-Cluster Leaf Connections

Each CPC gets a `nats-leaf-csc` secret. The CSC gets the pubkey for each CPC.

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-leaf-csc` | seed | CPC-to-CSC leaf connection (CPC only) |
| `nats-leaf-cpc-{id}` | pubkey | CPC leaf users (CSC only, via generated auth-callout env) |

### mTLS Secrets

The generation script always produces mTLS keys (there is no flag to skip them). These secrets are only consumed when `global.eventBus.mtls.enabled: true`; they can be ignored for non-mTLS deployments.

**Server:**

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-mtls-server-tls` | ca.crt, tls.crt, tls.key | mTLS server certificates |

**Leaf connections:**

| Secret | Keys | Purpose |
|--------|------|---------|
| `nats-mtls-leaf` | seed, pubkey | DC account leaf connection |
| `nats-mtls-authx-leaf` | seed, pubkey | AUTHX account leaf connection |
| `nats-mtls-sys-leaf` | seed, pubkey | SYS account leaf connection (monitoring) |

When `global.eventBus.mtls.enabled: false`, none of the mTLS secrets are required and the mTLS NATS cluster is not deployed.

## NKey Generation

NKeys are Ed25519 public-key pairs used for NATS authentication. The generation script requires [`nsc`](https://github.com/nats-io/nsc/releases) and `nk` on `PATH`.

```bash
nsc generate nkey --user     # user nkey (SU seed, U pubkey)
nsc generate nkey --account  # account nkey (SA seed, A pubkey)
nsc generate nkey --curve    # xkey (SX seed, X pubkey)
```

### Required Keys

| Key | Type | Required |
|-----|------|----------|
| auth-signing | account nkey | Always |
| authx-user | user nkey | Always |
| nack-user | user nkey | Always |
| surveyor | user nkey | Always |
| xkey | xkey | Always |
| authx-leaf-user | user nkey | Always generated, used when mTLS enabled |
| mtls-leaf-user | user nkey | Always generated, used when mTLS enabled |
| mtls-sys-leaf-user | user nkey | Always generated, used when mTLS enabled |

### Generation Script

A script generates all required secrets for a cluster. Without CPC IDs, only the CSC output is generated or left unchanged. With CPC IDs, CSC and the requested CPC outputs are generated or left unchanged. For example:

```bash
./deploy/scripts/generate-nkeys.sh [OPTIONS] [cpc-ids...]

# Options:
#   -o, --output DIR             Output root directory (default: deploy/secrets)
#       --extra-account NAME     Generate CPC-to-CSC leaf keys for an extra account
#   -h, --help                   Show help message

# Examples:
./deploy/scripts/generate-nkeys.sh                             # CSC only
./deploy/scripts/generate-nkeys.sh 1 2 3                       # CSC + CPC-1, CPC-2, CPC-3
./deploy/scripts/generate-nkeys.sh --extra-account LaunchLayer 1 2
```

Each key is written as a subdirectory containing `seed` and `pubkey` files:

```text
secrets/{cluster}/
  nkeys/
    nats-auth-signing/seed
    nats-auth-signing/pubkey
    nats-xkey/seed
    nats-xkey/pubkey
    nats-authx-user/seed
    nats-authx-user/pubkey
    nats-nack-user/seed
    nats-nack-user/pubkey
    nats-nack-user/nack-user.nk
    nats-mtls-leaf/seed
    nats-mtls-leaf/pubkey
    nats-mtls-authx-leaf/seed
    nats-mtls-authx-leaf/pubkey
    nats-mtls-sys-leaf/seed
    nats-mtls-sys-leaf/pubkey
    nats-surveyor/seed
    nats-surveyor/pubkey
    auth-callout-keys/nkey-seed
    auth-callout-keys/issuer-seed
    auth-callout-keys/xkey-seed
    nats-leaf-cpc-{id}/pubkey    # CSC only, one per CPC
    xkey.nk
```

## Secrets Pipeline

The event bus consumes Kubernetes Secrets — it does not interact with any secrets backend directly. Any pipeline that materializes the secrets listed in [Required Secrets](#required-secrets) into the target namespace before `helm install` will work. The Helm chart does not assume Vault, sealed-secrets, external-secrets, or any other specific provider.

One common pattern is [OpenBao](https://openbao.org/) or [HashiCorp Vault](https://developer.hashicorp.com/vault) with the Vault Secrets Operator (VSO) to materialize secrets and the Vault Agent Injector for auth-callout seed injection. For installation, K8s auth methods, policies, and PKI setup, see the respective documentation:

- [OpenBao](https://openbao.org/docs/)
- [Vault on Kubernetes](https://developer.hashicorp.com/vault/docs/deploy/kubernetes)
- [Vault Secrets Operator](https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso)
- [KV Secret Engine](https://developer.hashicorp.com/vault/docs/secrets/kv)
- [PKI Secret Engine](https://developer.hashicorp.com/vault/docs/secrets/pki)

## Certificate Management

cert-manager handles TLS certificate lifecycle. The Issuer can be backed by any supported provider (Vault/OpenBao PKI, self-signed, ACME, etc.).

### Server TLS Certificate

All TLS-terminated Gateway listeners reference a cert-manager Certificate:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: event-bus-server-tls-certificate
spec:
  secretName: event-bus-server-tls-certificate
  issuerRef:
    name: event-bus-certificate-issuer
    kind: Issuer
  commonName: "event-bus.example.com"
  dnsNames:
    - "event-bus.example.com"
```

## Gateway Setup

A Gateway API controller must be installed before deploying the event bus. The Gateway resource defines the external listeners that route traffic to the NATS pods.

### Gateway Resource

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: event-bus-gateway
spec:
  gatewayClassName: eg
  listeners:
    - name: mqtt
      protocol: TLS
      port: 1883
      allowedRoutes:
        namespaces:
          from: All
      tls:
        mode: Terminate
        certificateRefs:
          - kind: Secret
            name: event-bus-server-tls-certificate
    - name: nats-client
      protocol: TLS
      port: 4222
      allowedRoutes:
        namespaces:
          from: All
      tls:
        mode: Terminate
        certificateRefs:
          - kind: Secret
            name: event-bus-server-tls-certificate
    - name: nats-leafnode
      protocol: TLS
      port: 7422
      allowedRoutes:
        namespaces:
          from: All
      tls:
        mode: Terminate
        certificateRefs:
          - kind: Secret
            name: event-bus-server-tls-certificate
    - name: mqtt-mtls
      protocol: TLS
      port: 8883
      allowedRoutes:
        namespaces:
          from: All
      tls:
        mode: Passthrough
  infrastructure:
    parametersRef:
      group: gateway.envoyproxy.io
      kind: EnvoyProxy
      name: event-bus-proxy-config
```

The `mqtt-mtls` listener uses Passthrough mode because TLS termination happens at the NATS pod to verify the client certificate.

### Listener-to-Route Mapping

Gateway listener names must match the `sectionName` in the Helm `gateway.routes` values. Route kind depends on the TLS mode:

| Listener | Port | TLS Mode | Route Kind |
|----------|------|----------|------------|
| mqtt | 1883 | Terminate | TCPRoute |
| nats-client | 4222 | Terminate | TCPRoute |
| nats-leafnode | 7422 | Terminate | TCPRoute |
| mqtt-mtls | 8883 | Passthrough | TLSRoute |

## External References

- Vault KV Secret Engine: https://developer.hashicorp.com/vault/docs/secrets/kv
- Vault PKI Secret Engine: https://developer.hashicorp.com/vault/docs/secrets/pki
- Vault Secrets Operator: https://developer.hashicorp.com/vault/docs/deploy/kubernetes/vso
- cert-manager Vault Issuer: https://cert-manager.io/docs/configuration/vault/
- NATS NKey Auth: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_intro/nkey_auth
- NATS Auth Callout: https://docs.nats.io/running-a-nats-service/configuration/securing_nats/auth_callout
