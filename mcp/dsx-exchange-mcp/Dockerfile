# Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
# SPDX-License-Identifier: Apache-2.0

ARG BUILDER_IMG=golang
ARG BUILDER_TAG=1.25.5
ARG FINAL_IMG=gcr.io/distroless/static-debian12
ARG FINAL_TAG=nonroot
ARG SERVICE_PORT=8080
ARG LABEL_CREATED=unknown
ARG LABEL_REVISION=unknown
ARG LABEL_VERSION=dev

FROM ${BUILDER_IMG}:${BUILDER_TAG} AS build
WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -mod=vendor -o /out/dsx-exchange-mcp ./cmd/dsx-exchange-mcp

FROM ${FINAL_IMG}:${FINAL_TAG}

ARG LABEL_VERSION
ARG LABEL_CREATED
ARG LABEL_REVISION

LABEL org.opencontainers.image.created="${LABEL_CREATED}" \
      org.opencontainers.image.description="DSX Exchange MCP Server" \
      org.opencontainers.image.documentation="https://github.com/NVIDIA/dsx-exchange/tree/main/mcp/dsx-exchange-mcp" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.revision="${LABEL_REVISION}" \
      org.opencontainers.image.source="https://github.com/NVIDIA/dsx-exchange/tree/main/mcp/dsx-exchange-mcp" \
      org.opencontainers.image.title="dsx-exchange-mcp" \
      org.opencontainers.image.url="https://github.com/NVIDIA/dsx-exchange/tree/main/mcp/dsx-exchange-mcp" \
      org.opencontainers.image.vendor="NVIDIA" \
      org.opencontainers.image.version="${LABEL_VERSION}"
COPY --from=build /out/dsx-exchange-mcp /dsx-exchange-mcp
EXPOSE ${SERVICE_PORT}
USER nonroot:nonroot
ENTRYPOINT ["/dsx-exchange-mcp"]
