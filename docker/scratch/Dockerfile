# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

#
# Builder
#
FROM golang:1.22.4 AS builder
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
FROM scratch
LABEL maintainer="Consul Team <consul@hashicorp.com>"

ADD "https://curl.haxx.se/ca/cacert.pem" "/etc/ssl/certs/ca-certificates.crt"
COPY --from=builder "/consul-replicate" "/bin/consul-replicate"
ENTRYPOINT ["/bin/consul-replicate"]
