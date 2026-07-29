# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

ARG BUILDER_IMG=golang:1.26.5-alpine
FROM ${BUILDER_IMG} AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags='-s -w' -o /dsx-agentgateway-bridge ./cmd/dsx-agentgateway-bridge

FROM scratch
COPY --from=build /dsx-agentgateway-bridge /dsx-agentgateway-bridge
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
EXPOSE 3001 9464
ENTRYPOINT ["/dsx-agentgateway-bridge"]
