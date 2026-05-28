{{/*
Expand the name of the chart.
*/}}
{{- define "app.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "app.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "app.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "app.labels" -}}
helm.sh/chart: {{ include "app.chart" . }}
{{ include "app.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
kratos_logging: 'true'
kratos_metrics: 'true'
{{- end }}

{{/*
Selector labels
*/}}
{{- define "app.selectorLabels" -}}
app.kubernetes.io/name: {{ include "app.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "app.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "app.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Event-bus mTLS integration defaults.
*/}}
{{- define "auth-callout.eventBusMtlsEnabled" -}}
{{- if dig "eventBus" "mtls" "enabled" false (.Values.global | default dict) -}}true{{- end -}}
{{- end }}

{{- define "auth-callout.mtlsCAEnabled" -}}
{{- $mtlsCA := .Values.mtlsCA | default dict -}}
{{- $enabled := get $mtlsCA "enabled" | default false -}}
{{- $requireEventBusMtls := get $mtlsCA "requireEventBusMtls" | default false -}}
{{- if and $enabled (or (not $requireEventBusMtls) (eq (include "auth-callout.eventBusMtlsEnabled" .) "true")) -}}true{{- end -}}
{{- end }}

{{- define "auth-callout.configuredMTLSCAPath" -}}
{{- $serviceConfig := .Values.serviceConfig | default dict -}}
{{- $mtls := get $serviceConfig "mtls" | default dict -}}
{{- get $mtls "ca-path" | default "" -}}
{{- end }}

{{- define "auth-callout.mtlsCAMountPath" -}}
{{- $mtlsCA := .Values.mtlsCA | default dict -}}
{{- default "/etc/mtls-ca" (get $mtlsCA "mountPath") -}}
{{- end }}

{{- define "auth-callout.mtlsCAFileName" -}}
{{- $mtlsCA := .Values.mtlsCA | default dict -}}
{{- default "ca.crt" (get $mtlsCA "fileName") -}}
{{- end }}

{{- define "auth-callout.mtlsCAPath" -}}
{{- printf "%s/%s" (trimSuffix "/" (include "auth-callout.mtlsCAMountPath" .)) (include "auth-callout.mtlsCAFileName" .) -}}
{{- end }}

{{- define "auth-callout.hasExtraVolume" -}}
{{- $name := .name -}}
{{- $found := false -}}
{{- range $volume := (.root.Values.extraVolumes | default list) -}}
{{- if eq (get $volume "name") $name -}}
{{- $found = true -}}
{{- end -}}
{{- end -}}
{{- if $found -}}true{{- end -}}
{{- end }}

{{- define "auth-callout.hasExtraVolumeMount" -}}
{{- $name := .name -}}
{{- $mountPath := .mountPath -}}
{{- $found := false -}}
{{- range $mount := (.root.Values.extraVolumeMounts | default list) -}}
{{- if or (eq (get $mount "name") $name) (eq (get $mount "mountPath") $mountPath) -}}
{{- $found = true -}}
{{- end -}}
{{- end -}}
{{- if $found -}}true{{- end -}}
{{- end }}

{{- define "auth-callout.shouldMountEventBusMtlsCA" -}}
{{- if and (eq (include "auth-callout.mtlsCAEnabled" .) "true") (eq (include "auth-callout.configuredMTLSCAPath" .) "") -}}
{{- $mountPath := include "auth-callout.mtlsCAMountPath" . -}}
{{- if not (eq (include "auth-callout.hasExtraVolumeMount" (dict "root" . "name" "mtls-ca" "mountPath" $mountPath)) "true") -}}
true
{{- end -}}
{{- end -}}
{{- end }}

{{- define "auth-callout.shouldAddEventBusMtlsCAVolume" -}}
{{- if eq (include "auth-callout.shouldMountEventBusMtlsCA" .) "true" -}}
{{- if not (eq (include "auth-callout.hasExtraVolume" (dict "root" . "name" "mtls-ca")) "true") -}}
true
{{- end -}}
{{- end -}}
{{- end }}

{{- define "auth-callout.serviceConfig" -}}
{{- $config := deepCopy (.Values.serviceConfig | default dict) -}}
{{- if eq (include "auth-callout.mtlsCAEnabled" .) "true" -}}
{{- $mtls := deepCopy (get $config "mtls" | default dict) -}}
{{- if not (get $mtls "ca-path") -}}
{{- $_ := set $mtls "ca-path" (include "auth-callout.mtlsCAPath" .) -}}
{{- $_ := set $config "mtls" $mtls -}}
{{- end -}}
{{- end -}}
{{ $config | toYaml }}
{{- end }}

{{/*
==============================================
Metrics Helper Functions (observability)
==============================================
*/}}

{{/*
Get metrics config object
*/}}
{{- define "metrics.config" -}}
{{- index .Values.serviceConfig "observability" "metrics" }}
{{- end }}

{{/*
Check if metrics are enabled
*/}}
{{- define "metrics.enabled" -}}
{{- index .Values.serviceConfig "observability" "metrics" "enabled" }}
{{- end }}

{{/*
Get metrics provider type
*/}}
{{- define "metrics.provider" -}}
{{- index .Values.serviceConfig "observability" "metrics" "provider" }}
{{- end }}

{{/*
Get Prometheus metrics port
*/}}
{{- define "metrics.prometheusPort" -}}
{{- index .Values.serviceConfig "observability" "metrics" "prometheus" "port" }}
{{- end }}

{{/*
Check if Prometheus metrics are enabled (metrics enabled AND provider is prometheus)
*/}}
{{- define "metrics.prometheusEnabled" -}}
{{- $enabled := index .Values.serviceConfig "observability" "metrics" "enabled" -}}
{{- $provider := index .Values.serviceConfig "observability" "metrics" "provider" -}}
{{- if and $enabled (eq $provider "prometheus") -}}
true
{{- end -}}
{{- end }}

{{/*
Check if OTLP metrics are enabled (metrics enabled AND provider is otlp)
*/}}
{{- define "metrics.otlpEnabled" -}}
{{- $enabled := index .Values.serviceConfig "observability" "metrics" "enabled" -}}
{{- $provider := index .Values.serviceConfig "observability" "metrics" "provider" -}}
{{- if and $enabled (eq $provider "otlp") -}}
true
{{- end -}}
{{- end }}

{{/*
==============================================
Tracing Helper Functions (observability)
==============================================
*/}}

{{/*
Get tracing config object
*/}}
{{- define "tracing.config" -}}
{{- index .Values.serviceConfig "observability" "tracing" }}
{{- end }}

{{/*
==============================================
Telemetry Helper Functions (observability)
==============================================
*/}}

{{/*
Get telemetry config object
*/}}
{{- define "telemetry.config" -}}
{{- index .Values.serviceConfig "observability" "telemetry" }}
{{- end }}

{{/*
Get service name from telemetry config
*/}}
{{- define "telemetry.serviceName" -}}
{{- index .Values.serviceConfig "observability" "telemetry" "service-name" }}
{{- end }}
