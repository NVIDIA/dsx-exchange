// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MessagesPublished tracks total messages published
	MessagesPublished = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mqtt_messages_published_total",
			Help: "Total number of MQTT messages published",
		},
		[]string{"broker", "topic", "qos", "retained", "federation"},
	)

	// MessagesReceived tracks total messages received
	MessagesReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mqtt_messages_received_total",
			Help: "Total number of MQTT messages received",
		},
		[]string{"broker", "topic", "qos", "retained", "federation"},
	)

	// PublishDuration tracks message publish latency
	PublishDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mqtt_publish_duration_seconds",
			Help:    "Time taken to publish messages",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 15), // 0.1ms to ~3s
		},
		[]string{"broker", "topic", "qos", "retained", "federation"},
	)

	// EndToEndLatency tracks end-to-end message latency (publish to receive)
	EndToEndLatency = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "mqtt_e2e_latency_seconds",
			Help:       "End-to-end message latency from publish to receive",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.95: 0.005, 0.99: 0.001},
			MaxAge:     10 * time.Minute,
		},
		[]string{"broker_pub", "broker_sub", "topic", "qos", "retained", "federation"},
	)

	// ConnectionsActive tracks number of active connections
	ConnectionsActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mqtt_connections_active",
			Help: "Number of active MQTT connections",
		},
		[]string{"broker", "role"}, // role: publisher, subscriber
	)

	// Errors tracks total errors by type
	Errors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mqtt_errors_total",
			Help: "Total number of errors encountered",
		},
		[]string{"broker", "error_type"},
	)

	// Throughput tracks messages per second
	Throughput = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "mqtt_throughput_messages_per_second",
			Help: "Current throughput in messages per second",
		},
		[]string{"broker", "direction", "qos", "retained", "federation"}, // direction: publish, receive
	)
)

// Labels holds common metric labels
type Labels struct {
	Broker     string
	Topic      string
	QoS        string
	Retained   string
	Federation string
	Role       string
}

// RecordPublish records a message publication
func RecordPublish(labels Labels, duration float64) {
	MessagesPublished.WithLabelValues(
		labels.Broker,
		labels.Topic,
		labels.QoS,
		labels.Retained,
		labels.Federation,
	).Inc()

	PublishDuration.WithLabelValues(
		labels.Broker,
		labels.Topic,
		labels.QoS,
		labels.Retained,
		labels.Federation,
	).Observe(duration)
}

// RecordReceive records a message reception
func RecordReceive(labels Labels) {
	MessagesReceived.WithLabelValues(
		labels.Broker,
		labels.Topic,
		labels.QoS,
		labels.Retained,
		labels.Federation,
	).Inc()
}

// RecordEndToEndLatency records end-to-end latency
func RecordEndToEndLatency(brokerPub, brokerSub, topic, qos, retained, federation string, latency float64) {
	EndToEndLatency.WithLabelValues(
		brokerPub,
		brokerSub,
		topic,
		qos,
		retained,
		federation,
	).Observe(latency)
}

// SetConnectionsActive sets the number of active connections
func SetConnectionsActive(broker, role string, count int) {
	ConnectionsActive.WithLabelValues(broker, role).Set(float64(count))
}

// RecordError records an error
func RecordError(broker, errorType string) {
	Errors.WithLabelValues(broker, errorType).Inc()
}

// SetThroughput sets the current throughput
func SetThroughput(broker, direction, qos, retained, federation string, rate float64) {
	Throughput.WithLabelValues(broker, direction, qos, retained, federation).Set(rate)
}
