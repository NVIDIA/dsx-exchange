// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dummybms

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadScenarioParsesScenarioAndRendersTimestamp(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Metadata/Rack/RackPower/site-a/row-1/rack-1/power,"{""objectType"":""Rack"",""pointType"":""RackPower"",""rackLocationName"":""row-1 rack-1"",""rackLocationId"":""rack-1"",""engUnit"":""kW""}"
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":{{timestamp_ms}},""quality"":1}"
`)

	scenario, err := loadScenario(t, path)
	if err != nil {
		t.Fatalf("loadScenario() error = %v", err)
	}

	if len(scenario.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(scenario.Entries))
	}
	if scenario.Entries[0].Offset != 0 {
		t.Fatalf("first offset = %s, want 0s", scenario.Entries[0].Offset)
	}
	if scenario.Entries[1].Offset != 500*time.Millisecond {
		t.Fatalf("second offset = %s, want 500ms", scenario.Entries[1].Offset)
	}

	payload, err := scenario.Entries[1].RenderPayload(time.UnixMilli(1743620423000))
	if err != nil {
		t.Fatalf("RenderPayload() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("rendered payload is not JSON: %v", err)
	}
	if got["timestamp"] != float64(1743620423000) {
		t.Fatalf("timestamp = %v, want 1743620423000", got["timestamp"])
	}
}

func TestLoadScenarioRejectsComments(t *testing.T) {
	path := writeScenario(t, `# comment
offset,topic,payload
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want comment rejection")
	}
}

func TestLoadScenarioRejectsInvalidValuePayload(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want missing quality error")
	}
}

func TestLoadScenarioRejectsMetadataTopicMismatch(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Metadata/Rack/RackPower/site-a/row-1/rack-1/power,"{""objectType"":""Rack"",""pointType"":""RackLiquidFlow"",""rackLocationName"":""row-1 rack-1"",""rackLocationId"":""rack-1"",""engUnit"":""LPM""}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want topic mismatch error")
	}
}

func TestLoadScenarioRejectsMQTTWildcardsInPublishTopic(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/#,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want wildcard topic error")
	}
}

func TestLoadScenarioRejectsMetadataThatViolatesBMSSchema(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Metadata/Rack/RackPower/site-a/row-1/rack-1/power,"{""objectType"":""Rack"",""pointType"":""RackPower"",""rackLocationName"":""row-1 rack-1"",""rackLocationId"":""rack-1"",""engUnit"":""C""}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want BMS schema validation error")
	}
}

func TestLoadScenarioRejectsOutOfOrderOffsets(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
1s,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":43.0,""timestamp"":1743620423500,""quality"":1}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want out-of-order offset error")
	}
}

func TestLoadScenarioRejectsBareNumericOffset(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
500,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want bare numeric offset error")
	}
}

func TestLoadScenarioRequiresCanonicalCSVColumns(t *testing.T) {
	path := writeScenario(t, `time,topic,message
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want canonical column error")
	}
}

func TestLoadScenarioRejectsNonCanonicalCSVColumns(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{
			name:   "uppercase",
			header: "Offset,topic,payload",
		},
		{
			name:   "leading space",
			header: "offset, topic,payload",
		},
		{
			name:   "trailing space",
			header: "offset,topic,payload ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeScenario(t, tt.header+`
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

			if _, err := loadScenario(t, path); err == nil {
				t.Fatal("loadScenario() error = nil, want noncanonical column error")
			}
		})
	}
}

func TestLoadScenarioRejectsDuplicateCSVColumns(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload,payload
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}","{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want duplicate column error")
	}
}

func TestLoadScenarioRejectsUnknownCSVColumns(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload,extra
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}",ignored
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want unknown column error")
	}
}

func TestLoadScenarioRejectsExtraCSVFields(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}",ignored
`)

	if _, err := loadScenario(t, path); err == nil {
		t.Fatal("loadScenario() error = nil, want extra field error")
	}
}

func TestLoadScenarioRejectsNonCanonicalValues(t *testing.T) {
	tests := []struct {
		name string
		row  string
	}{
		{
			name: "offset leading space",
			row:  ` 500ms,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"`,
		},
		{
			name: "topic leading space",
			row:  `500ms, BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeScenario(t, "offset,topic,payload\n"+tt.row+"\n")

			if _, err := loadScenario(t, path); err == nil {
				t.Fatal("loadScenario() error = nil, want noncanonical value error")
			}
		})
	}
}

func TestLoadScenarioAcceptsGeneratedExemplarCSV(t *testing.T) {
	scenario, err := LoadScenario("../../examples/dsx_exemplar.csv", testBMSSchema(t))
	if err != nil {
		t.Fatalf("LoadScenario() error = %v", err)
	}

	if len(scenario.Entries) != 4711 {
		t.Fatalf("len(Entries) = %d, want 4711", len(scenario.Entries))
	}
	placeholderCount := 0
	for _, entry := range scenario.Entries {
		if strings.Contains(entry.payloadTemplate, timestampPlaceholder) {
			placeholderCount++
		}
	}
	if placeholderCount != 4545 {
		t.Fatalf("timestamp placeholder count = %d, want 4545", placeholderCount)
	}
	wantLastOffset := time.Minute + 39*time.Second + 367544100*time.Nanosecond
	if got := scenario.Entries[len(scenario.Entries)-1].Offset; got != wantLastOffset {
		t.Fatalf("last offset = %s, want %s", got, wantLastOffset)
	}
}

func TestPublishOnceWaitsAndPublishesScenarioRows(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":{{timestamp_ms}},""quality"":1}"
750ms,BMS/v1/PUB/Value/Rack/RackLiquidSupplyTemperature/site-a/row-1/rack-1/supply-temp,"{""value"":22.9,""timestamp"":{{timestamp_ms}},""quality"":1}"
`)

	scenario, err := loadScenario(t, path)
	if err != nil {
		t.Fatalf("loadScenario() error = %v", err)
	}

	publisher := &recordingPublisher{}
	startedAt := time.UnixMilli(1743620423000)
	var publishTimes []time.Time
	options := PublishOptions{
		QoS: 1,
		Now: func() time.Time {
			return startedAt
		},
		WaitUntil: func(ctx context.Context, publishAt time.Time) error {
			publishTimes = append(publishTimes, publishAt)
			return nil
		},
	}

	if err := PublishOnce(context.Background(), scenario, publisher, options); err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}

	if len(publishTimes) != 2 {
		t.Fatalf("len(publishTimes) = %d, want 2", len(publishTimes))
	}
	if !publishTimes[0].Equal(startedAt) || !publishTimes[1].Equal(startedAt.Add(750*time.Millisecond)) {
		t.Fatalf("publishTimes = %v, want [%s %s]", publishTimes, startedAt, startedAt.Add(750*time.Millisecond))
	}
	if len(publisher.messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(publisher.messages))
	}
	if publisher.messages[0].qos != 1 || publisher.messages[0].retain {
		t.Fatalf("publish options = qos %d retain %v, want qos 1 retain false", publisher.messages[0].qos, publisher.messages[0].retain)
	}

	var got map[string]any
	if err := json.Unmarshal(publisher.messages[0].payload, &got); err != nil {
		t.Fatalf("published payload is not JSON: %v", err)
	}
	if got["timestamp"] != float64(1743620423000) {
		t.Fatalf("timestamp = %v, want 1743620423000", got["timestamp"])
	}
}

func TestPublishOnceRetainsMetadataOnly(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Metadata/Rack/RackPower/site-a/row-1/rack-1/power,"{""objectType"":""Rack"",""pointType"":""RackPower"",""rackLocationName"":""row-1 rack-1"",""rackLocationId"":""rack-1"",""engUnit"":""kW""}"
1s,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	scenario, err := loadScenario(t, path)
	if err != nil {
		t.Fatalf("loadScenario() error = %v", err)
	}

	publisher := &recordingPublisher{}
	options := PublishOptions{
		WaitUntil: func(ctx context.Context, publishAt time.Time) error {
			return nil
		},
	}

	if err := PublishOnce(context.Background(), scenario, publisher, options); err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}

	if len(publisher.messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(publisher.messages))
	}
	if !publisher.messages[0].retain {
		t.Fatal("metadata retain = false, want true")
	}
	if publisher.messages[1].retain {
		t.Fatal("value retain = true, want false")
	}
}

func TestPublishOncePassesContextToPublisher(t *testing.T) {
	path := writeScenario(t, `offset,topic,payload
0s,BMS/v1/PUB/Value/Rack/RackPower/site-a/row-1/rack-1/power,"{""value"":42.5,""timestamp"":1743620423000,""quality"":1}"
`)

	scenario, err := loadScenario(t, path)
	if err != nil {
		t.Fatalf("loadScenario() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	publisher := contextCheckingPublisher{}
	err = PublishOnce(ctx, scenario, publisher, PublishOptions{
		WaitUntil: func(ctx context.Context, publishAt time.Time) error {
			return nil
		},
	})
	if err == nil {
		t.Fatal("PublishOnce() error = nil, want canceled context error")
	}
}

type recordingPublisher struct {
	messages []recordedMessage
}

type recordedMessage struct {
	topic   string
	payload []byte
	qos     byte
	retain  bool
}

func (p *recordingPublisher) Publish(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error {
	p.messages = append(p.messages, recordedMessage{
		topic:   topic,
		payload: payload,
		qos:     qos,
		retain:  retain,
	})
	return nil
}

type contextCheckingPublisher struct{}

func (p contextCheckingPublisher) Publish(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error {
	return ctx.Err()
}

func loadScenario(t *testing.T, path string) (*Scenario, error) {
	t.Helper()

	return LoadScenario(path, testBMSSchema(t))
}

func testBMSSchema(t *testing.T) *BMSSchema {
	t.Helper()

	schema, err := LoadBMSSchema(testBMSSchemaPath(t))
	if err != nil {
		t.Fatalf("failed to load BMS schema: %v", err)
	}
	return schema
}

func testBMSSchemaPath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("../../../../schema/schema/bms/bms.yaml")
	if err != nil {
		t.Fatalf("failed to resolve BMS schema path: %v", err)
	}
	return path
}

func writeScenario(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "scenario.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write scenario: %v", err)
	}
	return path
}
