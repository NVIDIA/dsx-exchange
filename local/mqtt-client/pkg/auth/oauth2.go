// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// GetOIDCToken obtains an OAuth2 access token using the client credentials flow.
func GetOIDCToken(idpURL, clientID, clientSecret string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return GetOIDCTokenContext(ctx, idpURL, clientID, clientSecret)
}

// GetOIDCTokenContext obtains an OAuth2 access token using the supplied context.
func GetOIDCTokenContext(ctx context.Context, idpURL, clientID, clientSecret string) (string, error) {
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     idpURL + "/token",
		Scopes:       []string{"mqtt"},
		AuthStyle:    oauth2.AuthStyleInParams,
	}

	token, err := config.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to obtain token: %w", err)
	}

	return token.AccessToken, nil
}
