// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"

	natsjwt "github.com/nats-io/jwt/v2"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
)

// Authenticator interface for all authentication methods
type Authenticator interface {
	// CanAuthenticate checks if this authenticator can handle the given request
	CanAuthenticate(rc *natsjwt.AuthorizationRequestClaims) bool
	// TryAuthenticate attempts to authenticate the request
	TryAuthenticate(ctx context.Context, rc *natsjwt.AuthorizationRequestClaims) (config.UserProfile, error)
}
