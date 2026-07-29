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

// Package runner — watch-based readiness and termination waits for the
// functional tests. Each wait blocks on Kubernetes watch events rather
// than polling on a timer.
package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

// WaitForDeploymentRolloutsReady waits on Deployment watch events
// until status.observedGeneration reflects the latest metadata.generation
// AND availableReplicas equals spec.replicas.
func WaitForDeploymentRolloutsReady(t *testing.T, ns, labelSel string, timeout time.Duration) {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := DeploymentRolloutsReady(ctx, cs, ns, labelSel); err != nil {
		t.Fatalf("deployments matching %q in %s not ready within %s: %v", labelSel, ns, timeout, err)
	}
}

// DeploymentRolloutsReady waits on Deployment watch events and returns an
// error instead of failing the test. Tests use it when rollout recovery is one
// event among several concurrent outcomes.
func DeploymentRolloutsReady(ctx context.Context, cs kubernetes.Interface, ns, labelSel string) error {
	if cs == nil {
		return fmt.Errorf("nil kubernetes client")
	}
	for {
		deps, err := cs.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("list deploys ns=%s label=%q: %w", ns, labelSel, err)
		}
		if deploymentsReady(deps.Items) {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err := waitForKubernetesEventContext(ctx, metav1.ListOptions{
			LabelSelector:   labelSel,
			ResourceVersion: deps.ResourceVersion,
		}, cs.AppsV1().Deployments(ns).Watch); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("watch deploys ns=%s label=%q: %w", ns, labelSel, err)
		}
	}
}

// WaitForStatefulSetReady waits until StatefulSet status is ready and its
// selected Pods are present, non-terminating, and Ready.
func WaitForStatefulSetReady(t *testing.T, ns, name string, timeout time.Duration) {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fieldSel := fields.OneTermEqualSelector("metadata.name", name).String()
	for {
		sets, err := cs.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{FieldSelector: fieldSel})
		if err != nil {
			t.Fatalf("list sts %s/%s: %v", ns, name, err)
		}
		if len(sets.Items) == 1 && statefulSetReady(&sets.Items[0]) {
			set := &sets.Items[0]
			labelSel := metav1.FormatLabelSelector(set.Spec.Selector)
			pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
			if err != nil {
				t.Fatalf("list pods for sts %s/%s: %v", ns, name, err)
			}
			if statefulSetPodsReady(set, pods.Items) {
				return
			}
			if ctx.Err() != nil {
				t.Fatalf("sts %s/%s pods not ready within %s", ns, name, timeout)
			}
			waitForKubernetesEvent(t, ctx, metav1.ListOptions{
				LabelSelector:   labelSel,
				ResourceVersion: pods.ResourceVersion,
			}, cs.CoreV1().Pods(ns).Watch)
			continue
		}
		if ctx.Err() != nil {
			t.Fatalf("sts %s/%s not ready within %s", ns, name, timeout)
		}
		waitForKubernetesEvent(t, ctx, metav1.ListOptions{
			FieldSelector:   fieldSel,
			ResourceVersion: sets.ResourceVersion,
		}, cs.AppsV1().StatefulSets(ns).Watch)
	}
}

func statefulSetPodsReady(set *appsv1.StatefulSet, pods []corev1.Pod) bool {
	desired := int32(1)
	if set.Spec.Replicas != nil {
		desired = *set.Spec.Replicas
	}
	if int32(len(pods)) != desired {
		return false
	}
	for i := range pods {
		if pods[i].DeletionTimestamp != nil || !podConditionTrue(&pods[i], corev1.PodReady) {
			return false
		}
	}
	return true
}

func podConditionTrue(pod *corev1.Pod, conditionType corev1.PodConditionType) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// WaitForPodsGone waits until no Pod matching labelSel remains in ns.
func WaitForPodsGone(t *testing.T, ns, labelSel string, timeout time.Duration) {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
		if err != nil {
			t.Fatalf("list pods ns=%s label=%q: %v", ns, labelSel, err)
		}
		if len(pods.Items) == 0 {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("pods matching %q in %s did not terminate within %s", labelSel, ns, timeout)
		}
		waitForKubernetesEvent(t, ctx, metav1.ListOptions{
			LabelSelector:   labelSel,
			ResourceVersion: pods.ResourceVersion,
		}, cs.CoreV1().Pods(ns).Watch)
	}
}

// PodDeleted waits on Pod watch events until the named Pod is gone.
func PodDeleted(ctx context.Context, cs kubernetes.Interface, ns, name string) error {
	if cs == nil {
		return fmt.Errorf("nil kubernetes client")
	}
	fieldSel := fields.OneTermEqualSelector("metadata.name", name).String()
	for {
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("get pod %s/%s: %w", ns, name, err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err := waitForKubernetesEventContext(ctx, metav1.ListOptions{
			FieldSelector:   fieldSel,
			ResourceVersion: pod.ResourceVersion,
		}, cs.CoreV1().Pods(ns).Watch); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("watch pod %s/%s: %w", ns, name, err)
		}
	}
}

// WaitForNoPodsTerminating waits until no Pod matching labelSel has
// deletionTimestamp set.
func WaitForNoPodsTerminating(t *testing.T, ns, labelSel string, timeout time.Duration) {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
		if err != nil {
			t.Fatalf("list pods ns=%s label=%q: %v", ns, labelSel, err)
		}
		terminating := 0
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				terminating++
			}
		}
		if terminating == 0 {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("%d pods matching %q in %s were still terminating after %s", terminating, labelSel, ns, timeout)
		}
		waitForKubernetesEvent(t, ctx, metav1.ListOptions{
			LabelSelector:   labelSel,
			ResourceVersion: pods.ResourceVersion,
		}, cs.CoreV1().Pods(ns).Watch)
	}
}

// WaitForService waits on Service watch events until ready returns true.
func WaitForService(t *testing.T, ns, name string, timeout time.Duration, ready func(*corev1.Service) bool) *corev1.Service {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fieldSel := fields.OneTermEqualSelector("metadata.name", name).String()
	for {
		svc, err := cs.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get svc %s/%s: %v", ns, name, err)
		}
		if ready(svc) {
			return svc
		}
		if ctx.Err() != nil {
			t.Fatalf("service %s/%s did not reach expected shape within %s", ns, name, timeout)
		}
		waitForKubernetesEvent(t, ctx, metav1.ListOptions{
			FieldSelector:   fieldSel,
			ResourceVersion: svc.ResourceVersion,
		}, cs.CoreV1().Services(ns).Watch)
	}
}

// WaitForReadyEndpointCount waits until a Service has want ready
// EndpointSlice endpoints. Positive values are lower bounds; zero
// is exact because outage tests need to observe endpoint removal.
func WaitForReadyEndpointCount(t *testing.T, ns, serviceName string, want int, timeout time.Duration) {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	labelSel := "kubernetes.io/service-name=" + serviceName
	for {
		slices, err := cs.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
		if err != nil {
			t.Fatalf("list endpointslices for %s/%s: %v", ns, serviceName, err)
		}
		ready := readyEndpointCount(slices.Items)
		if (want == 0 && ready == 0) || (want > 0 && ready >= want) {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("%s/%s has %d ready endpoint(s), want %d within %s", ns, serviceName, ready, want, timeout)
		}
		waitForKubernetesEvent(t, ctx, metav1.ListOptions{
			LabelSelector:   labelSel,
			ResourceVersion: slices.ResourceVersion,
		}, cs.DiscoveryV1().EndpointSlices(ns).Watch)
	}
}

// ReadyEndpointExcludesPod waits until the Service's ready EndpointSlice
// entries no longer target podName.
func ReadyEndpointExcludesPod(ctx context.Context, cs kubernetes.Interface, ns, serviceName, podName string) error {
	if cs == nil {
		return fmt.Errorf("nil kubernetes client")
	}
	labelSel := "kubernetes.io/service-name=" + serviceName
	for {
		slices, err := cs.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSel})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("list endpointslices for %s/%s: %w", ns, serviceName, err)
		}
		if !readyEndpointTargetsPod(slices.Items, podName) {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err := waitForKubernetesEventContext(ctx, metav1.ListOptions{
			LabelSelector:   labelSel,
			ResourceVersion: slices.ResourceVersion,
		}, cs.DiscoveryV1().EndpointSlices(ns).Watch); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("watch endpointslices for %s/%s: %w", ns, serviceName, err)
		}
	}
}

// WaitForPodTerminal waits until a Pod reaches Succeeded or Failed and
// returns its terminal phase.
func WaitForPodTerminal(t *testing.T, ns, name string, timeout time.Duration) corev1.PodPhase {
	t.Helper()
	cs := K8s(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fieldSel := fields.OneTermEqualSelector("metadata.name", name).String()
	for {
		pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get pod %s/%s: %v", ns, name, err)
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded, corev1.PodFailed:
			return pod.Status.Phase
		}
		if ctx.Err() != nil {
			t.Fatalf("pod %s/%s did not finish within %s; phase=%s", ns, name, timeout, pod.Status.Phase)
		}
		waitForKubernetesEvent(t, ctx, metav1.ListOptions{
			FieldSelector:   fieldSel,
			ResourceVersion: pod.ResourceVersion,
		}, cs.CoreV1().Pods(ns).Watch)
	}
}

func waitForKubernetesEvent(t *testing.T, ctx context.Context, opts metav1.ListOptions, watchFn func(context.Context, metav1.ListOptions) (watch.Interface, error)) {
	t.Helper()
	if err := waitForKubernetesEventContext(ctx, opts, watchFn); err != nil {
		t.Fatalf("wait for Kubernetes event: %v", err)
	}
}

func waitForKubernetesEventContext(ctx context.Context, opts metav1.ListOptions, watchFn func(context.Context, metav1.ListOptions) (watch.Interface, error)) error {
	if opts.ResourceVersion == "" {
		return fmt.Errorf("missing resource version")
	}
	watcher := &cache.ListWatch{WatchFuncWithContext: func(ctx context.Context, watchOpts metav1.ListOptions) (watch.Interface, error) {
		watchOpts.FieldSelector = opts.FieldSelector
		watchOpts.LabelSelector = opts.LabelSelector
		watchOpts.AllowWatchBookmarks = true
		return watchFn(ctx, watchOpts)
	}}
	_, err := watchtools.Until(ctx, opts.ResourceVersion, watcher, func(event watch.Event) (bool, error) {
		if event.Type == watch.Error {
			return false, apierrors.FromObject(event.Object)
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	return nil
}

func deploymentsReady(deps []appsv1.Deployment) bool {
	if len(deps) == 0 {
		return false
	}
	for _, d := range deps {
		if !deploymentReady(&d) {
			return false
		}
	}
	return true
}

func deploymentReady(d *appsv1.Deployment) bool {
	if d.Status.ObservedGeneration < d.Generation {
		return false
	}
	want := int32(0)
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}
	return d.Status.AvailableReplicas == want && d.Status.UpdatedReplicas == want
}

func statefulSetReady(sts *appsv1.StatefulSet) bool {
	want := int32(0)
	if sts.Spec.Replicas != nil {
		want = *sts.Spec.Replicas
	}
	return sts.Status.ReadyReplicas == want
}

func readyEndpointCount(slices []discoveryv1.EndpointSlice) int {
	ready := 0
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				ready++
			}
		}
	}
	return ready
}

func readyEndpointTargetsPod(slices []discoveryv1.EndpointSlice, podName string) bool {
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
				continue
			}
			if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name == podName {
				return true
			}
		}
	}
	return false
}
