#!/bin/bash

set -exu

TAG=$(git rev-parse HEAD)
SHORT_TAG=$(git rev-parse --short=7 HEAD)
REPO="quay.io/app-sre/assisted-image-service"
IMAGE="${REPO}:${TAG}"

docker build -f Dockerfile.image-service . -t "${IMAGE}" --no-cache

docker login -u="${QUAY_USER}" -p="${QUAY_TOKEN}" quay.io

docker tag "${IMAGE}" "${REPO}:${SHORT_TAG}"
docker tag "${IMAGE}" "${REPO}:latest"
docker push "${IMAGE}"
docker push "${REPO}:${SHORT_TAG}"
docker push "${REPO}:latest"
