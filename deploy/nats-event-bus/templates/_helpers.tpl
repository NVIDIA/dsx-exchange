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
