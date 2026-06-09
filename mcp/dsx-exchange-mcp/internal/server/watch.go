// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/mqttbus"
)

const (
	watchStatusStarting = "starting"
	watchStatusRunning  = "running"
	watchStatusExpired  = "expired"
	watchStatusFailed   = "failed"
	watchStatusStopped  = "stopped"

	codeStatefulSessionRequired = "stateful_session_required"
	codeSubscriptionNotFound    = "subscription_not_found"
	codeSubscriptionNotOwner    = "subscription_not_owner"
	codeSchemaTopicNotFound     = "schema_topic_not_found"
	codeSchemaTopicAmbiguous    = "schema_topic_ambiguous"
	codeBufferOverflow          = "buffer_overflow"
)

const (
	finishedWatchRetention     = 5 * time.Minute
	maxWatchStatusUpdates      = 50
	maxWatchStatusPayloadBytes = 4096
)

type streamRunner func(context.Context, mqttbus.Config, string, string, mqttbus.StreamOptions, func(mqttbus.Message) error) (mqttbus.StreamResult, error)

type watchManager struct {
	cfg    Config
	runner streamRunner

	mu          sync.Mutex
	watches     map[string]map[string]*watch
	total       int
	activeTotal int
	now         func() time.Time
	newID       func() string
	retention   time.Duration
}

type watch struct {
	id          string
	sessionID   string
	authKey     string
	topicFilter string
	status      string
	createdAt   time.Time
	expiresAt   time.Time
	finishedAt  time.Time
	lastMessage time.Time
	lastError   *errorBody

	cursor         int64
	droppedCount   int64
	messageCount   int64
	bufferBytes    int
	buffer         []bufferedWatchMessage
	updates        map[string]*watchTopicUpdate
	updatesDropped int64

	maxMessages int
	maxBytes    int
	cancel      context.CancelFunc
	stopped     bool
	active      bool
}

type bufferedWatchMessage struct {
	cursor string
	size   int
	msg    mqttbus.Message
}

type watchStartRequest struct {
	Caller            auth.Caller
	TopicFilter       string
	TTLS              int
	BufferMaxMessages int
	BufferMaxBytes    int
}

type watchReadRequest struct {
	Caller         auth.Caller
	SubscriptionID string
	Cursor         string
	MaxMessages    int
	MaxBytes       int
}

type watchStatusRequest struct {
	Caller         auth.Caller
	SubscriptionID string
}

type watchStopRequest struct {
	Caller         auth.Caller
	SubscriptionID string
}

type watchLimitsOutput struct {
	TTLSeconds        int    `json:"ttl_seconds"`
	BufferMaxMessages int    `json:"buffer_max_messages"`
	BufferMaxBytes    int    `json:"buffer_max_bytes"`
	OverflowPolicy    string `json:"overflow_policy"`
}

type watchWatermark struct {
	OldestCursor string `json:"oldest_cursor"`
	NewestCursor string `json:"newest_cursor"`
}

type watchMessageOutput struct {
	Cursor          string    `json:"cursor"`
	Topic           string    `json:"topic"`
	Payload         string    `json:"payload"`
	PayloadEncoding string    `json:"payload_encoding"`
	Retained        bool      `json:"retained"`
	QoS             byte      `json:"qos"`
	ReceivedAt      time.Time `json:"received_at"`
}

type watchTopicUpdate struct {
	topic                  string
	count                  int64
	latestCursor           string
	latestPayload          string
	latestPayloadEncoding  string
	latestPayloadTruncated bool
	retained               bool
	qos                    byte
	latestReceivedAt       time.Time
	numeric                *watchNumericAggregate
}

type watchTopicUpdateOutput struct {
	Topic                  string              `json:"topic"`
	Count                  int64               `json:"count"`
	LatestCursor           string              `json:"latest_cursor"`
	LatestPayload          string              `json:"latest_payload,omitempty"`
	LatestPayloadEncoding  string              `json:"latest_payload_encoding,omitempty"`
	LatestPayloadTruncated bool                `json:"latest_payload_truncated,omitempty"`
	Retained               bool                `json:"retained"`
	QoS                    byte                `json:"qos"`
	LatestReceivedAt       time.Time           `json:"latest_received_at"`
	Numeric                *watchNumericOutput `json:"numeric,omitempty"`
}

type watchNumericAggregate struct {
	field  string
	count  int64
	min    float64
	max    float64
	sum    float64
	latest float64
}

type watchNumericOutput struct {
	Field  string  `json:"field"`
	Count  int64   `json:"count"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	Latest float64 `json:"latest"`
}

type watchStartOutput struct {
	SubscriptionID              string            `json:"subscription_id"`
	Status                      string            `json:"status"`
	TopicFilter                 string            `json:"topic_filter"`
	Cursor                      string            `json:"cursor"`
	ExpiresAt                   time.Time         `json:"expires_at"`
	RecommendedReadAfterSeconds int               `json:"recommended_read_after_seconds"`
	Limits                      watchLimitsOutput `json:"limits"`
}

type watchReadOutput struct {
	SubscriptionID  string               `json:"subscription_id"`
	Status          string               `json:"status"`
	Messages        []watchMessageOutput `json:"messages"`
	Count           int                  `json:"count"`
	NextCursor      string               `json:"next_cursor"`
	DroppedCount    int64                `json:"dropped_count"`
	BufferWatermark watchWatermark       `json:"buffer_watermark"`
	ExpiresAt       time.Time            `json:"expires_at"`
	LastError       *errorBody           `json:"last_error,omitempty"`
}

type watchStatusOutput struct {
	SubscriptionID   string                   `json:"subscription_id"`
	Status           string                   `json:"status"`
	TopicFilter      string                   `json:"topic_filter"`
	MessageCount     int64                    `json:"message_count"`
	DroppedCount     int64                    `json:"dropped_count"`
	UpdateCount      int                      `json:"update_count"`
	UpdatesDropped   int64                    `json:"updates_dropped"`
	UpdatesTruncated bool                     `json:"updates_truncated"`
	Updates          []watchTopicUpdateOutput `json:"updates,omitempty"`
	OldestCursor     string                   `json:"oldest_cursor"`
	NewestCursor     string                   `json:"newest_cursor"`
	ExpiresAt        time.Time                `json:"expires_at"`
	LastMessageAt    *time.Time               `json:"last_message_at,omitempty"`
	LastError        *errorBody               `json:"last_error,omitempty"`
	BufferWatermark  watchWatermark           `json:"buffer_watermark"`
}

type watchStopOutput struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	StoppedReason  string `json:"stopped_reason"`
	MessageCount   int64  `json:"message_count"`
	DroppedCount   int64  `json:"dropped_count"`
}

type streamFinished struct {
	result mqttbus.StreamResult
	err    error
}

func newWatchManager(cfg Config) *watchManager {
	return &watchManager{
		cfg:       cfg,
		runner:    mqttbus.Stream,
		watches:   map[string]map[string]*watch{},
		now:       time.Now,
		newID:     randomSubscriptionID,
		retention: finishedWatchRetention,
	}
}

func (m *watchManager) start(req watchStartRequest) (watchStartOutput, error) {
	if err := validateWatchCaller(req.Caller); err != nil {
		return watchStartOutput{}, err
	}
	if err := mqttbus.ValidateTopicFilter(req.TopicFilter); err != nil {
		return watchStartOutput{}, err
	}
	ttlS, bufferMessages, bufferBytes, err := m.applyStartLimits(req)
	if err != nil {
		return watchStartOutput{}, err
	}

	started := m.now()
	ctx, cancel := context.WithCancel(context.Background())
	w := &watch{
		id:          m.newID(),
		sessionID:   req.Caller.SessionID,
		authKey:     callerAuthKey(req.Caller),
		topicFilter: strings.TrimSpace(req.TopicFilter),
		status:      watchStatusStarting,
		createdAt:   started,
		expiresAt:   started.Add(time.Duration(ttlS) * time.Second),
		updates:     map[string]*watchTopicUpdate{},
		maxMessages: bufferMessages,
		maxBytes:    bufferBytes,
		cancel:      cancel,
		active:      true,
	}

	ready := make(chan struct{}, 1)
	finished := make(chan streamFinished, 1)

	if !m.cfg.watchStartAdmission.tryAcquire() {
		cancel()
		return watchStartOutput{}, admissionLimitedError()
	}
	releaseStartup := sync.OnceFunc(m.cfg.watchStartAdmission.release)

	m.mu.Lock()
	if m.activeTotal >= m.cfg.WatchMaxPerPod {
		m.mu.Unlock()
		releaseStartup()
		cancel()
		return watchStartOutput{}, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("active watch count exceeds per-pod cap %d", m.cfg.WatchMaxPerPod)}
	}
	sessionWatches := m.watches[w.sessionID]
	if activeSessionCount(sessionWatches) >= m.cfg.WatchMaxPerSession {
		m.mu.Unlock()
		releaseStartup()
		cancel()
		return watchStartOutput{}, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("active watch count exceeds per-session cap %d", m.cfg.WatchMaxPerSession)}
	}
	if sessionWatches == nil {
		sessionWatches = map[string]*watch{}
		m.watches[w.sessionID] = sessionWatches
	}
	sessionWatches[w.id] = w
	m.total++
	m.activeTotal++
	m.mu.Unlock()
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.BeginWatch()
	}

	go func() {
		result, err := m.runner(ctx, m.cfg.MQTT, req.Caller.Bearer, w.topicFilter, mqttbus.StreamOptions{
			ClientID:    "dsx-exchange-mcp-watch-" + w.id,
			MaxMessages: math.MaxInt32,
			MaxDuration: time.Duration(ttlS) * time.Second,
			OnSubscribed: func() {
				releaseStartup()
				m.markRunning(w.sessionID, w.id)
				select {
				case ready <- struct{}{}:
				default:
				}
			},
		}, func(msg mqttbus.Message) error {
			m.recordMessage(w.sessionID, w.id, msg)
			return nil
		})
		releaseStartup()
		m.finish(w.sessionID, w.id, result, err)
		select {
		case finished <- streamFinished{result: result, err: err}:
		default:
		}
	}()

	select {
	case <-ready:
		return m.startOutput(w.sessionID, w.id), nil
	case done := <-finished:
		if done.err != nil {
			m.remove(w.sessionID, w.id)
			return watchStartOutput{}, done.err
		}
		return m.startOutput(w.sessionID, w.id), nil
	case <-time.After(m.startWait()):
		return m.startOutput(w.sessionID, w.id), nil
	}
}

func (m *watchManager) read(req watchReadRequest) (watchReadOutput, error) {
	if err := validateWatchCaller(req.Caller); err != nil {
		return watchReadOutput{}, err
	}
	cursor, err := parseCursor(req.Cursor)
	if err != nil {
		return watchReadOutput{}, err
	}
	maxMessages, maxBytes, err := m.applyReadLimits(req.MaxMessages, req.MaxBytes)
	if err != nil {
		return watchReadOutput{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.lookupLocked(req.Caller, req.SubscriptionID)
	if err != nil {
		return watchReadOutput{}, err
	}

	messages := make([]watchMessageOutput, 0, maxMessages)
	bytes := 0
	nextCursor := strconv.FormatInt(w.cursor, 10)
	for _, buffered := range w.buffer {
		messageCursor, _ := strconv.ParseInt(buffered.cursor, 10, 64)
		if messageCursor <= cursor {
			continue
		}
		if len(messages) >= maxMessages {
			break
		}
		if len(messages) > 0 && bytes+buffered.size > maxBytes {
			break
		}
		bytes += buffered.size
		nextCursor = buffered.cursor
		messages = append(messages, watchMessageOutput{
			Cursor:          buffered.cursor,
			Topic:           buffered.msg.Topic,
			Payload:         buffered.msg.Payload,
			PayloadEncoding: buffered.msg.PayloadEncoding,
			Retained:        buffered.msg.Retained,
			QoS:             buffered.msg.QoS,
			ReceivedAt:      buffered.msg.ReceivedAt,
		})
	}

	return watchReadOutput{
		SubscriptionID:  w.id,
		Status:          w.status,
		Messages:        messages,
		Count:           len(messages),
		NextCursor:      nextCursor,
		DroppedCount:    w.droppedCount,
		BufferWatermark: w.watermark(),
		ExpiresAt:       w.expiresAt,
		LastError:       w.lastError,
	}, nil
}

func (m *watchManager) status(req watchStatusRequest) (watchStatusOutput, error) {
	if err := validateWatchCaller(req.Caller); err != nil {
		return watchStatusOutput{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	w, err := m.lookupLocked(req.Caller, req.SubscriptionID)
	if err != nil {
		return watchStatusOutput{}, err
	}
	updates, updatesTruncated := w.statusUpdates()
	out := watchStatusOutput{
		SubscriptionID:   w.id,
		Status:           w.status,
		TopicFilter:      w.topicFilter,
		MessageCount:     w.messageCount,
		DroppedCount:     w.droppedCount,
		UpdateCount:      len(w.updates),
		UpdatesDropped:   w.updatesDropped,
		UpdatesTruncated: updatesTruncated || w.updatesDropped > 0,
		Updates:          updates,
		OldestCursor:     w.oldestCursor(),
		NewestCursor:     strconv.FormatInt(w.cursor, 10),
		ExpiresAt:        w.expiresAt,
		LastError:        w.lastError,
		BufferWatermark:  w.watermark(),
	}
	if !w.lastMessage.IsZero() {
		last := w.lastMessage
		out.LastMessageAt = &last
	}
	return out, nil
}

func (m *watchManager) stop(req watchStopRequest) (watchStopOutput, error) {
	if err := validateWatchCaller(req.Caller); err != nil {
		return watchStopOutput{}, err
	}

	m.mu.Lock()
	w, err := m.lookupLocked(req.Caller, req.SubscriptionID)
	if err != nil {
		m.mu.Unlock()
		return watchStopOutput{}, err
	}
	out := watchStopOutput{
		SubscriptionID: w.id,
		Status:         watchStatusStopped,
		StoppedReason:  "user_requested",
		MessageCount:   w.messageCount,
		DroppedCount:   w.droppedCount,
	}
	w.stopped = true
	w.status = watchStatusStopped
	cancel := w.cancel
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.remove(req.Caller.SessionID, req.SubscriptionID)
	return out, nil
}

func (m *watchManager) applyStartLimits(req watchStartRequest) (int, int, int, error) {
	ttlS := req.TTLS
	if ttlS == 0 {
		ttlS = m.cfg.WatchDefaultTTLS
	}
	if ttlS <= 0 {
		return 0, 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "ttl_seconds must be greater than zero"}
	}
	if ttlS > m.cfg.WatchMaxTTLS {
		return 0, 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("ttl_seconds exceeds cap %d", m.cfg.WatchMaxTTLS)}
	}

	bufferMessages := req.BufferMaxMessages
	if bufferMessages == 0 {
		bufferMessages = m.cfg.WatchDefaultBufferMessages
	}
	if bufferMessages <= 0 {
		return 0, 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "buffer_max_messages must be greater than zero"}
	}
	if bufferMessages > m.cfg.WatchMaxBufferMessages {
		return 0, 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("buffer_max_messages exceeds cap %d", m.cfg.WatchMaxBufferMessages)}
	}

	bufferBytes := req.BufferMaxBytes
	if bufferBytes == 0 {
		bufferBytes = m.cfg.WatchDefaultBufferBytes
	}
	if bufferBytes <= 0 {
		return 0, 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "buffer_max_bytes must be greater than zero"}
	}
	if bufferBytes > m.cfg.WatchMaxBufferBytes {
		return 0, 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("buffer_max_bytes exceeds cap %d", m.cfg.WatchMaxBufferBytes)}
	}
	return ttlS, bufferMessages, bufferBytes, nil
}

func (m *watchManager) applyReadLimits(maxMessages, maxBytes int) (int, int, error) {
	if maxMessages == 0 {
		maxMessages = m.cfg.WatchDefaultBufferMessages
	}
	if maxMessages <= 0 {
		return 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "max_messages must be greater than zero"}
	}
	if maxMessages > m.cfg.WatchMaxBufferMessages {
		return 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("max_messages exceeds cap %d", m.cfg.WatchMaxBufferMessages)}
	}
	if maxBytes == 0 {
		maxBytes = m.cfg.WatchDefaultBufferBytes
	}
	if maxBytes <= 0 {
		return 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "max_bytes must be greater than zero"}
	}
	if maxBytes > m.cfg.WatchMaxBufferBytes {
		return 0, 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: fmt.Sprintf("max_bytes exceeds cap %d", m.cfg.WatchMaxBufferBytes)}
	}
	return maxMessages, maxBytes, nil
}

func (m *watchManager) startWait() time.Duration {
	timeout := m.cfg.MQTT.ConnectTimeout + m.cfg.MQTT.SubscribeTimeout + time.Second
	if timeout <= time.Second {
		return 11 * time.Second
	}
	return timeout
}

func (m *watchManager) startOutput(sessionID, subscriptionID string) watchStartOutput {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.watches[sessionID][subscriptionID]
	if w == nil {
		return watchStartOutput{}
	}
	return watchStartOutput{
		SubscriptionID:              w.id,
		Status:                      w.status,
		TopicFilter:                 w.topicFilter,
		Cursor:                      strconv.FormatInt(w.cursor, 10),
		ExpiresAt:                   w.expiresAt,
		RecommendedReadAfterSeconds: 30,
		Limits: watchLimitsOutput{
			TTLSeconds:        int(w.expiresAt.Sub(w.createdAt).Seconds()),
			BufferMaxMessages: w.maxMessages,
			BufferMaxBytes:    w.maxBytes,
			OverflowPolicy:    "drop_oldest",
		},
	}
}

func (m *watchManager) markRunning(sessionID, subscriptionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w := m.watches[sessionID][subscriptionID]; w != nil && w.status == watchStatusStarting {
		w.status = watchStatusRunning
	}
}

func (m *watchManager) recordMessage(sessionID, subscriptionID string, msg mqttbus.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.watches[sessionID][subscriptionID]
	if w == nil {
		return
	}
	w.cursor++
	w.messageCount++
	w.lastMessage = msg.ReceivedAt
	cursor := strconv.FormatInt(w.cursor, 10)
	w.recordTopicUpdate(cursor, msg)
	size := len(msg.Topic) + len(msg.Payload)
	w.buffer = append(w.buffer, bufferedWatchMessage{
		cursor: cursor,
		size:   size,
		msg:    msg,
	})
	w.bufferBytes += size
	droppedCount := int64(0)
	for len(w.buffer) > w.maxMessages || w.bufferBytes > w.maxBytes {
		dropped := w.buffer[0]
		w.buffer = w.buffer[1:]
		w.bufferBytes -= dropped.size
		w.droppedCount++
		droppedCount++
	}
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.RecordWatchMessage()
		m.cfg.Metrics.RecordWatchDrop(droppedCount)
	}
}

func (w *watch) recordTopicUpdate(cursor string, msg mqttbus.Message) {
	if w.updates == nil {
		w.updates = map[string]*watchTopicUpdate{}
	}
	update := w.updates[msg.Topic]
	if update == nil {
		if len(w.updates) >= maxWatchStatusUpdates {
			w.evictOldestTopicUpdate()
		}
		update = &watchTopicUpdate{topic: msg.Topic}
		w.updates[msg.Topic] = update
	}
	payload, truncated := truncatePayloadSample(msg.Payload, maxWatchStatusPayloadBytes)
	update.count++
	update.latestCursor = cursor
	update.latestPayload = payload
	update.latestPayloadEncoding = msg.PayloadEncoding
	update.latestPayloadTruncated = truncated
	update.retained = msg.Retained
	update.qos = msg.QoS
	update.latestReceivedAt = msg.ReceivedAt
	if field, value, ok := extractNumericPayloadValue(msg.Payload); ok {
		update.recordNumeric(field, value)
	}
}

func (w *watch) evictOldestTopicUpdate() {
	var oldest *watchTopicUpdate
	oldestTopic := ""
	for topic, update := range w.updates {
		if oldest == nil ||
			update.latestReceivedAt.Before(oldest.latestReceivedAt) ||
			(update.latestReceivedAt.Equal(oldest.latestReceivedAt) && topic < oldestTopic) {
			oldest = update
			oldestTopic = topic
		}
	}
	if oldestTopic == "" {
		return
	}
	delete(w.updates, oldestTopic)
	w.updatesDropped++
}

func (w *watch) statusUpdates() ([]watchTopicUpdateOutput, bool) {
	updates := make([]watchTopicUpdateOutput, 0, len(w.updates))
	for _, update := range w.updates {
		var numeric *watchNumericOutput
		if update.numeric != nil {
			numeric = update.numeric.output()
		}
		updates = append(updates, watchTopicUpdateOutput{
			Topic:                  update.topic,
			Count:                  update.count,
			LatestCursor:           update.latestCursor,
			LatestPayload:          update.latestPayload,
			LatestPayloadEncoding:  update.latestPayloadEncoding,
			LatestPayloadTruncated: update.latestPayloadTruncated,
			Retained:               update.retained,
			QoS:                    update.qos,
			LatestReceivedAt:       update.latestReceivedAt,
			Numeric:                numeric,
		})
	}
	sort.Slice(updates, func(i, j int) bool {
		left := updates[i]
		right := updates[j]
		if !left.LatestReceivedAt.Equal(right.LatestReceivedAt) {
			return left.LatestReceivedAt.After(right.LatestReceivedAt)
		}
		return left.Topic < right.Topic
	})
	if len(updates) <= maxWatchStatusUpdates {
		return updates, false
	}
	return updates[:maxWatchStatusUpdates], true
}

func (u *watchTopicUpdate) recordNumeric(field string, value float64) {
	if u.numeric == nil || u.numeric.field != field {
		u.numeric = &watchNumericAggregate{
			field:  field,
			count:  1,
			min:    value,
			max:    value,
			sum:    value,
			latest: value,
		}
		return
	}
	u.numeric.count++
	u.numeric.sum += value
	u.numeric.latest = value
	if value < u.numeric.min {
		u.numeric.min = value
	}
	if value > u.numeric.max {
		u.numeric.max = value
	}
}

func (a *watchNumericAggregate) output() *watchNumericOutput {
	if a == nil || a.count == 0 {
		return nil
	}
	return &watchNumericOutput{
		Field:  a.field,
		Count:  a.count,
		Min:    a.min,
		Max:    a.max,
		Mean:   a.sum / float64(a.count),
		Latest: a.latest,
	}
}

func extractNumericPayloadValue(payload string) (string, float64, bool) {
	var body map[string]any
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		return "", 0, false
	}
	if value, ok := numericJSONValue(body["value"]); ok {
		return "value", value, true
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return "", 0, false
	}
	if value, ok := numericJSONValue(data["value"]); ok {
		return "data.value", value, true
	}
	return "", 0, false
}

func numericJSONValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func truncatePayloadSample(payload string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(payload) <= maxBytes {
		return payload, false
	}
	sample := payload[:maxBytes]
	for len(sample) > 0 && !utf8.ValidString(sample) {
		sample = sample[:len(sample)-1]
	}
	return sample, true
}

func (m *watchManager) finish(sessionID, subscriptionID string, result mqttbus.StreamResult, err error) {
	m.mu.Lock()
	w := m.watches[sessionID][subscriptionID]
	if w == nil {
		m.mu.Unlock()
		return
	}
	endWatch := false
	if w.active {
		w.active = false
		m.activeTotal--
		endWatch = true
	}
	switch {
	case w.stopped:
		w.status = watchStatusStopped
	case err != nil:
		w.status = watchStatusFailed
		w.lastError = &errorBody{Code: errorCode(err), Message: publicMessage(err)}
	case result.StoppedReason == mqttbus.StoppedMaxDuration:
		w.status = watchStatusExpired
	case result.StoppedReason == mqttbus.StoppedMaxMessages:
		w.status = watchStatusFailed
		w.lastError = &errorBody{Code: codeBufferOverflow, Message: "watch stream reached internal message cap"}
	default:
		w.status = watchStatusFailed
		if result.StoppedReason != "" {
			w.lastError = &errorBody{Code: result.StoppedReason, Message: "watch stream stopped"}
		}
	}
	w.finishedAt = m.now()
	m.mu.Unlock()
	if endWatch && m.cfg.Metrics != nil {
		m.cfg.Metrics.EndWatch()
	}

	if w.status == watchStatusExpired || w.status == watchStatusFailed {
		time.AfterFunc(m.retention, func() {
			m.remove(sessionID, subscriptionID)
		})
	}
}

func (m *watchManager) lookupLocked(caller auth.Caller, subscriptionID string) (*watch, error) {
	if strings.TrimSpace(subscriptionID) == "" {
		return nil, &mqttbus.BusError{Code: codeSubscriptionNotFound, Message: "subscription_id is required"}
	}
	sessionWatches := m.watches[caller.SessionID]
	if sessionWatches == nil {
		return nil, &mqttbus.BusError{Code: codeSubscriptionNotFound, Message: "subscription is not active on this MCP session; it may have expired, been stopped, or been lost due to pod restart"}
	}
	w := sessionWatches[subscriptionID]
	if w == nil {
		return nil, &mqttbus.BusError{Code: codeSubscriptionNotFound, Message: "subscription is not active on this MCP session; it may have expired, been stopped, or been lost due to pod restart"}
	}
	if w.authKey != callerAuthKey(caller) {
		return nil, &mqttbus.BusError{Code: codeSubscriptionNotOwner, Message: "caller does not own this subscription"}
	}
	return w, nil
}

func (m *watchManager) remove(sessionID, subscriptionID string) {
	m.mu.Lock()
	sessionWatches := m.watches[sessionID]
	if sessionWatches == nil || sessionWatches[subscriptionID] == nil {
		m.mu.Unlock()
		return
	}
	w := sessionWatches[subscriptionID]
	endWatch := false
	if w.active {
		w.active = false
		m.activeTotal--
		endWatch = true
	}
	delete(sessionWatches, subscriptionID)
	m.total--
	if len(sessionWatches) == 0 {
		delete(m.watches, sessionID)
	}
	m.mu.Unlock()
	if endWatch && m.cfg.Metrics != nil {
		m.cfg.Metrics.EndWatch()
	}
}

func activeSessionCount(sessionWatches map[string]*watch) int {
	count := 0
	for _, w := range sessionWatches {
		if w != nil && w.active {
			count++
		}
	}
	return count
}

func (w *watch) oldestCursor() string {
	if len(w.buffer) == 0 {
		return strconv.FormatInt(w.cursor, 10)
	}
	return w.buffer[0].cursor
}

func (w *watch) watermark() watchWatermark {
	return watchWatermark{
		OldestCursor: w.oldestCursor(),
		NewestCursor: strconv.FormatInt(w.cursor, 10),
	}
}

func validateWatchCaller(caller auth.Caller) error {
	if strings.TrimSpace(caller.SessionID) == "" {
		return &mqttbus.BusError{Code: codeStatefulSessionRequired, Message: "background subscriptions require Mcp-Session-Id stateful routing"}
	}
	if strings.TrimSpace(caller.Bearer) == "" {
		return &mqttbus.BusError{Code: mqttbus.CodeMissingBearer, Message: "missing caller bearer; gateway should pass Authorization through"}
	}
	return nil
}

func callerAuthKey(caller auth.Caller) string {
	return strings.Join([]string{
		caller.Tenant,
		caller.Issuer,
		caller.Subject,
		caller.SpiffeID,
	}, "\x00")
}

func parseCursor(cursor string) (int64, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || parsed < 0 {
		return 0, &mqttbus.BusError{Code: mqttbus.CodeInvalidArgument, Message: "cursor must be a non-negative integer string"}
	}
	return parsed, nil
}

func randomSubscriptionID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "sub_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "sub_" + hex.EncodeToString(raw[:])
}
