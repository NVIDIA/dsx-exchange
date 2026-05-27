// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	Broker      string
	ClientID    string
	Username    string
	Password    string
	QoS         byte
	TLS         bool
	TLSCert     string // Path to client certificate
	TLSKey      string // Path to client key
	TLSCA       string // Path to CA certificate
	TLSInsecure bool   // Skip TLS verification
}

type Client struct {
	config Config
	client mqtt.Client
}

func New(config Config) (*Client, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(config.Broker)
	opts.SetClientID(config.ClientID)

	if config.Username != "" {
		opts.SetUsername(config.Username)
	}
	if config.Password != "" {
		opts.SetPassword(config.Password)
	}

	if config.TLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: config.TLSInsecure,
		}

		// Load CA certificate if provided (always load for proper validation)
		if config.TLSCA != "" {
			caCert, err := os.ReadFile(config.TLSCA)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse CA certificate")
			}
			tlsConfig.RootCAs = caCertPool
		}

		// Load client certificate if provided (mTLS)
		if config.TLSCert != "" && config.TLSKey != "" {
			cert, err := tls.LoadX509KeyPair(config.TLSCert, config.TLSKey)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		opts.SetTLSConfig(tlsConfig)
	}

	opts.SetCleanSession(true)
	opts.SetAutoReconnect(false) // Disable auto-reconnect for tests
	opts.SetConnectRetry(false)  // Disable connect retry for fast failure
	opts.SetConnectTimeout(5 * time.Second)

	client := mqtt.NewClient(opts)

	return &Client{
		config: config,
		client: client,
	}, nil
}

func (c *Client) Connect() error {
	token := c.client.Connect()
	token.Wait()
	return token.Error()
}

func (c *Client) Disconnect() {
	c.client.Disconnect(250)
}

func (c *Client) Publish(topic string, payload []byte, qos byte, retain bool) error {
	return c.PublishContext(context.Background(), topic, payload, qos, retain)
}

func (c *Client) PublishContext(ctx context.Context, topic string, payload []byte, qos byte, retain bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	token := c.client.Publish(topic, qos, retain, payload)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-token.Done():
		return token.Error()
	}
}

func (c *Client) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) error {
	token := c.client.Subscribe(topic, qos, callback)
	token.Wait()

	if err := token.Error(); err != nil {
		return err
	}

	// Check for SUBACK failure codes (workaround for Paho Go client bug)
	// The client receives SUBACK 0x80 (failure) but token.Error() returns nil
	if st, ok := token.(*mqtt.SubscribeToken); ok {
		for t, code := range st.Result() {
			if code == 0x80 {
				return fmt.Errorf("subscription denied for topic %s (SUBACK 0x80)", t)
			} else if code > 2 {
				return fmt.Errorf("subscription failed for topic %s (SUBACK 0x%02x)", t, code)
			}
		}
	}

	return nil
}

func (c *Client) Unsubscribe(topic string) error {
	token := c.client.Unsubscribe(topic)
	token.Wait()
	return token.Error()
}

func (c *Client) IsConnected() bool {
	return c.client.IsConnected()
}

func DefaultMessageHandler(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("Topic: %s\n", msg.Topic())
	fmt.Printf("Payload: %s\n", string(msg.Payload()))
	fmt.Printf("QoS: %d\n", msg.Qos())
	fmt.Printf("Retained: %v\n", msg.Retained())
	fmt.Println("---")
}
