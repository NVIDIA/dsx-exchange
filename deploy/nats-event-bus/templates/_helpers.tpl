{{/*
Shared helpers used across multiple templates.
*/}}

{{/*
dcAccount: Returns "CSC" or "CPC" based on cluster type.
Used for account references in permissions and environment config.
*/}}
{{- define "nats-event-bus.dcAccount" -}}
{{- if eq .Values.global.eventBus.clusterType "csc" -}}
CSC
{{- else -}}
CPC
{{- end -}}
{{- end -}}

{{/*
extraAccountEnvName: Converts an extra account name into a stable env-var token.
*/}}
{{- define "nats-event-bus.extraAccountEnvName" -}}
{{- regexReplaceAll "[^A-Z0-9]" (upper .) "_" -}}
{{- end -}}

{{/*
extraAccountSecretName: Converts an extra account name into a stable Kubernetes
secret-name token.
*/}}
{{- define "nats-event-bus.extraAccountSecretName" -}}
{{- trimAll "-" (regexReplaceAll "[^a-z0-9-]" (lower .) "-") -}}
{{- end -}}

{{/*
nkeySecretEnvValue: Renders a Kubernetes env valueFrom block for a standard
NKey secret ref. global.eventBus.nkeySecretRefOverrides can replace any
generated ref by default secret name and key.
*/}}
{{- define "nats-event-bus.nkeySecretEnvValue" -}}
{{- $root := .root -}}
{{- $defaultName := required "nkey secretName is required" .secretName -}}
{{- $defaultKey := required "nkey key is required" .key -}}
{{- $eventBus := $root.Values.global.eventBus | default dict -}}
{{- $overrides := get $eventBus "nkeySecretRefOverrides" | default dict -}}
{{- if not (kindIs "map" $overrides) -}}
{{- fail "global.eventBus.nkeySecretRefOverrides must be a map keyed by default secret name and key." -}}
{{- end -}}
{{/* Start with the standard env value tree; replace it on an exact name/key match. */}}
{{- $valueTree := dict "valueFrom" (dict "secretKeyRef" (dict "name" $defaultName "key" $defaultKey)) -}}
{{- if hasKey $overrides $defaultName -}}
{{- $secretOverrides := get $overrides $defaultName -}}
{{- if not (kindIs "map" $secretOverrides) -}}
{{- fail (printf "global.eventBus.nkeySecretRefOverrides.%s must be a map keyed by default secret key." $defaultName) -}}
{{- end -}}
{{- if hasKey $secretOverrides $defaultKey -}}
{{/* Override values are rendered verbatim as the env var value tree. */}}
{{- $valueTree = get $secretOverrides $defaultKey -}}
{{- end -}}
{{- end -}}
{{ $valueTree | toYaml }}
{{- end -}}

{{/*
nkeySecretEnv: Renders a Kubernetes env list entry for an NKey secret ref.
*/}}
{{- define "nats-event-bus.nkeySecretEnv" -}}
- name: {{ .env }}
{{ include "nats-event-bus.nkeySecretEnvValue" . | indent 2 }}
{{- end -}}

{{/*
nkeySecretEnvMap: Renders a NATS subchart env map entry for an NKey secret ref.
*/}}
{{- define "nats-event-bus.nkeySecretEnvMap" -}}
{{ .env }}:
{{ include "nats-event-bus.nkeySecretEnvValue" . | indent 2 }}
{{- end -}}

{{/*
authCalloutEventBusEnv: Renders event-bus-specific env vars consumed by
auth-callout at runtime.
*/}}
{{- define "nats-event-bus.authCalloutEventBusEnv" -}}
{{- $eventBus := .Values.global.eventBus | default dict -}}
{{/* Each item is env var -> default secret/key; the leaf helper applies overrides. */}}
{{- $generatedEnvRefs := list
  (dict "env" "AUTH_CALLOUT_NATS_NKEY_SEED" "secretName" "auth-callout-keys" "key" "nkey-seed")
  (dict "env" "AUTH_CALLOUT_NATS_ISSUER_SEED" "secretName" "auth-callout-keys" "key" "issuer-seed")
  (dict "env" "AUTH_CALLOUT_NATS_XKEY_SEED" "secretName" "auth-callout-keys" "key" "xkey-seed")
  (dict "env" "NKEY_NACK_USER_PUBKEY" "secretName" "nats-nack-user" "key" "pubkey")
  (dict "env" "NKEY_SURVEYOR_PUBKEY" "secretName" "nats-surveyor" "key" "pubkey")
-}}
{{- range $item := $generatedEnvRefs }}
{{ include "nats-event-bus.nkeySecretEnv" (dict "root" $ "env" (get $item "env") "secretName" (get $item "secretName") "key" (get $item "key")) }}
{{- end }}
{{- if (get (get $eventBus "mtls" | default dict) "enabled") }}
{{/* mTLS leaf pubkeys are only needed by auth-callout when mTLS is deployed. */}}
{{- $mtlsEnvRefs := list
  (dict "env" "NKEY_MTLS_LEAF_PUBKEY" "secretName" "nats-mtls-leaf" "key" "pubkey")
  (dict "env" "NKEY_MTLS_AUTHX_LEAF_PUBKEY" "secretName" "nats-mtls-authx-leaf" "key" "pubkey")
  (dict "env" "NKEY_MTLS_SYS_LEAF_PUBKEY" "secretName" "nats-mtls-sys-leaf" "key" "pubkey")
-}}
{{- range $item := $mtlsEnvRefs }}
{{ include "nats-event-bus.nkeySecretEnv" (dict "root" $ "env" (get $item "env") "secretName" (get $item "secretName") "key" (get $item "key")) }}
{{- end }}
{{- end }}
{{- if eq (get $eventBus "clusterType") "csc" }}
{{- range $cpcId := (get $eventBus "cpcIds" | default list) }}
{{/* CSC authorizes each incoming CPC leaf by exposing that CPC's pubkey. */}}
{{ include "nats-event-bus.nkeySecretEnv" (dict "root" $ "env" (printf "NKEY_LEAF_CPC_%s_PUBKEY" $cpcId) "secretName" (printf "nats-leaf-cpc-%s" $cpcId) "key" "pubkey") }}
{{- range $accountName, $config := (get $eventBus "extraAccounts" | default dict) }}
{{- $enabled := true -}}
{{- if hasKey $config "enabled" -}}
{{- $enabled = $config.enabled -}}
{{- end -}}
{{- if $enabled }}
{{- $accountEnvName := include "nats-event-bus.extraAccountEnvName" $accountName }}
{{- $accountSecretName := include "nats-event-bus.extraAccountSecretName" $accountName }}
{{/* Extra-account leaves use the same per-CPC pubkey pattern with account tokens. */}}
{{ include "nats-event-bus.nkeySecretEnv" (dict "root" $ "env" (printf "NKEY_LEAF_%s_CPC_%s_PUBKEY" $accountEnvName $cpcId) "secretName" (printf "nats-leaf-%s-cpc-%s" $accountSecretName $cpcId) "key" "pubkey") }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
natsEventBusEnv: Renders event-bus-specific env vars for NATS subchart
container.env maps.
*/}}
{{- define "nats-event-bus.natsEventBusEnv" -}}
{{- $root := .root -}}
{{- $eventBus := $root.Values.global.eventBus | default dict -}}
{{- $includeMtlsLeafSeeds := get . "includeMtlsLeafSeeds" | default false -}}
{{- $includeCpcLeafSeeds := get . "includeCpcLeafSeeds" | default false -}}
{{/* Base NATS refs are always needed by both main and mTLS NATS pods. */}}
{{- $envRefs := list
  (dict "env" "AUTHX_USER_NKEY" "secretName" "nats-authx-user" "key" "pubkey")
  (dict "env" "AUTH_SIGNING_KEY" "secretName" "nats-auth-signing" "key" "pubkey")
  (dict "env" "XKEY_PUBKEY" "secretName" "nats-xkey" "key" "pubkey")
-}}
{{- if $includeMtlsLeafSeeds -}}
{{/* nats-mtls connects back to main NATS with three account-scoped leaf seeds. */}}
{{- $envRefs = append $envRefs (dict "env" "MTLS_LEAF_USER_SEED" "secretName" "nats-mtls-leaf" "key" "seed") -}}
{{- $envRefs = append $envRefs (dict "env" "AUTHX_LEAF_USER_SEED" "secretName" "nats-mtls-authx-leaf" "key" "seed") -}}
{{- $envRefs = append $envRefs (dict "env" "MTLS_SYS_LEAF_USER_SEED" "secretName" "nats-mtls-sys-leaf" "key" "seed") -}}
{{- end -}}
{{- range $item := $envRefs }}
{{ include "nats-event-bus.nkeySecretEnvMap" (dict "root" $root "env" (get $item "env") "secretName" (get $item "secretName") "key" (get $item "key")) }}
{{- end }}
{{- if and $includeCpcLeafSeeds (eq (get $eventBus "clusterType") "cpc") }}
{{/* CPC NATS pods need outbound leaf seeds for CSC plus each enabled account. */}}
{{ include "nats-event-bus.nkeySecretEnvMap" (dict "root" $root "env" "LEAF_USER_SEED" "secretName" "nats-leaf-csc" "key" "seed") }}
{{- range $name, $config := (get $eventBus "extraAccounts" | default dict) }}
{{- $enabled := true -}}
{{- if hasKey $config "enabled" -}}
{{- $enabled = $config.enabled -}}
{{- end -}}
{{- if $enabled }}
{{- $accountEnvName := include "nats-event-bus.extraAccountEnvName" $name }}
{{- $accountSecretName := include "nats-event-bus.extraAccountSecretName" $name }}
{{ include "nats-event-bus.nkeySecretEnvMap" (dict "root" $root "env" (printf "LEAF_%s_USER_SEED" $accountEnvName) "secretName" (printf "nats-leaf-%s-csc" $accountSecretName) "key" "seed") }}
{{- end }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
natsConfValue: Renders one scalar or list value in NATS config syntax.
Lists use JSON syntax so structured account fields like exports/users stay valid.
*/}}
{{- define "nats-event-bus.natsConfValue" -}}
{{- $value := . -}}
{{- if kindIs "slice" $value -}}
{{- $value | toRawJson -}}
{{- else if or (kindIs "bool" $value) (kindIs "int" $value) (kindIs "float64" $value) -}}
{{- $value -}}
{{- else -}}
{{- $value | quote -}}
{{- end -}}
{{- end -}}

{{/*
natsConfField: Renders one key/value pair in NATS config syntax.
Map values become nested blocks; other values are delegated to natsConfValue.
*/}}
{{- define "nats-event-bus.natsConfField" -}}
{{- $key := .key -}}
{{- $value := .value -}}
{{- if kindIs "map" $value -}}
{{ $key }}: {
{{ include "nats-event-bus.natsConfFields" $value | indent 2 }}
}
{{- else -}}
{{ $key }}: {{ include "nats-event-bus.natsConfValue" $value }}
{{- end -}}
{{- end -}}

{{/*
natsConfFields: Renders a NATS configuration block body from a map.
*/}}
{{- define "nats-event-bus.natsConfFields" -}}
{{- range $key, $value := . }}
{{ include "nats-event-bus.natsConfField" (dict "key" $key "value" $value) }}
{{- end -}}
{{- end -}}

{{/*
natsConfBlock: Renders a named NATS configuration block (e.g. tls: { ... }).
*/}}
{{- define "nats-event-bus.natsConfBlock" -}}
{{- $name := .name -}}
{{- $fields := .fields -}}
{{- if and $name $fields (not (empty $fields)) -}}
{{ $name }}: {
{{ include "nats-event-bus.natsConfFields" $fields | indent 2 }}
}
{{- end -}}
{{- end -}}
