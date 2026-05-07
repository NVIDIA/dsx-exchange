// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2/clientcredentials"
)

// GetKeycloakToken obtains an OAuth2 access token from Keycloak using the Client Credentials flow.
func GetKeycloakToken(keycloakURL, clientID, clientSecret string) (string, error) {
	// Construct the token endpoint
	tokenURL := fmt.Sprintf("%s/realms/event-bus/protocol/openid-connect/token", keycloakURL)

	// Configure OAuth2 with client credentials
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       []string{"mqtt"},
	}

	// Obtain token using client credentials
	ctx := context.Background()
	token, err := config.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to obtain token: %w", err)
	}

	return token.AccessToken, nil
}
