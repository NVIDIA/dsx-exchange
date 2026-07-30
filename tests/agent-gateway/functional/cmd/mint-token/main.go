// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/NVIDIA/dsx-exchange/tests/agent-gateway/functional/internal/runner"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	clientID := flag.String("client", "", "OAuth client ID")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: mint-token [--client <client_id>] <operator|viewer|service-agent|tenant-a|tenant-b|tenant-c|tenant-test|tenant-test-b|bad-svid|wrong-key-svid|unconfigured-issuer>")
		os.Exit(2)
	}

	kubeContext := os.Getenv("KUBE_CONTEXT")
	if kubeContext == "" {
		kubeContext = "kind-dsx-exchange"
	}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	)
	cfg, err := loader.ClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig: %v\n", err)
		os.Exit(1)
	}
	k8s, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clientset: %v\n", err)
		os.Exit(1)
	}
	tok, err := runner.MintTokenWithClient(context.Background(), k8s, flag.Arg(0), *clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mint token: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(tok)
}
