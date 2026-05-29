#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

configure_inotify_limits() {
  local sysctl_cmd=(sysctl -w fs.inotify.max_user_instances=8192)

  echo "Configuring inotify limits for multi-cluster Kind..."
  if command -v sudo >/dev/null 2>&1; then
    sudo "${sysctl_cmd[@]}"
  else
    "${sysctl_cmd[@]}"
  fi
}

configure_inotify_limits
