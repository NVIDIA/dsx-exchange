# Vault Template Development Guide

## The Challenge: Templates Creating Templates

When using Vault Agent injection with Helm, you face a unique challenge: **you need to write a Helm template that generates a Vault template**. This is "template-ception" - one template language creating content for another template language.

Both Helm and Vault use Go template syntax with `{{ }}` delimiters, which creates ambiguity:
- **Helm** needs to process `{{ .Values.vault.secrets.database }}` at deployment time
- **Vault** needs to process `{{ .Data.data.postgres_password }}` at runtime in the pod

This guide explains how to write these templates correctly and verify them at each rendering stage.

---

## Table of Contents

- [Understanding the Rendering Pipeline](#understanding-the-rendering-pipeline)
- [The Backtick Escape Syntax](#the-backtick-escape-syntax)
- [Complete Example with All Stages](#complete-example-with-all-stages)
- [Step-by-Step: Adding a New Secret](#step-by-step-adding-a-new-secret)
- [Common Pitfalls and Solutions](#common-pitfalls-and-solutions)
- [Testing and Verification](#testing-and-verification)
- [Advanced Patterns](#advanced-patterns)

---

## Understanding the Rendering Pipeline

Your Vault template goes through **three distinct rendering stages**:

```
┌─────────────────────────────────────────────────────────────────────┐
│ Stage 1: values.yaml (what you write)                               │
│ - Contains Helm template syntax                                     │
│ - Contains escaped Vault template syntax (using backticks)          │
└─────────────────────────────────────────────────────────────────────┘
                                    ↓
                    Helm template rendering (helm template / install)
                                    ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Stage 2: Kubernetes annotation (after Helm renders)                 │
│ - Helm placeholders are replaced with actual values                 │
│ - Vault template syntax is now unescaped and ready for Vault        │
└─────────────────────────────────────────────────────────────────────┘
                                    ↓
                    Vault Agent processing (in pod at runtime)
                                    ↓
┌─────────────────────────────────────────────────────────────────────┐
│ Stage 3: Final file in pod (/vault/secrets/config-secrets.yaml)     │
│ - Vault has fetched secrets and populated values                    │
│ - Result is valid YAML with actual secret values                    │
└─────────────────────────────────────────────────────────────────────┘
```

---

## The Backtick Escape Syntax

Helm provides the backtick (`` ` ``) syntax to escape template delimiters. This allows you to create literal `{{ }}` text that Helm won't process.

### Basic Syntax

```yaml
# To create this Vault template in the final output:
{{ .Data.data.client_secret }}

# You write this in values.yaml:
{{`{{ .Data.data.client_secret }}`}}
```

### How It Works

| What You Write | Helm Sees | Helm Outputs |
|----------------|-----------|--------------|
| `` {{`{{ ... }}`}} `` | String literal: `{{ ... }}` | `{{ ... }}` |
| `{{ .Values.foo }}` | Template expression | Value of `.Values.foo` |

### Combining Both

When you need both Helm and Vault templates in the same expression:

```yaml
# To create: {{- with secret "secret/data/myapp/nats" }}
# You write:
{{`{{- with secret "`}}{{ .Values.vault.secrets.nats }}{{`" }}`}}
```

Let's break this down:
1. `` {{`{{- with secret "`}} `` → Creates literal `{{- with secret "`
2. `{{ .Values.vault.secrets.nats }}` → Helm evaluates this to `secret/data/myapp/nats`
3. `` {{`" }}`}} `` → Creates literal `" }}`

**Result after Helm:** `{{- with secret "secret/data/myapp/nats" }}`

---

## Complete Example with All Stages

Let's walk through a complete example showing what the template looks like at each stage. We'll use the typical use case of configuring NATS authentication credentials.

### Stage 1: values.yaml (What You Write)

```yaml
vault:
  enabled: true
  secrets:
    nats: "secret/data/myapp/nats"

  template:
    filename: "config-secrets.yaml"
    content: |
      # NATS Auth Callout secrets
      nats:
        {{`{{- with secret "`}}{{ .Values.vault.secrets.nats }}{{`" }}`}}
        nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"
        issuer-seed: "{{`{{ .Data.data.issuer_seed }}`}}"
        xkey-seed: "{{`{{ .Data.data.xkey_seed }}`}}"
        {{`{{- end }}`}}
```

### Stage 2: Kubernetes Annotation (After Helm Renders)

Run `helm template` to see what gets created:

```bash
helm template myapp ./deploy --set vault.enabled=true \
  --set vault.secrets.nats="secret/data/myapp/nats"
```

The pod annotation will contain:

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    vault.hashicorp.com/agent-inject-template-config-secrets.yaml: |
      # NATS Auth Callout secrets
      nats:
        {{- with secret "secret/data/myapp/nats" }}
        nkey-seed: "{{ .Data.data.nkey_seed }}"
        issuer-seed: "{{ .Data.data.issuer_seed }}"
        xkey-seed: "{{ .Data.data.xkey_seed }}"
        {{- end }}
```

**Notice:**
- Helm has replaced `{{ .Values.vault.secrets.nats }}` with `secret/data/myapp/nats`
- Backticks are gone - Vault template syntax is now literal `{{ }}` text
- This is valid Vault template syntax ready for Vault Agent to process

### Stage 3: Final File in Pod (After Vault Processes)

When Vault Agent runs in the pod, it:
1. Reads the annotation template
2. Fetches secrets from Vault
3. Renders the template with actual secret values
4. Writes to `/vault/secrets/config-secrets.yaml`

Final file content:

```yaml
# NATS Auth Callout secrets
nats:
  nkey-seed: "<NATS_NKEY_SEED>"
  issuer-seed: "<NATS_ISSUER_SEED>"
  xkey-seed: "<NATS_XKEY_SEED>"
```

**Notice:**
- All Vault template syntax is gone
- Only the actual secret values remain
- This is valid YAML your application can read and hot-reload

---

## Step-by-Step: Adding a New Secret

Let's add additional secrets to an existing Vault template.

### Step 1: Define the Secret Path

In `values.yaml`, add to the `secrets` map:

```yaml
vault:
  secrets:
    nats: "secret/data/myapp/nats"  # Already configured
    redis: "secret/data/myapp/redis"  # ← NEW
```

### Step 2: Add to Template Content

In `values.yaml`, update `template.content`:

```yaml
vault:
  template:
    content: |
      # ... existing nats section ...

      redis:  # ← NEW SECTION
        {{`{{- with secret "`}}{{ .Values.vault.secrets.redis }}{{`" }}`}}
        password: "{{`{{ .Data.data.redis_password }}`}}"
        {{`{{- end }}`}}
```

### Step 3: Store Secret in Vault

```bash
# Create the secret in Vault (do this once)
vault kv put secret/myapp/redis \
  redis_password="super-secret-redis-password"
```

### Step 4: Verify Each Stage

**Verify Helm rendering:**
```bash
helm template myapp ./deploy \
  --set vault.enabled=true \
  --set vault.secrets.redis="secret/data/myapp/redis" | \
  grep -A 10 "vault.hashicorp.com/agent-inject-template"
```

Expected output (Stage 2):
```yaml
redis:
  {{- with secret "secret/data/myapp/redis" }}
  password: "{{ .Data.data.redis_password }}"
  {{- end }}
```

**Verify final file in pod:**
```bash
# Deploy and exec into pod
kubectl exec -it myapp-pod -- cat /vault/secrets/config-secrets.yaml
```

Expected output (Stage 3):
```yaml
redis:
  password: "super-secret-redis-password"
```

---

## Common Pitfalls and Solutions

### ❌ Pitfall 1: Forgetting Backticks

**Wrong:**
```yaml
content: |
  {{- with secret "secret/data/myapp/nats" }}
  nkey-seed: "{{ .Data.data.nkey_seed }}"
  {{- end }}
```

**Problem:** Helm tries to evaluate `{{- with secret ...` and fails with "function 'secret' not defined"

**Solution:** Use backticks to escape Vault syntax:
```yaml
content: |
  {{`{{- with secret "secret/data/myapp/nats" }}`}}
  nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"
  {{`{{- end }}`}}
```

---

### ❌ Pitfall 2: Wrong Backtick Placement

**Wrong:**
```yaml
{{`{{- with secret "secret/data/myapp/nats" }}`}}
```

**Problem:** The secret path is hardcoded - can't use Helm values

**Correct:**
```yaml
{{`{{- with secret "`}}{{ .Values.vault.secrets.nats }}{{`" }}`}}
```

This splits the backticks so Helm can inject `.Values.vault.secrets.nats`

---

### ❌ Pitfall 3: Mismatched Secret Paths

**Wrong:**
```yaml
secrets:
  nats: "secret/data/myapp/wrong-path"  # ← Wrong path

template:
  content: |
    {{`{{- with secret "`}}{{ .Values.vault.secrets.nats }}{{`" }}`}}
    nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"  # ← Secret exists at secret/data/myapp/nats
```

**Problem:** The path in your config (`secret/data/myapp/wrong-path`) doesn't match where the secret is actually stored in Vault (`secret/data/myapp/nats`). Vault will report "secret not found."

**Solution:** Ensure paths match where secrets are actually stored:
```bash
# Check what secrets exist
vault kv list secret/myapp/

# Verify the specific secret exists
vault kv get secret/myapp/nats
```

---

### 📘 Understanding Vault KV v2 Path Differences

**Why does the CLI omit `/data/` but templates require it?**

This is a common point of confusion: when you update secrets using the Vault CLI, you don't include `/data/` in the path, but when referencing secrets in templates, you must include it.

**Different paths for different access methods:**

| Access Method | Path Format | Example |
|---------------|-------------|---------|
| **CLI Commands** | `secret/myapp/nats` | `vault kv put secret/myapp/nats nkey_seed=xyz` |
| **CLI Read** | `secret/myapp/nats` | `vault kv get secret/myapp/nats` |
| **API / Templates** | `secret/data/myapp/nats` | `{{- with secret "secret/data/myapp/nats" }}` |
| **Metadata API** | `secret/metadata/myapp/nats` | For versioning and metadata operations |

**Why the difference?**

Vault's KV v2 secrets engine has multiple API endpoints:
- **`secret/data/<path>`** - Read/write secret data
- **`secret/metadata/<path>`** - Manage versions, deletion, and metadata

The `vault kv` CLI commands automatically insert `/data/` or `/metadata/` into the path depending on the operation. This makes the CLI more user-friendly by hiding implementation details.

However, when using the **HTTP API directly** (which is what Vault Agent does in your pods), you must explicitly specify the full endpoint path including `/data/`.

**Key takeaway:** Both `vault kv get secret/myapp/nats` (CLI) and `secret/data/myapp/nats` (API) refer to the **same secret**—the CLI just hides the `/data/` complexity for convenience.

**Example workflow:**
```bash
# 1. Store secret using CLI (no /data/)
vault kv put secret/myapp/nats \
  nkey_seed="<NATS_NKEY_SEED>"

# 2. Reference in template (with /data/)
{{`{{- with secret "secret/data/myapp/nats" }}`}}
nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"
{{`{{- end }}`}}
```

**Official Documentation:**
- [Vault KV v2 API Documentation](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2) - Details the `/data/` and `/metadata/` API endpoints
- [Vault KV CLI Commands](https://developer.hashicorp.com/vault/docs/commands/kv) - Explains how CLI commands abstract the path segments
- [Versioned KV Secrets Engine Tutorial](https://developer.hashicorp.com/vault/tutorials/secrets-management/versioned-kv) - Complete guide to KV v2 usage

---

### ❌ Pitfall 4: Wrong Vault Secret Structure

Vault KV v2 has a specific structure: `secret/data/{path}` for read operations, and data is nested under `.Data.data.{key}`.

**Wrong:**
```yaml
# Trying to access v1 structure
{{`{{- with secret "secret/myapp/nats" }}`}}  # ← Missing /data/
nkey-seed: "{{`{{ .nkey_seed }}`}}"  # ← Wrong path
```

**Correct for KV v2:**
```yaml
{{`{{- with secret "secret/data/myapp/nats" }}`}}  # ← /data/ prefix
nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"  # ← .Data.data. prefix
```

---

### ❌ Pitfall 5: Indentation Issues

**Wrong:**
```yaml
vault:
  template:
    content: |
nats:  # ← No indentation
  nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"
```

**Problem:** YAML indentation is part of the content. The final file will have `nats:` at column 0, which might not match your config structure.

**Correct:**
```yaml
vault:
  template:
    content: |
      nats:  # ← Two spaces (matches your config structure)
        nkey-seed: "{{`{{ .Data.data.nkey_seed }}`}}"
```

---

### ❌ Pitfall 6: Quoting Issues

**Wrong:**
```yaml
clientsecret: {{`{{ .Data.data.client_secret }}`}}  # ← No quotes
```

**Problem:** If the secret contains special YAML characters (`:`, `#`, `{`, etc.), YAML parsing will fail

**Correct:**
```yaml
clientsecret: "{{`{{ .Data.data.client_secret }}`}}"  # ← Quoted
```

Always quote secret values to ensure valid YAML regardless of secret content.

---

## Testing and Verification

### Test Stage 1 → Stage 2 (Helm Rendering)

```bash
# See what Helm will generate
helm template myapp ./deploy \
  --set vault.enabled=true \
  --set vault.role="myapp-role" \
  --set vault.secrets.nats="secret/data/myapp/nats" \
  --debug

# Extract just the Vault template annotation
helm template myapp ./deploy \
  --set vault.enabled=true \
  --set vault.secrets.nats="secret/data/myapp/nats" | \
  grep -A 20 "vault.hashicorp.com/agent-inject-template-config-secrets.yaml"
```

**What to verify:**
- ✅ Helm values are replaced (no `{{ .Values.* }}` remain)
- ✅ Vault template syntax is present (`{{ .Data.data.* }}`)
- ✅ Secret paths are correct
- ✅ YAML structure matches your config

---

### Test Stage 2 → Stage 3 (Vault Rendering)

**Option A: Deploy and inspect**
```bash
# Deploy to Kubernetes
helm install myapp ./deploy \
  --set vault.enabled=true \
  --set vault.role="myapp-role"

# Wait for pod to be ready
kubectl wait --for=condition=ready pod -l app=myapp

# Check the rendered secrets file
kubectl exec -it deployment/myapp -- cat /vault/secrets/config-secrets.yaml
```

**Option B: Manual Vault template test**
```bash
# Extract the template from the annotation
kubectl get pod myapp-xxx -o jsonpath='{.metadata.annotations.vault\.hashicorp\.com/agent-inject-template-config-secrets\.yaml}' > test-template.tmpl

# Test rendering with vault CLI
vault read -format=json secret/data/myapp/nats

# Manually test the template logic
vault agent -config=test-agent-config.hcl  # See Vault Agent docs for config
```

**What to verify:**
- ✅ File exists at `/vault/secrets/config-secrets.yaml`
- ✅ Contains actual secret values (not template syntax)
- ✅ Valid YAML syntax
- ✅ No Vault error messages in pod logs

---

### Debugging Vault Template Issues

**Check Vault Agent logs:**
```bash
# Vault agent runs as init container and sidecar
kubectl logs myapp-pod -c vault-agent-init
kubectl logs myapp-pod -c vault-agent
```

**Common error messages:**

| Error | Cause | Solution |
|-------|-------|----------|
| `secret not found` | Wrong path or secret doesn't exist | Check `vault kv list` and verify path |
| `permission denied` | Vault role lacks read permission | Update Vault policy for the role |
| `missing: secret` | Vault template syntax error | Check annotation for valid template syntax |
| `failed to parse: invalid character` | YAML syntax error in rendered output | Check quoting and escaping |

---

## Advanced Patterns

### Pattern 1: Conditional Secret Injection

Only inject a secret if it exists:

```yaml
{{`{{- with secret "`}}{{ .Values.vault.secrets.optional }}{{`" }}`}}
{{`{{ if .Data.data.api_key }}`}}
api:
  key: "{{`{{ .Data.data.api_key }}`}}"
{{`{{ end }}`}}
{{`{{- end }}`}}
```

### Pattern 2: Multiple Values from One Secret

```yaml
{{`{{- with secret "`}}{{ .Values.vault.secrets.database }}{{`" }}`}}
database:
  postgres:
    host: "{{`{{ .Data.data.host }}`}}"
    port: "{{`{{ .Data.data.port }}`}}"
    username: "{{`{{ .Data.data.username }}`}}"
    password: "{{`{{ .Data.data.password }}`}}"
    database: "{{`{{ .Data.data.dbname }}`}}"
{{`{{- end }}`}}
```

Store in Vault:
```bash
vault kv put secret/myapp/database \
  host="postgres.example.com" \
  port="5432" \
  username="myapp_user" \
  password="secret123" \
  dbname="myapp_db"
```

### Pattern 3: Combining Multiple Secrets

```yaml
vault:
  secrets:
    db_primary: "secret/data/myapp/database-primary"
    db_replica: "secret/data/myapp/database-replica"

  template:
    content: |
      database:
        primary:
          {{`{{- with secret "`}}{{ .Values.vault.secrets.db_primary }}{{`" }}`}}
          host: "{{`{{ .Data.data.host }}`}}"
          password: "{{`{{ .Data.data.password }}`}}"
          {{`{{- end }}`}}
        replica:
          {{`{{- with secret "`}}{{ .Values.vault.secrets.db_replica }}{{`" }}`}}
          host: "{{`{{ .Data.data.host }}`}}"
          password: "{{`{{ .Data.data.password }}`}}"
          {{`{{- end }}`}}
```

### Pattern 4: Default Values for Missing Secrets

```yaml
{{`{{- with secret "`}}{{ .Values.vault.secrets.optional }}{{`" }}`}}
timeout: "{{`{{ .Data.data.timeout | default "30s" }}`}}"
{{`{{- end }}`}}
```

### Pattern 5: Formatting and Transformation

```yaml
# Convert to uppercase
api_key: "{{`{{ .Data.data.api_key | toUpper }}`}}"

# Convert to JSON
config: "{{`{{ .Data.data.json_config | toJSON }}`}}"

# Base64 encode
cert: "{{`{{ .Data.data.certificate | base64Encode }}`}}"
```

See [Vault Agent Template documentation](https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/template) for all available functions.

---

## Quick Reference

### Syntax Cheat Sheet

| Goal | Syntax |
|------|--------|
| Literal Vault template | `` {{`{{ .Data }}`}} `` |
| Helm value injection | `{{ .Values.foo }}` |
| Combined (most common) | `` {{`{{- with secret "`}}{{ .Values.path }}{{`" }}`}} `` |
| Vault conditional | `` {{`{{ if .Data.data.key }}`}}...{{`{{ end }}`}} `` |
| Vault iteration | `` {{`{{ range .Data.data }}`}}...{{`{{ end }}`}} `` |
| Vault functions | `` {{`{{ .Data.data.key \| default "value" }}`}} `` |

### Common Vault Secret Paths

| Secret Type | KV v2 Path | Template Access |
|-------------|------------|-----------------|
| NATS Auth | `secret/data/myapp/nats` | `.Data.data.nkey_seed` |
| API Keys | `secret/data/myapp/api` | `.Data.data.api_key` |
| Certificates | `secret/data/myapp/certs` | `.Data.data.tls_cert` |

### Validation Checklist

Before deploying:
- [ ] Secrets exist in Vault at specified paths
- [ ] Vault role has read permissions for all secret paths
- [ ] `helm template` renders valid Vault template syntax
- [ ] Secret keys match what's in Vault (`.Data.data.{key}`)
- [ ] YAML indentation matches application config structure
- [ ] All secret values are quoted
- [ ] `secrets` map and `template.content` are in sync

---

## Additional Resources

- [Vault Agent Injector Annotations](https://developer.hashicorp.com/vault/docs/platform/k8s/injector/annotations)
- [Vault Agent Template Syntax](https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/template)
- [Helm Template Functions](https://helm.sh/docs/chart_template_guide/functions_and_pipelines/)
- [Go Template Documentation](https://pkg.go.dev/text/template)

---

## Summary

The key to Vault template development is understanding the **three-stage rendering pipeline**:

1. **Stage 1 (values.yaml)**: Mix Helm syntax with escaped Vault syntax using backticks
2. **Stage 2 (Kubernetes annotation)**: Helm has rendered its values, Vault syntax is ready
3. **Stage 3 (Pod file)**: Vault has fetched and injected actual secrets

**Golden Rule**: Use backtick syntax `` {{`<content>`}} `` to create literal `<content>` text that Helm won't process but Vault will.

**Testing Strategy**: Verify each stage independently:
- `helm template` for Stage 1 → Stage 2
- `kubectl exec` for Stage 2 → Stage 3
- Pod logs for Vault Agent errors

With this understanding, you can confidently develop complex Vault templates that properly handle secret injection in your Kubernetes deployments! 🔐
