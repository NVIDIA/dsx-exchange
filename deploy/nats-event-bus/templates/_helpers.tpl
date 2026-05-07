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
