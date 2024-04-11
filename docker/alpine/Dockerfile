# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

#
# Builder
#
FROM golang:1.20 AS builder
LABEL maintainer="Consul Team <consul@hashicorp.com>"

ARG LD_FLAGS
ARG GOTAGS

WORKDIR "/go/src/github.com/hashicorp/consul-replicate"

COPY . .

RUN \
  CGO_ENABLED="0" \
  go build -a -o "/consul-replicate" -ldflags "${LD_FLAGS}" -tags "${GOTAGS}"

#
# Final
#
FROM alpine:latest
LABEL maintainer="Consul Team <consul@hashicorp.com>"


# This is the release of https://github.com/hashicorp/docker-base to pull in
# order to provide HashiCorp-built versions of basic utilities like dumb-init
# and gosu.
ARG DOCKER_BASE_VERSION="0.0.4"

# This is the location of the releases.
ARG HASHICORP_RELEASES="https://releases.hashicorp.com"

# Create a consul-replicate user and group first so the IDs get set the same way, even
# as the rest of this may change over time.
RUN addgroup consul-replicate && \
    adduser -S -G consul-replicate consul-replicate

# Set up certificates and base tools.
RUN apk add --no-cache ca-certificates curl gnupg libcap openssl && \
    mkdir -p /etc/ssl/certs/ && \
    update-ca-certificates --fresh && \
    gpg --keyserver pgp.mit.edu --recv-keys C874011F0AB405110D02105534365D9472D7468F && \
    mkdir -p /tmp/build && \
    cd /tmp/build && \
    curl -sO ${HASHICORP_RELEASES}/docker-base/${DOCKER_BASE_VERSION}/docker-base_${DOCKER_BASE_VERSION}_linux_amd64.zip && \
    curl -sO ${HASHICORP_RELEASES}/docker-base/${DOCKER_BASE_VERSION}/docker-base_${DOCKER_BASE_VERSION}_SHA256SUMS && \
    curl -sO ${HASHICORP_RELEASES}/docker-base/${DOCKER_BASE_VERSION}/docker-base_${DOCKER_BASE_VERSION}_SHA256SUMS.sig && \
    gpg --batch --verify docker-base_${DOCKER_BASE_VERSION}_SHA256SUMS.sig docker-base_${DOCKER_BASE_VERSION}_SHA256SUMS && \
    grep ${DOCKER_BASE_VERSION}_linux_amd64.zip docker-base_${DOCKER_BASE_VERSION}_SHA256SUMS | sha256sum -c && \
    unzip docker-base_${DOCKER_BASE_VERSION}_linux_amd64.zip && \
    cp bin/gosu bin/dumb-init /bin && \
    cd /tmp && \
    rm -rf /tmp/build && \
    apk del gnupg openssl && \
    rm -rf /root/.gnupg

# Copy the compiled binary from the builder
COPY --from=builder "/consul-replicate" "/bin/consul-replicate"

# The agent will be started with /consul-replicate/config as the configuration
# directory so you can add additional config files in that location.
RUN mkdir -p "/consul-replicate/data" && \
    mkdir -p "/consul-replicate/config" && \
    chown -R "consul-replicate:consul-replicate" "/consul-replicate"

# Expose the consul-replicate data directory as a volume since that's where shared
# results should be rendered.
VOLUME /consul-replicate/data

# The entry point script uses dumb-init as the top-level process to reap any
# zombie processes created by our app.
COPY "docker/alpine/docker-entrypoint.sh" "/bin/docker-entrypoint.sh"
RUN chmod +x "/bin/docker-entrypoint.sh"
ENTRYPOINT ["/bin/docker-entrypoint.sh"]

# Run our app by default
CMD ["/bin/consul-replicate"]
