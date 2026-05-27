// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/internal/dummybms"
	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
)

const progressLogInterval = 5 * time.Second

type mqttPublisher struct {
	client *client.Client
}

func (p mqttPublisher) Publish(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error {
	return p.client.PublishContext(ctx, topic, payload, qos, retain)
}

type progressPublisher struct {
	publisher dummybms.Publisher
	total     int
	pass      int
	count     int
	startedAt time.Time
	lastLog   time.Time
}

func (p *progressPublisher) StartPass(pass int) {
	now := time.Now()
	p.pass = pass
	p.count = 0
	p.startedAt = now
	p.lastLog = now
	log.Printf("starting dummy BMS pass %d with %d rows", p.pass, p.total)
}

func (p *progressPublisher) Publish(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error {
	if err := p.publisher.Publish(ctx, topic, payload, qos, retain); err != nil {
		return err
	}

	p.count++
	now := time.Now()
	if p.count == p.total || now.Sub(p.lastLog) >= progressLogInterval {
		p.logProgress(now)
	}
	return nil
}

func (p *progressPublisher) FinishPass() {
	log.Printf("completed dummy BMS pass %d: published %d/%d rows in %s",
		p.pass,
		p.count,
		p.total,
		time.Since(p.startedAt).Round(time.Second),
	)
}

func (p *progressPublisher) logProgress(now time.Time) {
	percent := 100 * float64(p.count) / float64(p.total)
	log.Printf("dummy BMS pass %d progress: published %d/%d rows (%.1f%%), elapsed %s",
		p.pass,
		p.count,
		p.total,
		percent,
		now.Sub(p.startedAt).Round(time.Second),
	)
	p.lastLog = now
}

func main() {
	defaultClientID := fmt.Sprintf("dummy-bms-%d", os.Getpid())

	csvPath := flag.String("csv", "examples/dsx_exemplar.csv", "CSV scenario path")
	schemaPath := flag.String("schema", dummybms.DefaultBMSSchemaPath, "BMS AsyncAPI schema path")
	broker := flag.String("broker", "tcp://127.0.0.1:1883", "MQTT broker URL")
	clientID := flag.String("client-id", defaultClientID, "MQTT client ID")
	qos := flag.Int("qos", 0, "MQTT QoS for publishes")
	once := flag.Bool("once", false, "publish the CSV scenario once and exit")
	flag.Parse()

	if *qos < 0 || *qos > 2 {
		log.Fatalf("qos must be 0, 1, or 2")
	}

	resolvedSchemaPath, err := filepath.Abs(*schemaPath)
	if err != nil {
		log.Fatalf("failed to resolve schema path %q: %v", *schemaPath, err)
	}
	schema, err := dummybms.LoadBMSSchema(resolvedSchemaPath)
	if err != nil {
		log.Fatalf("failed to load BMS schema: %v", err)
	}

	scenario, err := dummybms.LoadScenario(*csvPath, schema)
	if err != nil {
		log.Fatalf("failed to load scenario: %v", err)
	}
	scenarioDuration := scenario.Entries[len(scenario.Entries)-1].Offset.Round(time.Millisecond)

	mqttClient, err := client.New(client.Config{
		Broker:   *broker,
		ClientID: *clientID,
	})
	if err != nil {
		log.Fatalf("failed to create MQTT client: %v", err)
	}
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("failed to connect to MQTT broker %q: %v", *broker, err)
	}
	defer mqttClient.Disconnect()
	log.Printf("connected to MQTT broker %s as %s", *broker, *clientID)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	publisher := &progressPublisher{
		publisher: mqttPublisher{client: mqttClient},
		total:     len(scenario.Entries),
	}
	options := dummybms.PublishOptions{QoS: byte(*qos)}

	log.Printf("loaded %d dummy BMS rows from %s; scenario duration %s; qos %d", len(scenario.Entries), *csvPath, scenarioDuration, *qos)
	for pass := 1; ; pass++ {
		publisher.StartPass(pass)
		if err := dummybms.PublishOnce(ctx, scenario, publisher, options); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Fatalf("failed to publish scenario: %v", err)
		}
		publisher.FinishPass()
		if *once {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}
