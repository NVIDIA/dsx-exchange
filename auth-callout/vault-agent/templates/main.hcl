{{- with secret "" }}
SSA_CLIENT_ID={{ .Data.data.id }}
SSA_CLIENT_SECRET={{ .Data.data.secret }}
{{- end }}

export SSA_CLIENT_ID
export SSA_CLIENT_SECRET
