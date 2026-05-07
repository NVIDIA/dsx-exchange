################################################################################
### Build arguments
################################################################################

# Builder image
ARG BUILDER_IMG=golang
ARG BUILDER_TAG=1.25.5

# Final image
ARG FINAL_IMG=gcr.io/distroless/static-debian12
ARG FINAL_TAG=nonroot

# Port configuration (informational - actual port determined by config.yaml)
ARG SERVICE_PORT=8000

# Image metadata (can be overridden at build time)
ARG LABEL_CREATED=unknown
ARG LABEL_REVISION=unknown
ARG LABEL_VERSION=dev

################################################################################
### Stage: Builder
################################################################################

# Use the official golang image as the base image
FROM ${BUILDER_IMG}:${BUILDER_TAG} AS builder

# Set working directory
WORKDIR /app

# Copy source code (includes vendor/ directory)
COPY . .

# Build the binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -mod=vendor -a -installsuffix cgo -o auth-callout ./src/cmd/auth_callout

################################################################################
### Stage: Final
################################################################################

FROM ${FINAL_IMG}:${FINAL_TAG}

# Import metadata build args into final stage
ARG LABEL_VERSION
ARG LABEL_CREATED
ARG LABEL_REVISION

# OCI image labels for container registry integration and traceability
LABEL org.opencontainers.image.created="${LABEL_CREATED}" \
      org.opencontainers.image.description="Auth Callout" \
      org.opencontainers.image.documentation="https://github.com/NVIDIA/dsx-exchange/tree/main/auth-callout" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.revision="${LABEL_REVISION}" \
      org.opencontainers.image.source="https://github.com/NVIDIA/dsx-exchange/tree/main/auth-callout" \
      org.opencontainers.image.title="auth-callout" \
      org.opencontainers.image.url="https://github.com/NVIDIA/dsx-exchange/tree/main/auth-callout" \
      org.opencontainers.image.vendor="NVIDIA" \
      org.opencontainers.image.version="${LABEL_VERSION}"

WORKDIR /app
COPY --from=builder --chown=65532:65532 /app/auth-callout /app/auth-callout
USER 65532:65532

# Expose port
EXPOSE ${SERVICE_PORT}

# Run the binary
ENTRYPOINT ["./auth-callout"]
