#!/usr/bin/env bash
set -euo pipefail

NAME=${KDG_KIND_NAME:-kubediskguard}
KUBECONFIG_DIR=${KDG_KUBECONFIG_DIR:-$HOME/.kube}

case "${1:-}" in
  create)
    docker run --rm \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v "${KUBECONFIG_DIR}":/root/.kube \
      kindest/kind:latest create cluster --name "${NAME}"
    ;;
  delete)
    docker run --rm \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v "${KUBECONFIG_DIR}":/root/.kube \
      kindest/kind:latest delete cluster --name "${NAME}"
    ;;
  *)
    echo "Usage: $0 {create|delete}"
    ;;
esac
