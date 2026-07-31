#!/usr/bin/env bash
# Build the benchmark image and side-load it into the local kind nodes.
#
# Why not `kind load docker-image`: the kind binary isn't installed, and this
# cluster is Docker Desktop's kind-based Kubernetes, whose node containers are
# hidden from `docker ps`. They are still reachable by name via `docker exec`,
# which is all `kind load` does under the hood — docker save, then ctr import
# into each node's containerd, k8s.io namespace.
#
# Why not a registry: standing one up and wiring containerd mirror config is
# more moving parts than this, for a throwaway local cluster.
set -euo pipefail

IMAGE="${IMAGE:-poseidon-tests:local}"

# Node container names are derived from the cluster, not hardcoded. kind names
# its node containers exactly after the Kubernetes node objects, so kubectl —
# already a prerequisite — is an authoritative source. Hardcoding worked only
# on the machine this was written on: Docker Desktop names its nodes
# `desktop-control-plane`/`desktop-worker`, while a stock `kind create cluster`
# produces `kind-control-plane`.
#
# Deriving via `docker ps` is NOT an option: Docker Desktop hides its
# Kubernetes node containers from the container list even though `docker exec`
# reaches them, so the list would come back empty here.
NODES="${NODES:-$(kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name} {end}' 2>/dev/null)}"
if [ -z "${NODES// /}" ]; then
  echo "error: could not list cluster nodes — is kubectl pointed at a running cluster?" >&2
  echo "       (override with NODES=\"node-a node-b\" if you know the container names)" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$REPO_ROOT"

# Fail early and legibly if a node object has no matching container — that
# means this is not a kind-style cluster and the side-load approach does not
# apply (a remote cluster needs a registry instead).
for node in $NODES; do
  if ! docker inspect "$node" >/dev/null 2>&1; then
    echo "error: no docker container named '$node'" >&2
    echo "       This script only works with kind-style clusters whose nodes are" >&2
    echo "       local containers. For a remote cluster, push to a registry the" >&2
    echo "       cluster can pull from and drop imagePullPolicy:Never." >&2
    exit 1
  fi
done

echo "==> building $IMAGE"
docker build -t "$IMAGE" .

TAR="$(mktemp -t poseidon-tests-XXXXXX.tar)"
trap 'rm -f "$TAR"' EXIT

echo "==> saving image to $TAR"
docker save "$IMAGE" -o "$TAR"

for node in $NODES; do
  echo "==> importing into $node"
  # --all-platforms avoids a mismatch skip when the saved archive carries
  # more than one platform entry.
  docker exec -i "$node" ctr --namespace=k8s.io images import --all-platforms - < "$TAR"
done

echo "==> verifying"
for node in $NODES; do
  if docker exec "$node" crictl images 2>/dev/null | grep -q "${IMAGE%%:*}"; then
    echo "    $node: OK"
  else
    echo "    $node: MISSING — deployments using imagePullPolicy:Never will fail" >&2
    exit 1
  fi
done

echo "==> done: $IMAGE available on all nodes"
