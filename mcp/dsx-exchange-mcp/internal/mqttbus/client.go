// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mqttbus

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	DefaultUsername = "oauthtoken"
	DefaultAuthMode = AuthModeJWTPassthrough

	CodeMissingBearer           = "missing_bearer"
	CodeInvalidTopicFilter      = "invalid_topic_filter"
	CodeInvalidArgument         = "invalid_argument"
	CodeTLSConfigError          = "tls_config_error"
	CodeTLSHandshakeFailed      = "tls_handshake_failed"
	CodeBusUnavailable          = "bus_unavailable"
	CodeMQTTAuthFailed          = "mqtt_auth_failed"
	CodeTopicACLDenied          = "topic_acl_denied"
	CodeMQTTAuthorizationFailed = "mqtt_authorization_failed"
	CodeMQTTSubscribeFailed     = "mqtt_subscribe_failed"
	CodeMQTTAdmissionLimited    = "mqtt_admission_limited"
	CodeInternalError           = "internal_error"
)

const (
	StoppedMaxMessages    = "max_messages"
	StoppedMaxDuration    = "max_duration"
	StoppedRetainedIdle   = "retained_idle"
	StoppedCallerCancel   = "caller_cancelled"
	StoppedResultTooLarge = "result_too_large"
)

type AuthMode string

const (
	AuthModeJWTPassthrough AuthMode = "jwt_passthrough"
	AuthModeNoAuth         AuthMode = "noauth"
)

type Config struct {
	BrokerURL        string
	Username         string
	AuthMode         AuthMode
	TLS              TLSConfig
	ConnectTimeout   time.Duration
	SubscribeTimeout time.Duration
	MaxResultBytes   int
}

type TLSConfig struct {
	CAFile             string
	ServerName         string
	InsecureSkipVerify bool
}

// Message is a single MQTT message captured from the bus.
type Message struct {
	Topic           string    `json:"topic"`
	Payload         string    `json:"payload"`
	PayloadEncoding string    `json:"payload_encoding"`
	Retained        bool      `json:"retained"`
	QoS             byte      `json:"qos"`
	ReceivedAt      time.Time `json:"received_at"`
}

type CollectResult struct {
	Messages      []Message     `json:"messages"`
	StoppedReason string        `json:"stopped_reason"`
	Truncated     bool          `json:"truncated"`
	Duration      time.Duration `json:"-"`
}

type BusError struct {
	Code              string
	Message           string
	Err               error
	RetryAfterSeconds int
}

func (e *BusError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *BusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ErrorCode(err error) string {
	var busErr *BusError
	if errors.As(err, &busErr) {
		return busErr.Code
	}
	if err == nil {
		return ""
	}
	return CodeInternalError
}

func NormalizeAuthMode(mode AuthMode) (AuthMode, error) {
	switch mode {
	case "":
		return DefaultAuthMode, nil
	case AuthModeJWTPassthrough, AuthModeNoAuth:
		return mode, nil
	default:
		return "", &BusError{
			Code:    CodeInvalidArgument,
			Message: fmt.Sprintf("unsupported MQTT auth mode %q; use %q or %q", mode, AuthModeJWTPassthrough, AuthModeNoAuth),
		}
	}
}

func (cfg Config) Validate() error {
	_, err := NormalizeAuthMode(cfg.AuthMode)
	return err
}

func configureClientAuth(opts *mqtt.ClientOptions, cfg Config, bearer string) error {
	mode, err := NormalizeAuthMode(cfg.AuthMode)
	if err != nil {
		return err
	}
	switch mode {
	case AuthModeJWTPassthrough:
		if bearer == "" {
			return &BusError{Code: CodeMissingBearer, Message: "missing Authorization bearer for jwt_passthrough MQTT auth mode"}
		}
		username := cfg.Username
		if username == "" {
			username = DefaultUsername
		}
		opts.SetUsername(username)
		opts.SetPassword(bearer)
	case AuthModeNoAuth:
		// Intentionally omit username/password. Event Bus noauth matches only
		// when no OAuth2, mTLS, or NKey credentials are present.
	}
	return nil
}

// Collect opens a one-shot MQTT connection, subscribes to topicFilter, and
// returns up to maxMessages messages or until maxDuration elapses. The caller's
// bearer is passed as the MQTT password in jwt_passthrough mode; noauth mode
// sends no MQTT username/password. Event Bus auth-callout owns token
// validation, anonymous profile matching, and topic ACL enforcement.
func Collect(
	ctx context.Context,
	cfg Config,
	bearer, topicFilter string,
	maxMessages int,
	maxDuration time.Duration,
	retainedOnly bool,
) (CollectResult, error) {
	start := time.Now()
	out := CollectResult{}

	if err := ValidateTopicFilter(topicFilter); err != nil {
		return out, err
	}
	if maxMessages <= 0 {
		return out, &BusError{Code: CodeInvalidArgument, Message: "max_messages must be greater than zero"}
	}
	if maxDuration <= 0 {
		return out, &BusError{Code: CodeInvalidArgument, Message: "max_duration_s must be greater than zero"}
	}
	if cfg.BrokerURL == "" {
		return out, &BusError{Code: CodeInvalidArgument, Message: "broker URL is required"}
	}

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	subscribeTimeout := cfg.SubscribeTimeout
	if subscribeTimeout <= 0 {
		subscribeTimeout = 5 * time.Second
	}

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(fmt.Sprintf("dsx-exchange-mcp-%d", time.Now().UnixNano())).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectTimeout(connectTimeout)
	if err := configureClientAuth(opts, cfg, bearer); err != nil {
		return out, err
	}

	if usesTLS(cfg.BrokerURL) || cfg.TLS.CAFile != "" || cfg.TLS.ServerName != "" || cfg.TLS.InsecureSkipVerify {
		tlsCfg, err := buildTLSConfig(cfg.TLS)
		if err != nil {
			return out, err
		}
		opts.SetTLSConfig(tlsCfg)
	}

	var (
		mu          sync.Mutex
		messages    = make([]Message, 0, maxMessages)
		resultBytes int
		truncated   bool
		done        = make(chan string, 1)
		messageSeen = make(chan struct{}, 1)
		closed      bool
	)
	finish := func(reason string) {
		if !closed {
			closed = true
			done <- reason
		}
	}
	// Mutex ensures thread-safe adding of incoming MQTT messages and checks message size limits
	// Ensures messages are processed and appended in order, preventing overlap from concurrent handler calls.

	opts.SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		msg := convertMessage(m)
		nextBytes := resultBytes + len(msg.Topic) + len(msg.Payload)
		if cfg.MaxResultBytes > 0 && nextBytes > cfg.MaxResultBytes {
			truncated = true
			finish(StoppedResultTooLarge)
			return
		}
		resultBytes = nextBytes
		messages = append(messages, msg)
		select {
		case messageSeen <- struct{}{}:
		default:
		}
		if len(messages) >= maxMessages {
			finish(StoppedMaxMessages)
		}
	})

	c := mqtt.NewClient(opts)
	if tok := c.Connect(); !tok.WaitTimeout(connectTimeout) {
		return out, &BusError{Code: CodeBusUnavailable, Message: "mqtt connect timeout"}
	} else if tok.Error() != nil {
		return out, classifyConnectError(tok.Error())
	}
	defer c.Disconnect(250)

	if tok := c.Subscribe(topicFilter, 0, nil); !tok.WaitTimeout(subscribeTimeout) {
		return out, &BusError{Code: CodeBusUnavailable, Message: fmt.Sprintf("mqtt subscribe %q timeout", topicFilter)}
	} else if tok.Error() != nil {
		return out, classifySubscribeError(topicFilter, tok.Error())
	} else if err := classifySubscribeResult(topicFilter, tok); err != nil {
		return out, err
	}

	deadline := time.NewTimer(maxDuration)
	defer deadline.Stop()

	var idle *time.Timer
	if retainedOnly {
		idle = time.NewTimer(750 * time.Millisecond)
		defer idle.Stop()
	}

	for {
		var idleC <-chan time.Time
		if idle != nil {
			idleC = idle.C
		}
		select {
		case <-ctx.Done():
			// MCP client disconnects
			mu.Lock()
			finish(StoppedCallerCancel)
			out.Messages = append([]Message(nil), messages...)
			out.StoppedReason = StoppedCallerCancel
			out.Truncated = truncated
			out.Duration = time.Since(start)
			mu.Unlock()
			return out, ctx.Err()
		case <-deadline.C:
			// Exceeded max duration
			mu.Lock()
			finish(StoppedMaxDuration)
			out.Messages = append([]Message(nil), messages...)
			out.StoppedReason = StoppedMaxDuration
			out.Truncated = truncated
			out.Duration = time.Since(start)
			mu.Unlock()
			return out, nil
		case <-messageSeen:
			// Only relevant in retained-only mode:
			// Checks if no new messages have been received in the last 750ms.
			// Resets idle timer to 750ms.
			if idle != nil {
				if !idle.Stop() {
					select {
					case <-idle.C: // drains stale timer channel to prevent premature expiration.
					default:
					}
				}
				idle.Reset(750 * time.Millisecond) // reset idle timer
			}
		case <-idleC:
			// Only relevant in retained-only mode:
			// Finish reading retained messages after 750ms of inactivity.
			mu.Lock()
			finish(StoppedRetainedIdle)
			out.Messages = append([]Message(nil), messages...)
			out.StoppedReason = StoppedRetainedIdle
			out.Truncated = truncated
			out.Duration = time.Since(start)
			mu.Unlock()
			return out, nil
		case reason := <-done:
			// Exceeded Max Number / Size of Messages
			mu.Lock()
			out.Messages = append([]Message(nil), messages...)
			out.StoppedReason = reason
			out.Truncated = truncated
			out.Duration = time.Since(start)
			mu.Unlock()
			return out, nil
		}
	}
}

func ValidateTopicFilter(filter string) error {
	if filter == "" {
		return &BusError{Code: CodeInvalidTopicFilter, Message: "topic_filter is required"}
	}
	levels := strings.Split(filter, "/")
	for i, level := range levels {
		if strings.Contains(level, "#") {
			if level != "#" {
				return &BusError{Code: CodeInvalidTopicFilter, Message: "# wildcard must occupy an entire topic level"}
			}
			if i != len(levels)-1 {
				return &BusError{Code: CodeInvalidTopicFilter, Message: "# wildcard must be the final topic level"}
			}
		}
		if strings.Contains(level, "+") && level != "+" {
			return &BusError{Code: CodeInvalidTopicFilter, Message: "+ wildcard must occupy an entire topic level"}
		}
	}
	return nil
}

func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	if cfg.CAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		body, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, &BusError{Code: CodeTLSConfigError, Message: "read MQTT TLS CA file", Err: err}
		}
		if !pool.AppendCertsFromPEM(body) {
			return nil, &BusError{Code: CodeTLSConfigError, Message: "MQTT TLS CA file contains no PEM certificates"}
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}

func usesTLS(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "tls://") || strings.HasPrefix(lower, "ssl://") || strings.HasPrefix(lower, "mqtts://")
}

// convertMessage converts an mqtt.Message to a Message struct, storing the payload as a UTF-8 string if valid,
// or base64-encoding it otherwise. Sets encoding and message metadata accordingly.
func convertMessage(m mqtt.Message) Message {
	payload := m.Payload()
	encoding := "utf8"
	body := string(payload)
	if !utf8.Valid(payload) {
		encoding = "base64"
		body = base64.StdEncoding.EncodeToString(payload)
	}
	return Message{
		Topic:           m.Topic(),
		Payload:         body,
		PayloadEncoding: encoding,
		Retained:        m.Retained(),
		QoS:             m.Qos(),
		ReceivedAt:      time.Now().UTC(),
	}
}

func classifyConnectError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case looksTLS(msg):
		return &BusError{Code: CodeTLSHandshakeFailed, Message: "mqtt TLS handshake failed", Err: err}
	case looksAuth(msg):
		return &BusError{Code: CodeMQTTAuthFailed, Message: "mqtt broker rejected OAuth2 credentials", Err: err}
	case looksUnavailable(msg):
		return &BusError{Code: CodeBusUnavailable, Message: "mqtt broker unavailable", Err: err}
	default:
		return &BusError{Code: CodeBusUnavailable, Message: "mqtt connect failed", Err: err}
	}
}

func classifySubscribeError(topic string, err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case looksAuth(msg):
		return &BusError{Code: CodeTopicACLDenied, Message: fmt.Sprintf("mqtt subscribe %q denied by broker ACL", topic), Err: err}
	case looksUnavailable(msg):
		return &BusError{Code: CodeBusUnavailable, Message: fmt.Sprintf("mqtt subscribe %q failed because broker is unavailable", topic), Err: err}
	default:
		return &BusError{Code: CodeMQTTSubscribeFailed, Message: fmt.Sprintf("mqtt subscribe %q failed", topic), Err: err}
	}
}

func classifySubscribeResult(topic string, tok mqtt.Token) error {
	subTok, ok := tok.(*mqtt.SubscribeToken)
	if !ok {
		return nil
	}
	return classifySubscribeResultCode(topic, subTok.Result())
}

func classifySubscribeResultCode(topic string, result map[string]byte) error {
	code, ok := result[topic]
	if !ok {
		return &BusError{Code: CodeMQTTSubscribeFailed, Message: fmt.Sprintf("mqtt subscribe %q returned no broker result", topic)}
	}
	switch code {
	case 0, 1, 2:
		return nil
	case 0x80:
		return &BusError{Code: CodeTopicACLDenied, Message: fmt.Sprintf("mqtt subscribe %q denied by broker ACL", topic)}
	default:
		return &BusError{Code: CodeMQTTSubscribeFailed, Message: fmt.Sprintf("mqtt subscribe %q returned invalid SUBACK code 0x%02x", topic, code)}
	}
}

func looksAuth(msg string) bool {
	return strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "not authorised") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "unauthorised") ||
		strings.Contains(msg, "bad user") ||
		strings.Contains(msg, "username") ||
		strings.Contains(msg, "password") ||
		strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "authorization") ||
		strings.Contains(msg, "permission") ||
		strings.Contains(msg, "acl") ||
		strings.Contains(msg, "forbidden")
}

func looksTLS(msg string) bool {
	return strings.Contains(msg, "tls") ||
		strings.Contains(msg, "x509") ||
		strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "unknown authority")
}

func looksUnavailable(msg string) bool {
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "broken pipe")
}
