# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

ARG BUILDER_IMG=golang:1.26.5-alpine
FROM ${BUILDER_IMG} AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY main.go ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags='-s -w' -o /demoidp ./

FROM scratch
COPY --from=build /demoidp /demoidp
USER 65532:65532
EXPOSE 5556
ENTRYPOINT ["/demoidp"]
