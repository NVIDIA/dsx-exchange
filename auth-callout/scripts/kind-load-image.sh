#!/bin/sh
# Load a Docker image into a kind cluster.
# Usage: kind-load-image.sh <image> [cluster-name]

set -e

IMG=$1
CLUSTER=${2:-auth-callout}

if [ -z "$IMG" ]; then
  echo "Usage: $0 <image> [cluster-name]" >&2
  exit 1
fi

NAME=${IMG%%:*}
TAG=${IMG##*:}

# Pull if not present locally
if ! docker image inspect "$IMG" >/dev/null 2>&1; then
  echo "Pulling $IMG..."
  docker pull "$IMG"
fi

# Get all nodes in the cluster
NODES=$(kind get nodes --name "$CLUSTER")

# Load into each node if not already present
for NODE in $NODES; do
  if ! docker exec "$NODE" crictl images | grep -q "$NAME.*$TAG"; then
    echo "Loading $IMG into node $NODE..."
    # Use direct ctr import to avoid kind's --all-platforms bug
    # See: https://github.com/kubernetes-sigs/kind/issues/3795
    docker save "$IMG" | docker exec -i "$NODE" ctr --namespace=k8s.io images import -
    # Verify import succeeded
    if ! docker exec "$NODE" crictl images | grep -q "$NAME.*$TAG"; then
      echo "ERROR: Failed to load $IMG into node $NODE" >&2
      exit 1
    fi
  else
    echo "$IMG already present on node $NODE"
  fi
done
