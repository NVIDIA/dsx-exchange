#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0


set -e

echo "Deleting all Kind clusters..."

for cluster in csc cpc-1 cpc-2; do
    if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
        echo "Deleting ${cluster}..."
        kind delete cluster --name "$cluster" &
    fi
done

wait

echo "Cleanup complete"
