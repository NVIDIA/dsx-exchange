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

// Package runner — client-go wrapper for the cluster operations
// the functional tests need. Replaces shelling out to `kubectl`.
// Log retrieval lives in k8s_logs.go; watch-based readiness waits live
// in k8s_wait.go.
package runner

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

var (
	k8sOnce      sync.Once
	k8sCfg       *rest.Config
	k8sClient    *kubernetes.Clientset
	k8sDynClient dynamic.Interface
	k8sErr       error
)

func k8sInit(t *testing.T) {
	t.Helper()
	k8sOnce.Do(func() {
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
			k8sErr = fmt.Errorf("kubeconfig: %w", err)
			return
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			k8sErr = fmt.Errorf("clientset: %w", err)
			return
		}
		dyn, err := dynamic.NewForConfig(cfg)
		if err != nil {
			k8sErr = fmt.Errorf("dynamic client: %w", err)
			return
		}
		k8sCfg, k8sClient, k8sDynClient = cfg, cs, dyn
	})
	if k8sErr != nil {
		t.Fatalf("k8s init: %v", k8sErr)
	}
}

// K8s returns a typed kubernetes client.
func K8s(t *testing.T) *kubernetes.Clientset {
	t.Helper()
	k8sInit(t)
	return k8sClient
}

// Dyn returns a dynamic client for CRDs.
func Dyn(t *testing.T) dynamic.Interface {
	t.Helper()
	k8sInit(t)
	return k8sDynClient
}

// StatefulSetExists returns true when the named StatefulSet is in
// the namespace. Tests use this to skip when an optional dependency
// (Valkey) isn't deployed.
func StatefulSetExists(t *testing.T, ns, name string) bool {
	t.Helper()
	_, err := K8s(t).AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err == nil {
		return true
	}
	if apierrors.IsNotFound(err) {
		return false
	}
	t.Fatalf("get sts %s/%s: %v", ns, name, err)
	return false
}

// StatefulSetReplicas returns the spec.replicas of the named
// StatefulSet, or 0 if unset.
func StatefulSetReplicas(t *testing.T, ns, name string) int32 {
	t.Helper()
	sts, err := K8s(t).AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get sts %s/%s: %v", ns, name, err)
	}
	if sts.Spec.Replicas == nil {
		return 0
	}
	return *sts.Spec.Replicas
}

// ScaleStatefulSet sets spec.replicas on the named StatefulSet.
func ScaleStatefulSet(t *testing.T, ns, name string, replicas int32) {
	t.Helper()
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sts, err := K8s(t).AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		sts.Spec.Replicas = &replicas
		_, err = K8s(t).AppsV1().StatefulSets(ns).Update(context.Background(), sts, metav1.UpdateOptions{})
		return err
	}); err != nil {
		t.Fatalf("scale sts %s/%s to %d: %v", ns, name, replicas, err)
	}
}

// ScaleDeployment sets spec.replicas on the named Deployment.
func ScaleDeployment(t *testing.T, ns, name string, replicas int32) {
	t.Helper()
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deploy, err := K8s(t).AppsV1().Deployments(ns).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		deploy.Spec.Replicas = &replicas
		_, err = K8s(t).AppsV1().Deployments(ns).Update(context.Background(), deploy, metav1.UpdateOptions{})
		return err
	}); err != nil {
		t.Fatalf("scale deploy %s/%s to %d: %v", ns, name, replicas, err)
	}
}

// DeploymentReplicas returns spec.replicas of the named Deployment.
func DeploymentReplicas(t *testing.T, ns, name string) int32 {
	t.Helper()
	d, err := K8s(t).AppsV1().Deployments(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deploy %s/%s: %v", ns, name, err)
	}
	if d.Spec.Replicas == nil {
		return 0
	}
	return *d.Spec.Replicas
}

// ListPods returns Pods matching the label and field selectors.
func ListPods(t *testing.T, ns, labelSel, fieldSel string) []corev1.Pod {
	t.Helper()
	list, err := K8s(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSel,
		FieldSelector: fieldSel,
	})
	if err != nil {
		t.Fatalf("list pods ns=%s label=%q field=%q: %v", ns, labelSel, fieldSel, err)
	}
	return list.Items
}

// DeletePodNow removes a Pod with zero grace. Functional HA tests use
// this to exercise caller-visible behavior during an abrupt replica
// loss, matching a node failure more closely than a graceful drain.
func DeletePodNow(t *testing.T, ns, podName string) {
	t.Helper()
	zero := int64(0)
	if err := K8s(t).CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{
		GracePeriodSeconds: &zero,
	}); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete pod %s/%s: %v", ns, podName, err)
	}
}

// DeletePodGracefully asks kubelet to terminate a Pod using its configured
// grace period.
func DeletePodGracefully(t *testing.T, ns, podName string) {
	t.Helper()
	if err := K8s(t).CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete pod %s/%s: %v", ns, podName, err)
	}
}

// GetUnstructured returns a single CRD object as unstructured.
func GetUnstructured(t *testing.T, gvr schema.GroupVersionResource, ns, name string) *unstructured.Unstructured {
	t.Helper()
	obj, err := Dyn(t).Resource(gvr).Namespace(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get %s %s/%s: %v", gvr.Resource, ns, name, err)
	}
	return obj
}

// AgentgatewayPolicyResource is the GVR for AgentgatewayPolicy.
var AgentgatewayPolicyResource = schema.GroupVersionResource{
	Group: "agentgateway.dev", Version: "v1alpha1", Resource: "agentgatewaypolicies",
}

// AgentgatewayParametersResource is the GVR for AgentgatewayParameters.
var AgentgatewayParametersResource = schema.GroupVersionResource{
	Group: "agentgateway.dev", Version: "v1alpha1", Resource: "agentgatewayparameters",
}

// PatchUnstructured applies a merge patch to the named CR via the
// dynamic client. Tests use this to mutate AgentgatewayParameters
// in-place and assert the controller reconciles.
func PatchUnstructured(t *testing.T, gvr schema.GroupVersionResource, ns, name string, patch []byte) {
	t.Helper()
	if _, err := Dyn(t).Resource(gvr).Namespace(ns).Patch(
		context.Background(), name, types.MergePatchType, patch, metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("patch %s %s/%s: %v", gvr.Resource, ns, name, err)
	}
}

// GetService returns the named Service.
func GetService(t *testing.T, ns, name string) *corev1.Service {
	t.Helper()
	svc, err := K8s(t).CoreV1().Services(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get svc %s/%s: %v", ns, name, err)
	}
	return svc
}

// RolloutRestart sets a restartedAt annotation on every Deployment
// matching the label selector — equivalent to `kubectl rollout
// restart deployment -l <selector>`.
func RolloutRestart(t *testing.T, ns, labelSel string) {
	t.Helper()
	cs := K8s(t)
	deps, err := cs.AppsV1().Deployments(ns).List(context.Background(), metav1.ListOptions{LabelSelector: labelSel})
	if err != nil {
		t.Fatalf("list deploys ns=%s label=%q: %v", ns, labelSel, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Appendf(nil, `{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, now)
	for _, d := range deps.Items {
		if _, err := cs.AppsV1().Deployments(ns).Patch(
			context.Background(), d.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
		); err != nil {
			t.Fatalf("rollout restart deploy %s/%s: %v", ns, d.Name, err)
		}
	}
}
