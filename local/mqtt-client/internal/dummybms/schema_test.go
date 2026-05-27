// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dummybms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMessageMatchesPayloadFieldsToChannelParameters(t *testing.T) {
	schema := loadTestSchema(t, `
channels:
  telemetry:
    address: 'telemetry/{device}/{tagPath}'
    parameters:
      device:
        enum: [rack-1]
      tagPath:
        description: Vendor-defined path.
    messages:
      update:
        $ref: '#/components/messages/TelemetryMessage'
components:
  messages:
    TelemetryMessage:
      payload:
        type: object
        required: [device, value]
        properties:
          device: {}
          tagPath:
            type: string
          value:
            type: integer
        additionalProperties: false
`)

	tests := []struct {
		name        string
		payload     string
		wantErrText string
	}{
		{
			name:    "matching payload parameters",
			payload: `{"device":"rack-1","tagPath":"site/power","value":42}`,
		},
		{
			name:        "mismatched named parameter",
			payload:     `{"device":"rack-2","tagPath":"site/power","value":42}`,
			wantErrText: `payload device "rack-2" does not match topic parameter "rack-1"`,
		},
		{
			name:        "mismatched trailing parameter",
			payload:     `{"device":"rack-1","tagPath":"other","value":42}`,
			wantErrText: `payload tagPath "other" does not match topic parameter "site/power"`,
		},
		{
			name:        "non-string parameter field",
			payload:     `{"device":7,"value":42}`,
			wantErrText: "payload device must be a string to match topic parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.ValidateMessage("telemetry/rack-1/site/power", []byte(tt.payload))
			if tt.wantErrText == "" {
				if err != nil {
					t.Fatalf("ValidateMessage() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateMessage() error = nil, want %q", tt.wantErrText)
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Fatalf("ValidateMessage() error = %v, want text %q", err, tt.wantErrText)
			}
		})
	}
}

func TestValidateMessageRejectsEmptyTopicSegments(t *testing.T) {
	schema := loadTestSchema(t, `
channels:
  telemetry:
    address: 'telemetry/{device}/{tagPath}'
    messages:
      update:
        $ref: '#/components/messages/TelemetryMessage'
components:
  messages:
    TelemetryMessage:
      payload:
        type: object
        required: [device, tagPath, value]
        properties:
          device:
            type: string
          tagPath:
            type: string
          value:
            type: integer
        additionalProperties: false
`)

	err := schema.ValidateMessage(
		"telemetry/rack-1//site/power",
		[]byte(`{"device":"rack-1","tagPath":"/site/power","value":42}`),
	)
	if err == nil {
		t.Fatalf("ValidateMessage() error = nil, want error")
	}
	if !strings.Contains(err.Error(), `topic "telemetry/rack-1//site/power" does not match any BMS schema channel`) {
		t.Fatalf("ValidateMessage() error = %v, want topic mismatch", err)
	}
}

func TestValidateMessageRejectsMQTTWildcardsInPublishTopic(t *testing.T) {
	schema := loadTestSchema(t, `
channels:
  telemetry:
    address: 'telemetry/{device}/{tagPath}'
    messages:
      update:
        $ref: '#/components/messages/TelemetryMessage'
components:
  messages:
    TelemetryMessage:
      payload:
        type: object
        required: [device, tagPath, value]
        properties:
          device:
            type: string
          tagPath:
            type: string
          value:
            type: integer
        additionalProperties: false
`)

	tests := []string{
		"telemetry/rack-1/site/#",
		"telemetry/rack-1/+/power",
	}

	for _, topic := range tests {
		t.Run(topic, func(t *testing.T) {
			err := schema.ValidateMessage(
				topic,
				[]byte(`{"device":"rack-1","tagPath":"site/#","value":42}`),
			)
			if err == nil {
				t.Fatalf("ValidateMessage() error = nil, want error")
			}
			if !strings.Contains(err.Error(), "does not match any BMS schema channel") {
				t.Fatalf("ValidateMessage() error = %v, want topic mismatch", err)
			}
		})
	}
}

func loadTestSchema(t *testing.T, content string) *BMSSchema {
	t.Helper()

	path := filepath.Join(t.TempDir(), "schema.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write schema: %v", err)
	}
	schema, err := LoadBMSSchema(path)
	if err != nil {
		t.Fatalf("LoadBMSSchema() error = %v", err)
	}
	return schema
}
