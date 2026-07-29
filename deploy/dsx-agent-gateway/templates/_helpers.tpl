{{/* Reserve suffix space while keeping release-derived names within Kubernetes' 63-character limit. */}}
{{- define "dsxAgentGateway.releaseResourceName" -}}
{{- $suffix := .suffix | default "" -}}
{{- if $suffix -}}
{{- $baseLength := sub 62 (len $suffix) | int -}}
{{- printf "%s-%s" (.root.Release.Name | trunc $baseLength | trimSuffix "-") $suffix -}}
{{- else -}}
{{- .root.Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "dsxAgentGateway.gatewayName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" .) -}}
{{- end -}}

{{- define "dsxAgentGateway.agentgatewayParametersName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "params") -}}
{{- end -}}

{{- define "dsxAgentGateway.agentgatewayBackendName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "mcp") -}}
{{- end -}}

{{- define "dsxAgentGateway.upstreamTimeoutPolicyName" -}}
{{- $suffix := "mcp-timeout" -}}
{{- if .upstreamID -}}
{{- $suffix = printf "%s-%s" $suffix (sha256sum .upstreamID | trunc 8) -}}
{{- end -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" .root "suffix" $suffix) -}}
{{- end -}}

{{- define "dsxAgentGateway.validateRequestTimeout" -}}
{{- $path := .path -}}
{{- $timeout := required (printf "%s is required" $path) .value | toString -}}
{{- if not (regexMatch "^([0-9]{1,5}(h|m|s|ms)){1,4}$" $timeout) -}}
{{- fail (printf "%s=%q is not a valid Gateway API duration" $path $timeout) -}}
{{- end -}}
{{- $timeout -}}
{{- end -}}

{{- define "dsxAgentGateway.httpRouteName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "mcp-route") -}}
{{- end -}}

{{- define "dsxAgentGateway.agentgatewayPolicyName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "authz") -}}
{{- end -}}

{{- define "dsxAgentGateway.bridgeName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "bridge") -}}
{{- end -}}

{{- define "dsxAgentGateway.rateLimitServiceName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "ratelimit") -}}
{{- end -}}

{{- define "dsxAgentGateway.rateLimitConfigMapName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "ratelimit-config") -}}
{{- end -}}

{{- define "dsxAgentGateway.otelBackendName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" . "suffix" "otel") -}}
{{- end -}}

{{/* Add the chart-owned hub Service to MCP discovery. */}}
{{- define "dsxAgentGateway.computedUpstreams" -}}
{{- $upstreams := .Values.upstreams -}}
{{- if not (kindIs "map" $upstreams) -}}
{{- fail "upstreams must be a map" -}}
{{- end -}}
{{- $computedUpstreams := list -}}
{{- range $id, $upstream := $upstreams -}}
{{- $path := printf "upstreams.%s" $id -}}
{{- if not (kindIs "map" $upstream) -}}
{{- fail (printf "%s must be a map" $path) -}}
{{- end -}}
{{- $mode := $upstream.mode | default "selector" -}}
{{- if not (has $mode (list "selector" "static")) -}}
{{- fail (printf "%s.mode=%q is not recognized. Use selector or static" $path $mode) -}}
{{- end -}}
{{- if eq $mode "selector" -}}
{{- $_ := required (printf "%s.namespace is required" $path) $upstream.namespace -}}
{{- $_ := required (printf "%s.serviceLabels is required" $path) $upstream.serviceLabels -}}
{{- else -}}
{{- $address := required (printf "%s.address is required for static upstreams" $path) $upstream.address -}}
{{- $_ := include "dsxAgentGateway.endpointFromURL" (dict "name" (printf "%s.address" $path) "url" $address) -}}
{{- $protocol := $upstream.protocol | default "StreamableHTTP" -}}
{{- if not (has $protocol (list "StreamableHTTP" "SSE")) -}}
{{- fail (printf "%s.protocol=%q is not recognized. Use StreamableHTTP or SSE" $path $protocol) -}}
{{- end -}}
{{- end -}}
{{- $computedUpstreams = append $computedUpstreams (dict "id" $id "upstream" $upstream) -}}
{{- end -}}
{{- $bridge := .Values.bridge | default (dict) -}}
{{- if and ($bridge.enabled | default false) (eq ($bridge.role | default "") "hub") -}}
{{- $bridgeLabels := dict "app.kubernetes.io/instance" .Release.Name "app.kubernetes.io/component" "dsx-agentgateway-bridge" -}}
{{- $bridgeUpstream := dict "namespace" .Release.Namespace "serviceLabels" $bridgeLabels -}}
{{- $computedUpstreams = append $computedUpstreams (dict "id" "bridge" "upstream" $bridgeUpstream) -}}
{{- end -}}
{{- mustToJson $computedUpstreams -}}
{{- end -}}

{{/* Validate a complete in-cluster Service destination. */}}
{{- define "dsxAgentGateway.validateInClusterDestination" -}}
{{- $name := .name -}}
{{- $destination := .destination | default (dict) -}}
{{- if not (kindIs "map" $destination) -}}
{{- fail (printf "%s must be a map" $name) -}}
{{- end -}}
{{- $_ := required (printf "%s.serviceName is required" $name) $destination.serviceName -}}
{{- $_ := required (printf "%s.namespace is required" $name) $destination.namespace -}}
{{- $_ := required (printf "%s.port is required" $name) $destination.port -}}
{{- end -}}

{{/* Validate runtime coordinates, then return the destination kind. */}}
{{- define "dsxAgentGateway.destinationKind" -}}
{{- $destination := . | default (dict) -}}
{{- if not (kindIs "map" $destination) -}}
{{- fail "destination must be a map" -}}
{{- end -}}
{{- $inCluster := or (hasKey $destination "serviceName") (hasKey $destination "namespace") -}}
{{- $offCluster := hasKey $destination "host" -}}
{{- if and $inCluster $offCluster -}}
{{- fail "destination cannot mix in-cluster and off-cluster coordinates" -}}
{{- else if $inCluster -}}
{{- include "dsxAgentGateway.validateInClusterDestination" (dict "name" "destination" "destination" $destination) -}}
in-cluster
{{- else if $offCluster -}}
{{- $_ := required "off-cluster destination requires host" $destination.host -}}
{{- $_ := required "off-cluster destination requires port" $destination.port -}}
off-cluster
{{- else -}}
{{- fail "destination requires in-cluster Service coordinates or off-cluster host and port coordinates" -}}
{{- end -}}
{{- end -}}

{{/* Build the runtime host from a supported destination. */}}
{{- define "dsxAgentGateway.destinationHost" -}}
{{- $destination := . | default (dict) -}}
{{- $kind := include "dsxAgentGateway.destinationKind" $destination | trim -}}
{{- if eq $kind "in-cluster" -}}
{{- printf "%s.%s.svc" $destination.serviceName $destination.namespace -}}
{{- else -}}
{{- $destination.host -}}
{{- end -}}
{{- end -}}

{{/* Mirror pinned Valkey naming so the ingress policy selects subchart Pods. */}}
{{- define "dsxAgentGateway.valkeyName" -}}
{{- default "valkey" .Values.valkey.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "dsxAgentGateway.valkeyFullname" -}}
{{- if .Values.valkey.fullnameOverride -}}
{{- .Values.valkey.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "dsxAgentGateway.valkeyName" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "dsxAgentGateway.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/* Labels shared by every Prometheus Operator monitor. */}}
{{- define "dsxAgentGateway.monitorLabels" -}}
{{- include "dsxAgentGateway.labels" .root }}
app.kubernetes.io/component: {{ .component }}
{{- with .root.Values.observability.metrics.additionalLabels }}
{{- toYaml . | nindent 0 }}
{{- end }}
{{- end -}}

{{/* The DSX metrics contract uses one scrape policy for every component. */}}
{{- define "dsxAgentGateway.metricsEndpoint" -}}
- port: {{ .port }}
  path: /metrics
  interval: {{ required "observability.metrics.interval is required" .root.Values.observability.metrics.interval }}
  scrapeTimeout: {{ required "observability.metrics.scrapeTimeout is required" .root.Values.observability.metrics.scrapeTimeout }}
{{- end -}}

{{/* Validate the DSX OpenTelemetry Operator resources. */}}
{{- define "dsxAgentGateway.validateOpenTelemetry" -}}
{{- $tracing := .Values.observability.tracing | default (dict) -}}
{{- $_ := required "observability.tracing.instrumentationRef is required" $tracing.instrumentationRef -}}
{{- $_ := required "observability.tracing.sidecarRef is required" $tracing.sidecarRef -}}
{{- $exporter := $tracing.exporter | default (dict) -}}
{{- $endpoint := $exporter.endpoint | default "" -}}
{{- $protocol := $exporter.protocol | default "" -}}
{{- if ne (empty $endpoint) (empty $protocol) -}}
{{- fail "observability.tracing.exporter.endpoint and observability.tracing.exporter.protocol must be set together" -}}
{{- end -}}
{{- if not (has $protocol (list "" "grpc" "http/protobuf")) -}}
{{- fail (printf "observability.tracing.exporter.protocol=%q is not recognized. Use grpc or http/protobuf" $protocol) -}}
{{- end -}}
{{- end -}}

{{/* Opt traced application Pods into the platform SDK and sidecar injection. */}}
{{- define "dsxAgentGateway.otelPodAnnotations" -}}
{{- $root := .root -}}
{{- if $root.Values.observability.tracing.enabled -}}
{{- include "dsxAgentGateway.validateOpenTelemetry" $root -}}
{{- $tracing := $root.Values.observability.tracing | default (dict) -}}
instrumentation.opentelemetry.io/inject-sdk: {{ $tracing.instrumentationRef | quote }}
sidecar.opentelemetry.io/inject: {{ $tracing.sidecarRef | quote }}
resource.opentelemetry.io/service.name: {{ .serviceName | quote }}
{{- end -}}
{{- end -}}
