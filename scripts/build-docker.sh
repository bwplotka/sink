#!/bin/zsh

set -xe

REPO_ROOT=$(realpath $(dirname "${BASH_SOURCE[0]}")/..)

ARCH="amd64"
OS="linux"
BUILD_DIR=${REPO_ROOT}"/.build/${OS}-${ARCH}/"

echo "Building ${WHAT} into ${DOCKER_IMAGE}:${DOCKER_TAG} tag"

mkdir -p "${BUILD_DIR}"
pushd ./go
  CGO_ENABLED=0 GOOS=${OS} GOARCH=${ARCH} go build -o ${BUILD_DIR}sink ./sink
popd
DOCKER_BUILDKIT=1 docker build --tag ${DOCKER_IMAGE}:${DOCKER_TAG} .


if [ "${1}" = "yes" ] ; then
  # Prerequisite: gcloud auth configure-docker or docker login quay.io etc
  docker push ${DOCKER_IMAGE}:${DOCKER_TAG}

fi


