{{/*
Shared helpers used across multiple templates.
*/}}

{{/*
dcAccount: Returns "CSC" or "CPC" based on cluster type.
Used for account references in permissions and environment config.
*/}}
{{- define "nats-event-bus.dcAccount" -}}
{{- if eq .Values.eventBus.clusterType "csc" -}}
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
validateSecretKeyRef: Fails rendering unless a value is a required
valueFrom.secretKeyRef pointing at the expected secret key.
*/}}
{{- define "nats-event-bus.validateSecretKeyRef" -}}
{{- $value := .value -}}
{{- $path := .path -}}
{{- $secretName := .secretName -}}
{{- $key := .key -}}
{{- if not (kindIs "map" $value) -}}
{{- fail (printf "%s must be a valueFrom.secretKeyRef for secret %s key %s." $path $secretName $key) -}}
{{- end -}}
{{- $valueFrom := get $value "valueFrom" | default dict -}}
{{- if not (kindIs "map" $valueFrom) -}}
{{- fail (printf "%s.valueFrom must be set to secretKeyRef for secret %s key %s." $path $secretName $key) -}}
{{- end -}}
{{- $secretKeyRef := get $valueFrom "secretKeyRef" | default dict -}}
{{- if not (kindIs "map" $secretKeyRef) -}}
{{- fail (printf "%s.valueFrom.secretKeyRef must reference secret %s key %s." $path $secretName $key) -}}
{{- end -}}
{{- if ne (get $secretKeyRef "name") $secretName -}}
{{- fail (printf "%s.valueFrom.secretKeyRef.name must be %s." $path $secretName) -}}
{{- end -}}
{{- if ne (get $secretKeyRef "key") $key -}}
{{- fail (printf "%s.valueFrom.secretKeyRef.key must be %s." $path $key) -}}
{{- end -}}
{{- if eq (toString (get $secretKeyRef "optional")) "true" -}}
{{- fail (printf "%s.valueFrom.secretKeyRef.optional must not be true; missing leaf secrets must fail fast." $path) -}}
{{- end -}}
{{- end -}}

{{/*
natsConfFields: Renders a NATS configuration block body from a flat map.
Strings are quoted; booleans and numbers are unquoted.
*/}}
{{- define "nats-event-bus.natsConfFields" }}
{{- range $key, $value := . }}
{{- if kindIs "map" $value }}
{{ $key }}: {
{{ include "nats-event-bus.natsConfFields" $value | indent 2 }}
}
{{- else if or (kindIs "bool" $value) (kindIs "int" $value) (kindIs "float64" $value) }}
{{ $key }}: {{ $value }}
{{- else }}
{{ $key }}: {{ $value | quote }}
{{- end }}
{{- end }}
{{- end }}

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
