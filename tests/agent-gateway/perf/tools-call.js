/**
 * Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

// k6 perf — tools/call latency through DSX Agent Gateway.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';

const success200 = new Counter('success_200');
const PROFILE = (__ENV.RUN_PERF_PROFILE || (__ENV.RUN_PERF_BENCHMARK === '1' ? 'benchmark' : 'smoke')).toLowerCase();
const BENCHMARK = PROFILE === 'benchmark';
const TARGET_VUS = Number(__ENV.RUN_PERF_TARGET_VUS || (BENCHMARK ? '100' : '20'));
const SUCCESS_200_MIN = Number(__ENV.RUN_PERF_SUCCESS_200_MIN || (BENCHMARK ? '900' : '30'));
const POOL = Number(__ENV.RUN_PERF_SESSION_POOL || (BENCHMARK ? '10' : '4'));

// 429 from the per-tenant rate limit is the expected steady state at
// this VU count, so it does not count as a failure for the load-path
// threshold. http_req_failed catches 5xx / connection / unexpected
// 4xx; latency thresholds run on the successful subset only.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 429));

// See tools-list.js for the rationale on options + thresholds.
export const options = {
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
  thresholds: {
    http_req_failed: __ENV.REPLICA_LOSS === '1' ? ['rate<=0.15'] : ['rate<=0.05'],
    'http_req_duration{expected_response:true}': ['p(95)<2500', 'p(99)<4500'],
    success_200: [`count>=${SUCCESS_200_MIN}`],
  },
};

const GATEWAY_URL = __ENV.GATEWAY_URL || 'http://127.0.0.1:18080';
const TOKEN = __ENV.TOKEN_B || '';
// TOOL env override; otherwise setup() resolves from tools/list.
// Multi-target installs return `<target>_<bare>` names so a
// hardcoded bare name would miss.
const TOOL_OVERRIDE = __ENV.TOOL || '';

// One-time tool resolution (shared across VUs because the catalogue
// is the same). Each VU still creates its own session in default()
// below — only the tool-name lookup is shared. The pinned agentgateway release
// can return an empty catalogue on a fresh session due to the
// upstream-init race; reopen up to 30 times until tools/list returns
// a populated catalogue so setup doesn't fail the whole run on a
// transient empty response.
export function setup() {
  if (!TOKEN) throw new Error('TOKEN_B required');
  if (TOOL_OVERRIDE) {
    const sids = [];
    let lastErr;
    for (let i = 0; i < POOL; i++) {
      try { sids.push(openSession(TOOL_OVERRIDE)); } catch (e) { lastErr = e; } // best-effort; cause surfaced below if all fail
    }
    if (sids.length === 0) {
      throw new Error(`setup: no SIDs converged for the per-VU pool (TOOL_OVERRIDE branch); last error: ${lastErr}`);
    }
    return { tool: TOOL_OVERRIDE, sids };
  }
  let lastNames = [];
  for (let attempt = 0; attempt < 30; attempt++) {
    if (attempt > 0) sleep(0.2);
    const init = http.post(
      `${GATEWAY_URL}/mcp`,
      JSON.stringify({
        jsonrpc: '2.0', id: 1, method: 'initialize',
        params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'k6-setup', version: '1' } },
      }),
      {
        headers: {
          Authorization: `Bearer ${TOKEN}`,
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
        },
      }
    );
    const sid = init.headers['Mcp-Session-Id'] || init.headers['mcp-session-id'] || '';
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
    const listRes = http.post(
      `${GATEWAY_URL}/mcp`,
      JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/list', params: {} }),
      {
        headers: {
          Authorization: `Bearer ${TOKEN}`,
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
          'Mcp-Session-Id': sid,
        },
      }
    );
    const raw = (listRes.body || '');
    const dataLine = raw.split('\n').find((l) => l.startsWith('data: '));
    const json = dataLine ? dataLine.slice(6) : raw;
    let parsed;
    try { parsed = JSON.parse(json); } catch (_) { parsed = null; }
    const names = (parsed && parsed.result && parsed.result.tools || []).map((t) => t.name);
    lastNames = names;
    // Resolve to `echo` (or `<backend>_echo` once agentgateway's
    // multi-target aggregator prefixes names — DELIMITER='_' in
    // mcp/handler.rs). No `|| names[0]` fallback: benchmarking must
    // fail loudly when `echo` is absent rather than silently measure
    // whatever tool happens to come first in the live catalogue.
    const tool = names.find((n) => n === 'echo') ||
                 names.find((n) => n && n.endsWith('_echo'));
    if (tool) {
      // Pre-warm a pool of sessions sequentially so default()
      // doesn't pay cold-start at peak load. See tools-list.js for
      // the same rationale; the upstream MCP backend can't service
      // 50 concurrent catalogue inits, so VUs that latch onto an
      // empty session at first iteration produce "Unknown tool" for
      // the rest of the run. Staging session opens here is the
      // workaround until agentgateway exposes a backend
      // connection-pool warm-up knob.
      // Smoke keeps this small for fast local checks; benchmark keeps
      // the larger pool used by the sustained profile.
      const sids = [];
      let lastErr;
      for (let i = 0; i < POOL; i++) {
        try { sids.push(openSession()); } catch (e) { lastErr = e; } // best-effort; cause surfaced below if all fail
      }
      if (sids.length === 0) {
        throw new Error(`setup: no SIDs converged for the per-VU pool (${POOL}/${POOL} attempts saw empty catalogue across 30 retries each); last error: ${lastErr}`);
      }
      console.log(`setup: pre-warmed ${sids.length}/${POOL} sessions for the VU pool`);
      return { tool, sids };
    }
  }
  throw new Error(`tools-call.js perf requires the 'echo' tool. Catalogue stayed empty / missing 'echo' across 30 fresh sessions. Set TOOL=<name> to override explicitly. Last catalogue seen: ${lastNames.join(',')}`);
}

// Per-VU session (each VU has its own JS runtime in k6, so this is
// effectively VU-isolated). Lazily initialized on first iteration.
let vuSid = null;

function openSession(requiredTool = '') {
  // The pinned agentgateway aggregator opens upstream backend sessions
  // lazily on first client request. The catalogue can come back
  // empty when the upstream MCP init is still in flight, and the
  // empty state persists for the lifetime of that client session —
  // every subsequent tools/call returns "Unknown tool". Reopen up
  // to 30 sessions and only return one whose tools/list is non-empty.
  for (let attempt = 0; attempt < 30; attempt++) {
    if (attempt > 0) sleep(0.2);
    const init = http.post(
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
    const sid = init.headers['Mcp-Session-Id'] || init.headers['mcp-session-id'] || '';
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
    if (Array.isArray(tools) && tools.length > 0 &&
        (!requiredTool || tools.some((tool) => tool.name === requiredTool))) {
      return sid;
    }
  }
  if (requiredTool) {
    throw new Error(`openSession: catalogue did not contain TOOL override ${requiredTool} across 30 fresh sessions`);
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
    JSON.stringify({
      jsonrpc: '2.0', id: 1, method: 'tools/call',
      params: { name: data.tool, arguments: { message: 'perf' } },
    }),
    {
      headers: {
        Authorization: `Bearer ${TOKEN}`,
        'Content-Type': 'application/json',
        Accept: 'application/json, text/event-stream',
        'Mcp-Session-Id': vuSid,
      },
    }
  );
  // Semantic success: HTTP 200 AND a JSON-RPC `"result"` field. A
  // bare 200 with an error envelope is not a successful tool call.
  if (res.status === 200 && (res.body || '').includes('"result"')) {
    success200.add(1);
  }
  check(res, {
    'status 200': (r) => r.status === 200,
    'has result': (r) => (r.body || '').includes('"result"'),
  });
}
