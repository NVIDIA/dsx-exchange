// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
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

	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
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
