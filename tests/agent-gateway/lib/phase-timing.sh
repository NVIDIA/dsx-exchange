#!/usr/bin/env bash
# Copyright 2026 NVIDIA Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

path_mtime_epoch() {
  local mtime
  mtime="$(stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null || printf '0')"
  if [[ "$mtime" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$mtime"
  else
    printf '0\n'
  fi
}

phase_timing_init() {
  PHASE_TIMING_SCRIPT="${1:?script name required}"
  local reset="${2:-append}"
  PHASE_TIMINGS_FILE="${PHASE_TIMINGS_FILE:-tests/agent-gateway/artifacts/phase-timings.tsv}"
  PHASE_TIMINGS_LOG="${PHASE_TIMINGS_LOG:-tests/agent-gateway/artifacts/phase-timings.log}"
  PHASE_TIMINGS_RUN_ID_FILE="${PHASE_TIMINGS_RUN_ID_FILE:-${PHASE_TIMINGS_FILE%.tsv}.run-id}"
  mkdir -p "$(dirname "$PHASE_TIMINGS_FILE")" "$(dirname "$PHASE_TIMINGS_LOG")" "$(dirname "$PHASE_TIMINGS_RUN_ID_FILE")"
  local write_run_id=0
  if [[ "$reset" == "reset" || -z "${PHASE_TIMINGS_RUN_ID:-}" ]]; then
    if [[ "$reset" == "append" && -s "$PHASE_TIMINGS_RUN_ID_FILE" ]]; then
      PHASE_TIMINGS_RUN_ID="$(<"$PHASE_TIMINGS_RUN_ID_FILE")"
    else
      PHASE_TIMINGS_RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
      write_run_id=1
    fi
    [[ "$reset" == "reset" ]] && write_run_id=1
    if (( write_run_id == 1 )); then
      printf '%s\n' "$PHASE_TIMINGS_RUN_ID" > "$PHASE_TIMINGS_RUN_ID_FILE"
    fi
    export PHASE_TIMINGS_RUN_ID
  fi
  if [[ "$reset" == "reset" ]]; then
    : > "$PHASE_TIMINGS_LOG"
    phase_timing_write_header
  elif [[ ! -s "$PHASE_TIMINGS_FILE" ]] || ! head -n 1 "$PHASE_TIMINGS_FILE" | grep -q $'^run_id\tscript\tphase\tstart_utc\tend_utc\tduration_seconds\tstatus$'; then
    phase_timing_write_header
  fi
  PHASE_TIMING_ACTIVE_PHASE=""
  PHASE_TIMING_ACTIVE_START_EPOCH=""
  PHASE_TIMING_ACTIVE_START_UTC=""
}

phase_timing_write_header() {
  printf 'run_id\tscript\tphase\tstart_utc\tend_utc\tduration_seconds\tstatus\n' > "$PHASE_TIMINGS_FILE"
}

phase_timing_start() {
  phase_timing_finish
  local phase="${1:?phase name required}"
  # Phase labels are report data. Keep callers on literal labels so
  # timing artifacts never capture env vars, command lines, or secrets.
  if [[ "$phase" == *'|'* || "$phase" == *'`'* || "$phase" == *$'\t'* || "$phase" == *$'\n'* ]]; then
    echo "phase timing: phase label contains a markdown or TSV delimiter" >&2
    return 1
  fi
  PHASE_TIMING_ACTIVE_PHASE="$phase"
  PHASE_TIMING_ACTIVE_START_EPOCH="$(date +%s)"
  PHASE_TIMING_ACTIVE_START_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'START: %s %s %s run=%s\n' \
    "$PHASE_TIMING_SCRIPT" "$PHASE_TIMING_ACTIVE_PHASE" "$PHASE_TIMING_ACTIVE_START_UTC" "$PHASE_TIMINGS_RUN_ID" \
    | tee -a "$PHASE_TIMINGS_LOG"
}

phase_timing_finish() {
  if [[ -z "${PHASE_TIMING_ACTIVE_PHASE:-}" ]]; then
    return 0
  fi
  local status="${1:-OK}"
  local end_epoch end_utc duration_s
  end_epoch="$(date +%s)"
  end_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  duration_s=$((end_epoch - PHASE_TIMING_ACTIVE_START_EPOCH))
  printf 'END: %s %s %ss run=%s\n' \
    "$PHASE_TIMING_SCRIPT" "$PHASE_TIMING_ACTIVE_PHASE" "$duration_s" "$PHASE_TIMINGS_RUN_ID" \
    | tee -a "$PHASE_TIMINGS_LOG"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$PHASE_TIMINGS_RUN_ID" "$PHASE_TIMING_SCRIPT" "$PHASE_TIMING_ACTIVE_PHASE" \
    "$PHASE_TIMING_ACTIVE_START_UTC" "$end_utc" "$duration_s" "$status" \
    >> "$PHASE_TIMINGS_FILE"
  PHASE_TIMING_ACTIVE_PHASE=""
  PHASE_TIMING_ACTIVE_START_EPOCH=""
  PHASE_TIMING_ACTIVE_START_UTC=""
}

phase_timing_prune_script_rows() {
  local script="${1:?script name required}"
  local tmp
  tmp="$(mktemp "${PHASE_TIMINGS_FILE}.tmp.XXXXXX")"
  awk -F '\t' -v OFS='\t' -v run_id="$PHASE_TIMINGS_RUN_ID" -v script="$script" \
    'NR == 1 || !($1 == run_id && $2 == script)' \
    "$PHASE_TIMINGS_FILE" > "$tmp"
  mv "$tmp" "$PHASE_TIMINGS_FILE"
}

phase_timing_prune_other_runs() {
  local tmp
  tmp="$(mktemp "${PHASE_TIMINGS_FILE}.tmp.XXXXXX")"
  awk -F '\t' -v OFS='\t' -v run_id="$PHASE_TIMINGS_RUN_ID" \
    'NR == 1 || $1 == run_id' \
    "$PHASE_TIMINGS_FILE" > "$tmp"
  mv "$tmp" "$PHASE_TIMINGS_FILE"
}

phase_start() {
  phase_timing_start "$1"
}

phase_end() {
  local status="OK"
  local expected_phase=""
  if (( $# >= 2 )); then
    expected_phase="$1"
    status="$2"
  elif [[ "${1:-}" == "OK" || "${1:-}" == "FAIL" ]]; then
    status="$1"
  else
    expected_phase="${1:-}"
  fi
  if [[ -n "$expected_phase" && "$expected_phase" != "${PHASE_TIMING_ACTIVE_PHASE:-}" ]]; then
    echo "phase timing: expected active phase '$expected_phase', got '${PHASE_TIMING_ACTIVE_PHASE:-none}'" >&2
    phase_timing_finish FAIL || true
    return 1
  fi
  phase_timing_finish "$status"
}

phase_err_trap() {
  local rc=$?
  phase_timing_finish FAIL || true
  exit "$rc"
}
