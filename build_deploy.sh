#!/bin/bash

set -exu

TAG=$(git rev-parse --short=7 HEAD)
REPO="quay.io/app-sre/assisted-image-service"
export IMAGE="${REPO}:${TAG}"

make build

podman login -u="${QUAY_USER}" -p="${QUAY_TOKEN}" quay.io

podman tag "${IMAGE}" "${REPO}:latest"
podman push "${IMAGE}"
podman push "${REPO}:latest"
