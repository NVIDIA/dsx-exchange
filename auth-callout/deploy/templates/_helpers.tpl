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
