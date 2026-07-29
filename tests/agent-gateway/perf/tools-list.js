/**
 * Copyright 2026 NVIDIA Corporation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// k6 perf — tools/list latency through DSX Agent Gateway.
// setup() does the MCP initialize + gets a session ID; each VU iteration
// reuses that session for tools/list.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';

// Per-iteration counters so we can threshold on the absolute count
// of successful 200 responses, not just rates relative to the total.
// A run that returns nothing but 429s would otherwise satisfy the
// failure-rate and latency thresholds while proving nothing about
// the load path.
const success200 = new Counter('success_200');
const PROFILE = (__ENV.RUN_PERF_PROFILE || (__ENV.RUN_PERF_BENCHMARK === '1' ? 'benchmark' : 'smoke')).toLowerCase();
const BENCHMARK = PROFILE === 'benchmark';
const TARGET_VUS = Number(__ENV.RUN_PERF_TARGET_VUS || (BENCHMARK ? '100' : '20'));
const SUCCESS_200_MIN = Number(__ENV.RUN_PERF_SUCCESS_200_MIN || (BENCHMARK ? '900' : '30'));
const POOL = Number(__ENV.RUN_PERF_SESSION_POOL || (BENCHMARK ? '10' : '4'));

// Smoke profile: short local confidence. Benchmark profile:
// ramp to 100 VUs over 10s, hold 60s, ramp down 5s.
// k6 runs on the host against the same gateway URL the Go functional
// suite uses. In kind that is the NodePort-mapped localhost path.
//
// The profile saturates the per-tenant 30 TPS rate limit, so 429s are
// the normal outcome for most requests. Treat 429 as an expected
// response (not an error) via responseCallback so http_req_failed
// reflects load-path breakage (5xx, connection errors, malformed
// 4xx not in the limiter's set) rather than rate-limit enforcement.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 429));

export const options = {
  // k6's default Trend stats omit p(99); declare here so it
  // lands in the summary export.
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(50)', 'p(95)', 'p(99)'],
  stages: BENCHMARK ? [
    { duration: '10s', target: TARGET_VUS },
    { duration: '60s', target: TARGET_VUS },
    { duration: '5s',  target: 0 },
  ] : [
    { duration: '2s', target: TARGET_VUS },
    { duration: '8s', target: TARGET_VUS },
    { duration: '2s', target: 0 },
  ],
  // 429 is marked expected via http.setResponseCallback below,
  // so http_req_failed counts only non-2xx, non-429 responses
  // (real load-path breakage). The latency thresholds key on
  // expected_response=true so they read against successful
  // requests, not 429 echoes.
  thresholds: {
    // Steady-state: 5% load-path noise floor. Replica-loss:
    // 15% to absorb the kube-proxy reconcile window after a
    // Pod deletion (RST'd in-flight connections).
    http_req_failed: __ENV.REPLICA_LOSS === '1' ? ['rate<=0.15'] : ['rate<=0.05'],
    'http_req_duration{expected_response:true}': ['p(95)<2500', 'p(99)<4500'],
    success_200: [`count>=${SUCCESS_200_MIN}`],
  },
};

const GATEWAY_URL = __ENV.GATEWAY_URL || 'http://127.0.0.1:18080';
const TOKEN = __ENV.TOKEN_A || '';

export function setup() {
  if (!TOKEN) throw new Error('TOKEN_A required');
  // Pre-warm a pool of MCP sessions sequentially before any VU spawns.
  // Per-VU openSession() under 50-VU concurrent load triggers a
  // self-DDoS on the upstream MCP backend's catalogue-init path: every
  // session sees an empty catalogue and tools/list returns nothing for
  // the rest of the run. Staging session opens serially in setup()
  // lets the upstream init complete one connection at a time. Each
  // openSession may still fail under transient conditions (Valkey
  // recovery from T90, scheduler churn); collect best-effort and let
  // VUs share the surviving SIDs. The pool only needs to be non-empty.
  // Benchmark keeps a larger pool; smoke uses fewer sessions so the
  // local check spends its time in the actual load window.
  const sids = [];
  let lastErr;
  for (let i = 0; i < POOL; i++) {
    try { sids.push(openSession()); } catch (e) { lastErr = e; } // best-effort; cause surfaced below if all fail
  }
  if (sids.length === 0) {
    throw new Error(`setup: no SIDs converged for the per-VU pool (${POOL}/${POOL} attempts saw empty catalogue across 30 retries each); last error: ${lastErr}`);
  }
  console.log(`setup: pre-warmed ${sids.length}/${POOL} sessions for the VU pool`);
  return { sids };
}

// Per-VU session id, populated lazily on the first iteration from the
// pre-warmed pool returned by setup(). Each VU has its own JavaScript
// runtime in k6, so this module-level binding is VU-isolated.
let vuSid = null;

function openSession() {
  // agentgateway aggregator opens upstream backend sessions
  // lazily on first client request. The catalogue can come back
  // empty when the upstream MCP init is still in flight, and the
  // empty state persists for the lifetime of that client session.
  // Reopen up to 30 sessions and only return one whose tools/list is
  // non-empty, so the per-VU steady state isn't dominated by VUs
  // that latched onto an empty-catalogue session at first iteration.
  // 30 attempts × 200ms backoff = ~6s budget per session; the
  // per-session upstream-init race on the pinned agentgateway release typically
  // converges within 1-2 attempts on a warm dataplane but can take
  // longer immediately after Valkey/RLS recovery (e.g. T90's outage
  // probe scales the rate-limit store down and back).
  for (let attempt = 0; attempt < 30; attempt++) {
    if (attempt > 0) sleep(0.2);
    const res = http.post(
      `${GATEWAY_URL}/mcp`,
      JSON.stringify({
        jsonrpc: '2.0', id: 1, method: 'initialize',
        params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'k6', version: '1' } },
      }),
      {
        headers: {
          Authorization: `Bearer ${TOKEN}`,
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
        },
      }
    );
    const sid = res.headers['Mcp-Session-Id'] || res.headers['mcp-session-id'] || '';
    if (!sid) continue;
    http.post(
      `${GATEWAY_URL}/mcp`,
      JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized', params: {} }),
      {
        headers: {
          Authorization: `Bearer ${TOKEN}`,
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
          'Mcp-Session-Id': sid,
        },
      }
    );
    const probe = http.post(
      `${GATEWAY_URL}/mcp`,
      JSON.stringify({ jsonrpc: '2.0', id: 99, method: 'tools/list', params: {} }),
      {
        headers: {
          Authorization: `Bearer ${TOKEN}`,
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
          'Mcp-Session-Id': sid,
        },
      }
    );
    const raw = probe.body || '';
    const dataLine = raw.split('\n').find((l) => l.startsWith('data: '));
    const json = dataLine ? dataLine.slice(6) : raw;
    let parsed;
    try { parsed = JSON.parse(json); } catch (_) { parsed = null; }
    const tools = parsed && parsed.result && parsed.result.tools;
    if (Array.isArray(tools) && tools.length > 0) {
      return sid;
    }
  }
  throw new Error('openSession: catalogue stayed empty across 30 fresh sessions with 200ms backoff (cold-start race did not converge)');
}

export default function (data) {
  if (!vuSid) {
    // __VU is 1-based and increments past the pool size if the stage
    // ramps higher; modulo keeps the index in range so excess VUs
    // share a SID rather than crash on undefined.
    vuSid = data.sids[(__VU - 1) % data.sids.length];
  }
  const res = http.post(
    `${GATEWAY_URL}/mcp`,
    JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list', params: {} }),
    {
      headers: {
        Authorization: `Bearer ${TOKEN}`,
        'Content-Type': 'application/json',
        Accept: 'application/json, text/event-stream',
        'Mcp-Session-Id': vuSid,
      },
    }
  );
  // Semantic success: HTTP 200 AND the body carries a JSON-RPC
  // `"result"` field. A bare 200 with a JSON-RPC error envelope
  // (`"error"` + no `"result"`) would otherwise satisfy a status-only
  // gate while the load path is broken at the protocol level.
  if (res.status === 200 && (res.body || '').includes('"result"')) {
    success200.add(1);
  }
  check(res, {
    'status 200': (r) => r.status === 200,
    'has result': (r) => (r.body || '').includes('"result"'),
  });
}
