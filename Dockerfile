ARG ARCH="amd64"
ARG OS="linux"
FROM quay.io/prometheus/busybox-${OS}-${ARCH}:latest
LABEL maintainer="bwplotka"

ARG ARCH="amd64"
ARG OS="linux"
COPY .build/${OS}-${ARCH}/sink /bin/sink

EXPOSE      9101
USER        nobody
ENTRYPOINT  [ "/bin/sink" ]
