// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mqttbus

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateTopicFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantErr bool
	}{
		{name: "plain", filter: "BMS/v1/PUB/Metadata/Rack"},
		{name: "single wildcard", filter: "BMS/+/PUB"},
		{name: "multi wildcard final", filter: "BMS/#"},
		{name: "empty", filter: "", wantErr: true},
		{name: "hash not final", filter: "BMS/#/Rack", wantErr: true},
		{name: "hash partial", filter: "BMS/foo#/Rack", wantErr: true},
		{name: "plus partial", filter: "BMS/foo+/Rack", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTopicFilter(tt.filter)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateTopicFilter(%q) succeeded, want error", tt.filter)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateTopicFilter(%q) failed: %v", tt.filter, err)
			}
		})
	}
}

func TestErrorCode(t *testing.T) {
	err := &BusError{Code: CodeMQTTAuthFailed, Message: "denied"}
	if got := ErrorCode(err); got != CodeMQTTAuthFailed {
		t.Fatalf("ErrorCode = %q, want %q", got, CodeMQTTAuthFailed)
	}
	if got := ErrorCode(errors.New("plain")); got != CodeInternalError {
		t.Fatalf("ErrorCode for plain error = %q, want %q", got, CodeInternalError)
	}
}

func TestClassifyConnectError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "auth", err: errors.New("not authorized"), code: CodeMQTTAuthFailed},
		{name: "tls", err: errors.New("x509: certificate signed by unknown authority"), code: CodeTLSHandshakeFailed},
		{name: "unavailable", err: errors.New("connection refused"), code: CodeBusUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyConnectError(tt.err)
			if got := ErrorCode(err); got != tt.code {
				t.Fatalf("code = %q, want %q (err=%v)", got, tt.code, err)
			}
		})
	}
}

func TestClassifySubscribeResult(t *testing.T) {
	tests := []struct {
		name    string
		results map[string]byte
		want    string
	}{
		{name: "granted qos 0", results: map[string]byte{"BMS/#": 0}},
		{name: "denied", results: map[string]byte{"BMS/#": 0x80}, want: CodeTopicACLDenied},
		{name: "missing result", results: map[string]byte{}, want: CodeMQTTSubscribeFailed},
		{name: "invalid code", results: map[string]byte{"BMS/#": 0x81}, want: CodeMQTTSubscribeFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifySubscribeResultCode("BMS/#", tt.results)
			if tt.want == "" && err != nil {
				t.Fatalf("classifySubscribeResultCode returned error: %v", err)
			}
			if tt.want != "" && ErrorCode(err) != tt.want {
				t.Fatalf("code = %q, want %q (err=%v)", ErrorCode(err), tt.want, err)
			}
		})
	}
}

func TestBuildTLSConfigMissingCA(t *testing.T) {
	_, err := buildTLSConfig(TLSConfig{CAFile: "/no/such/ca.crt"})
	if err == nil {
		t.Fatalf("buildTLSConfig succeeded with missing CA file")
	}
	if got := ErrorCode(err); got != CodeTLSConfigError {
		t.Fatalf("code = %q, want %q", got, CodeTLSConfigError)
	}
	if !strings.Contains(err.Error(), "no/such/ca.crt") {
		t.Fatalf("error did not include CA path context: %v", err)
	}
}
