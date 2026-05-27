// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dummybms

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const timestampPlaceholder = "{{timestamp_ms}}"

type Scenario struct {
	Entries []Entry
}

type Entry struct {
	Offset          time.Duration
	Topic           string
	Retain          bool
	payloadTemplate string
	schema          *BMSSchema
}

type Publisher interface {
	Publish(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error
}

type PublishOptions struct {
	QoS       byte
	Now       func() time.Time
	WaitUntil func(context.Context, time.Time) error
}

func LoadScenario(path string, schema *BMSSchema) (*Scenario, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open scenario CSV: %w", err)
	}
	defer file.Close()

	scenario, err := ReadScenario(file, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to read scenario CSV %q: %w", path, err)
	}
	return scenario, nil
}

func ReadScenario(reader io.Reader, schema *BMSSchema) (*Scenario, error) {
	if schema == nil {
		return nil, fmt.Errorf("BMS schema is required")
	}

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1

	header, err := csvReader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("scenario CSV is empty")
		}
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	if err := validateScenarioHeader(header); err != nil {
		return nil, err
	}
	csvReader.FieldsPerRecord = len(header)

	var entries []Entry
	var previousOffset time.Duration
	rowNumber := 1
	for {
		rowNumber++
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("row %d: failed to read record: %w", rowNumber, err)
		}

		entry, err := entryFromRecord(record, schema)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", rowNumber, err)
		}
		if len(entries) > 0 && entry.Offset < previousOffset {
			return nil, fmt.Errorf("row %d: offset %s is before previous offset %s", rowNumber, entry.Offset, previousOffset)
		}
		if _, err := entry.RenderPayload(time.UnixMilli(0)); err != nil {
			return nil, fmt.Errorf("row %d: %w", rowNumber, err)
		}

		entries = append(entries, entry)
		previousOffset = entry.Offset
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("scenario CSV has no data rows")
	}

	return &Scenario{Entries: entries}, nil
}

func (e Entry) RenderPayload(now time.Time) ([]byte, error) {
	payload := strings.ReplaceAll(e.payloadTemplate, timestampPlaceholder, strconv.FormatInt(now.UnixMilli(), 10))
	rendered := []byte(payload)
	if err := e.schema.ValidateMessage(e.Topic, rendered); err != nil {
		return nil, err
	}
	return rendered, nil
}

func PublishOnce(ctx context.Context, scenario *Scenario, publisher Publisher, options PublishOptions) error {
	if scenario == nil {
		return fmt.Errorf("scenario is required")
	}
	if publisher == nil {
		return fmt.Errorf("publisher is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	waitUntil := options.WaitUntil
	if waitUntil == nil {
		waitUntil = waitUntilContext
	}

	startedAt := now()
	for i, entry := range scenario.Entries {
		publishAt := startedAt.Add(entry.Offset)
		if err := waitUntil(ctx, publishAt); err != nil {
			return fmt.Errorf("failed waiting for entry %d: %w", i, err)
		}

		payload, err := entry.RenderPayload(now())
		if err != nil {
			return fmt.Errorf("failed to render entry %d payload: %w", i, err)
		}
		if err := publisher.Publish(ctx, entry.Topic, payload, options.QoS, entry.Retain); err != nil {
			return fmt.Errorf("failed to publish entry %d to %q: %w", i, entry.Topic, err)
		}
	}

	return nil
}

func PublishLoop(ctx context.Context, scenario *Scenario, publisher Publisher, options PublishOptions) error {
	for {
		if err := PublishOnce(ctx, scenario, publisher, options); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func validateScenarioHeader(header []string) error {
	if len(header) != 3 || header[0] != "offset" || header[1] != "topic" || header[2] != "payload" {
		return fmt.Errorf("header must be exactly offset,topic,payload")
	}
	return nil
}

func entryFromRecord(record []string, schema *BMSSchema) (Entry, error) {
	offset, err := parseOffset(record[0])
	if err != nil {
		return Entry{}, err
	}

	topicType, err := parseTopicType(record[1])
	if err != nil {
		return Entry{}, err
	}

	payloadTemplate := record[2]
	if payloadTemplate == "" {
		return Entry{}, fmt.Errorf("payload is required")
	}

	return Entry{
		Offset:          offset,
		Topic:           record[1],
		Retain:          topicType == "Metadata",
		payloadTemplate: payloadTemplate,
		schema:          schema,
	}, nil
}

func parseOffset(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, fmt.Errorf("offset is required")
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid offset %q: use a Go duration like 500ms or 1s", raw)
	}
	if duration < 0 {
		return 0, fmt.Errorf("offset must be non-negative")
	}
	return duration, nil
}

func parseTopicType(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("topic is required")
	}

	segments := strings.Split(raw, "/")
	if len(segments) < 4 {
		return "", fmt.Errorf("topic %q does not match BMS/v1/{publisher}/{Value|Metadata}/...", raw)
	}
	for _, segment := range segments {
		if segment == "" {
			return "", fmt.Errorf("topic %q contains an empty segment", raw)
		}
		if containsMQTTWildcard(segment) {
			return "", fmt.Errorf("topic %q contains MQTT wildcard characters", raw)
		}
	}
	if segments[0] != "BMS" || segments[1] != "v1" {
		return "", fmt.Errorf("topic %q must start with BMS/v1", raw)
	}

	topicType := segments[3]
	if topicType != "Value" && topicType != "Metadata" {
		return "", fmt.Errorf("topic %q must use Value or Metadata topic type", raw)
	}
	if topicType == "Metadata" && segments[2] != "PUB" {
		return "", fmt.Errorf("metadata topic %q must use PUB publisher", raw)
	}
	return topicType, nil
}

func waitUntilContext(ctx context.Context, publishAt time.Time) error {
	duration := time.Until(publishAt)
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
