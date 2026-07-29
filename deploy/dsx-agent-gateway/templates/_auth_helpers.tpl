# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

{{/* Tenant identity must come from verified JWT claims. */}}
{{- define "dsxAgentGateway.auth.validateTenantExtractor" -}}
{{- $name := .name -}}
{{- $expr := .expr -}}
{{- if regexMatch "request\\s*\\.\\s*headers" $expr -}}
{{- fail (printf "%s must use verified jwt.* claims, not request headers" $name) -}}
{{- end -}}
{{- if not (regexMatch "jwt\\." $expr) -}}
{{- fail (printf "%s must reference verified jwt.* claims" $name) -}}
{{- end -}}
{{- end -}}

{{/* Validate generic JWT providers and their tenant extractors. */}}
{{- define "dsxAgentGateway.auth.validateProviders" -}}
{{- $auth := .Values.auth | default (dict) -}}
{{- $providers := $auth.jwt.providers | default (dict) -}}
{{- if not (kindIs "map" $providers) -}}
{{- fail "auth.jwt.providers must be a map" -}}
{{- end -}}
{{- if eq (len $providers) 0 -}}
{{- fail "auth.jwt.providers requires at least one provider" -}}
{{- end -}}
{{- $seenIssuers := dict -}}
{{- range $providerName := keys $providers | sortAlpha -}}
{{- if or (gt (len $providerName) 56) (not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" $providerName)) -}}
{{- fail (printf "auth.jwt.providers.%s name must be a lowercase DNS label of at most 56 characters" $providerName) -}}
{{- end -}}
{{- $provider := index $providers $providerName -}}
{{- if not (kindIs "map" $provider) -}}
{{- fail (printf "auth.jwt.providers.%s must be a map" $providerName) -}}
{{- end -}}
{{- $issuerField := printf "auth.jwt.providers.%s.issuer" $providerName -}}
{{- $issuer := required (printf "%s is required" $issuerField) $provider.issuer -}}
{{- if hasKey $seenIssuers $issuer -}}
{{- fail (printf "%s duplicates auth.jwt.providers.%s.issuer" $issuerField (index $seenIssuers $issuer)) -}}
{{- end -}}
{{- $_ := set $seenIssuers $issuer $providerName -}}
{{- $expressionField := printf "auth.jwt.providers.%s.tenantIdExpression" $providerName -}}
{{- $expression := required (printf "%s is required" $expressionField) $provider.tenantIdExpression -}}
{{- include "dsxAgentGateway.auth.validateTenantExtractor" (dict "name" $expressionField "expr" $expression) -}}
{{- end -}}
{{- end -}}

{{/* Build the tenant ID from the verified JWT issuer and claims. */}}
{{- define "dsxAgentGateway.auth.tenantIdExpression" -}}
{{- $auth := .Values.auth | default (dict) -}}
{{- $providers := $auth.jwt.providers | default (dict) -}}
{{- $expression := "\"\"" -}}
{{- range $providerName := keys $providers | sortAlpha | reverse -}}
{{- $provider := index $providers $providerName -}}
{{- $expression = printf "jwt.iss == %q ? (%s) : (%s)" $provider.issuer $provider.tenantIdExpression $expression -}}
{{- end -}}
{{- $expression -}}
{{- end -}}

{{/* Match the chart-owned /mcp route and require a verified tenant ID. */}}
{{- define "dsxAgentGateway.auth.trafficExpression" -}}
{{- $tenantIdExpr := include "dsxAgentGateway.auth.tenantIdExpression" . -}}
{{- printf "(request.path == \"/mcp\" || request.path.startsWith(\"/mcp/\")) && ((%s) != \"\")" $tenantIdExpr -}}
{{- end -}}

{{/* Quote configured MCP target names for the CEL allowlist. */}}
{{- define "dsxAgentGateway.auth.unprivilegedTargetLiterals" -}}
{{- $unprivilegedMCPs := .Values.auth.cel.unprivilegedTenantMCPs | default (list) -}}
{{- if not (kindIs "slice" $unprivilegedMCPs) -}}
{{- fail "auth.cel.unprivilegedTenantMCPs must be a list of target name strings" -}}
{{- end -}}
{{- $celTargets := list -}}
{{- range $idx, $target := $unprivilegedMCPs -}}
{{- if not (kindIs "string" $target) -}}
{{- fail (printf "auth.cel.unprivilegedTenantMCPs[%d] must be a target name string" $idx) -}}
{{- end -}}
{{- if not $target -}}
{{- fail (printf "auth.cel.unprivilegedTenantMCPs[%d] is required" $idx) -}}
{{- end -}}
{{- $celTargets = append $celTargets (printf "%q" $target) -}}
{{- end -}}
{{- join "," $celTargets -}}
{{- end -}}

{{/* Operators reach all targets. Other tenants reach only configured targets. */}}
{{- define "dsxAgentGateway.auth.mcpAuthorizationExpression" -}}
{{- $cel := .Values.auth.cel | default (dict) -}}
{{- $operatorTenantId := required "auth.cel.operatorTenantId is required" $cel.operatorTenantId -}}
{{- $tenantIdExpr := include "dsxAgentGateway.auth.tenantIdExpression" . -}}
{{- $operatorExpr := printf "(%s) == %q" $tenantIdExpr $operatorTenantId -}}
{{- $targets := include "dsxAgentGateway.auth.unprivilegedTargetLiterals" . -}}
{{- if not $targets -}}
{{- $operatorExpr -}}
{{- else -}}
{{- $targetChecks := list -}}
{{- range $category := list "tool" "prompt" "resource" -}}
{{- $targetChecks = append $targetChecks (printf "mcp.%s.target in [%s]" $category $targets) -}}
{{- end -}}
{{- $unprivilegedExpr := printf "(%s)" (join " || " $targetChecks) -}}
{{- printf "(%s) || ((%s) != %q && %s)" $operatorExpr $tenantIdExpr $operatorTenantId $unprivilegedExpr -}}
{{- end -}}
{{- end -}}

{{/* Derive the agentgateway backend coordinates from one complete JWKS URL. */}}
{{- define "dsxAgentGateway.auth.jwksEndpoint" -}}
{{- $root := .root -}}
{{- $providerName := .name -}}
{{- $providers := $root.Values.auth.jwt.providers | default (dict) -}}
{{- $provider := index $providers $providerName | default (dict) -}}
{{- $field := printf "auth.jwt.providers.%s.jwksUrl" $providerName -}}
{{- $url := required (printf "%s is required" $field) $provider.jwksUrl -}}
{{- include "dsxAgentGateway.endpointFromURL" (dict "name" $field "url" $url) -}}
{{- end -}}

{{/* Parse one complete HTTP(S) URL into agentgateway backend coordinates. */}}
{{- define "dsxAgentGateway.endpointFromURL" -}}
{{- $name := .name -}}
{{- $url := .url -}}
{{- $parsed := urlParse $url -}}
{{- $scheme := $parsed.scheme | lower -}}
{{- $path := regexReplaceAll "(?i)^https?://[^/]+" $url "" -}}
{{- if not (has $scheme (list "http" "https")) -}}
{{- fail (printf "%s must use http or https" $name) -}}
{{- end -}}
{{- if or (not $parsed.hostname) (not $path) -}}
{{- fail (printf "%s must include a host and path" $name) -}}
{{- end -}}
{{- if $parsed.userinfo -}}
{{- fail (printf "%s must not include credentials" $name) -}}
{{- end -}}
{{- if $parsed.fragment -}}
{{- fail (printf "%s must not include a fragment" $name) -}}
{{- end -}}
{{- if $parsed.query -}}
{{- fail (printf "%s must not include a query string" $name) -}}
{{- end -}}
{{- $port := 80 -}}
{{- if eq $scheme "https" -}}
{{- $port = 443 -}}
{{- end -}}
{{- $portSuffix := regexFind ":[0-9]+$" $parsed.host -}}
{{- if $portSuffix -}}
{{- $port = atoi (trimPrefix ":" $portSuffix) -}}
{{- end -}}
{{- $host := regexReplaceAll ":[0-9]+$" $parsed.host "" -}}
{{- mustToJson (dict "host" $host "path" $path "port" $port "scheme" $scheme) -}}
{{- end -}}

{{- define "dsxAgentGateway.auth.jwksBackendName" -}}
{{- include "dsxAgentGateway.releaseResourceName" (dict "root" .root "suffix" (printf "%s-jwks" .name)) -}}
{{- end -}}

{{/* Render one fixed JWT provider from its chart-owned JWKS backend. */}}
{{- define "dsxAgentGateway.auth.jwtProvider" -}}
{{- $root := .root -}}
{{- $providerName := .name -}}
{{- $providers := $root.Values.auth.jwt.providers | default (dict) -}}
{{- $provider := index $providers $providerName | default (dict) -}}
{{- $audiences := $provider.audiences | default (list) -}}
{{- $endpoint := include "dsxAgentGateway.auth.jwksEndpoint" . | fromJson -}}
{{- if eq (len $audiences) 0 -}}
{{- fail (printf "auth.jwt.providers.%s.audiences is required" $providerName) -}}
{{- end -}}
- issuer: {{ required (printf "auth.jwt.providers.%s.issuer is required" $providerName) $provider.issuer | quote }}
  audiences:
    {{- range $audiences }}
    - {{ . | quote }}
    {{- end }}
  jwks:
    remote:
      backendRef:
        group: agentgateway.dev
        kind: AgentgatewayBackend
        name: {{ include "dsxAgentGateway.auth.jwksBackendName" . }}
      jwksPath: {{ $endpoint.path | quote }}
{{- end -}}

{{- define "dsxAgentGateway.auth.jwtProviders" -}}
{{- $providerBlocks := list -}}
{{- $providers := .Values.auth.jwt.providers | default (dict) -}}
{{- range $providerName := keys $providers | sortAlpha -}}
{{- $providerBlocks = append $providerBlocks (include "dsxAgentGateway.auth.jwtProvider" (dict "root" $ "name" $providerName) | trim) -}}
{{- end -}}
{{- join "\n" $providerBlocks -}}
{{- end -}}
