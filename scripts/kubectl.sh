#!/usr/bin/env bash
set -euo pipefail

KUBECONFIG_DIR=${KDG_KUBECONFIG_DIR:-$HOME/.kube}

docker run --rm -i \
  -v "${KUBECONFIG_DIR}":/root/.kube \
  bitnami/kubectl:latest "$@"
