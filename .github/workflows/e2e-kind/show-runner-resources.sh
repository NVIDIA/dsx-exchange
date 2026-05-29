#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

echo "== runner cpu =="
nproc

echo "== runner memory =="
free -h

echo "== runner limits =="
ulimit -a

echo "== runner inotify limits =="
sysctl fs.inotify.max_user_instances fs.inotify.max_user_watches || true

echo "== runner disk =="
df -h /

echo "== docker info =="
docker info --format 'ServerVersion={{.ServerVersion}} CgroupDriver={{.CgroupDriver}} CgroupVersion={{.CgroupVersion}} OperatingSystem={{.OperatingSystem}}'

echo "== docker system df =="
docker system df

echo "== docker containers =="
docker ps -a --format 'table {{.Names}}\t{{.Status}}\t{{.Image}}'
