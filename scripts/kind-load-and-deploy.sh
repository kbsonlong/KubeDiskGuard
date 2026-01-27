#!/usr/bin/env bash
set -euo pipefail

NAME=${KDG_KIND_NAME:-kubediskguard}
KUBECONFIG_DIR=${KDG_KUBECONFIG_DIR:-$HOME/.kube}
IMAGE_TAG=${KDG_IMAGE_TAG:-kubediskguard:local}

docker build -t "${IMAGE_TAG}" .

docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "${KUBECONFIG_DIR}":/root/.kube \
  kindest/kind:latest load docker-image "${IMAGE_TAG}" --name "${NAME}"

scripts/kubectl.sh apply -f k8s-daemonset.yaml

scripts/kubectl.sh -n kube-system get pod -l app=kubediskguard
