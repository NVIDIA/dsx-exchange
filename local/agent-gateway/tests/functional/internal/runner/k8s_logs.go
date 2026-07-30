// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package runner — Pod log retrieval helpers for the functional tests.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodLogs returns the log content of one container of one Pod with
// the given options applied.
func PodLogs(t *testing.T, ns, podName string, opts *corev1.PodLogOptions) string {
	t.Helper()
	if opts == nil {
		opts = &corev1.PodLogOptions{}
	}
	req := K8s(t).CoreV1().Pods(ns).GetLogs(podName, opts)
	stream, err := req.Stream(context.Background())
	if err != nil {
		t.Fatalf("logs %s/%s: %v", ns, podName, err)
	}
	defer stream.Close()
	buf, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("read logs %s/%s: %v", ns, podName, err)
	}
	return string(buf)
}

// LogsByLabel concatenates per-Pod logs for every Pod matching the
// label selector. Each line is prefixed with `[pod/<name>/<container>] `
// to match `kubectl logs --all-pods=true`.
func LogsByLabel(t *testing.T, ns, labelSel string, since time.Duration) string {
	t.Helper()
	return logsByLabel(t, ns, labelSel, logsSinceDuration(since), true)
}

// BestEffortLogsByLabel is the opt-in form for absence checks where
// log-stream failures should not themselves prove the event occurred.
func BestEffortLogsByLabel(t *testing.T, ns, labelSel string, since time.Duration) string {
	t.Helper()
	return logsByLabel(t, ns, labelSel, logsSinceDuration(since), false)
}

// LogsByLabelSinceTime concatenates per-Pod logs written after the
// given timestamp for every Pod matching the label selector.
func LogsByLabelSinceTime(t *testing.T, ns, labelSel string, since time.Time) string {
	t.Helper()
	return logsByLabel(t, ns, labelSel, logsSinceTime(since), true)
}

// BestEffortLogsByLabelSinceTime is the opt-in form for absence checks
// that should ignore transient log-stream failures.
func BestEffortLogsByLabelSinceTime(t *testing.T, ns, labelSel string, since time.Time) string {
	t.Helper()
	return logsByLabel(t, ns, labelSel, logsSinceTime(since), false)
}

type logsWindow struct {
	desc  string
	apply func(*corev1.PodLogOptions)
}

func logsSinceDuration(since time.Duration) logsWindow {
	return logsWindow{desc: fmt.Sprintf("since=%s", since), apply: func(opts *corev1.PodLogOptions) {
		if since > 0 {
			secs := int64(since.Seconds())
			opts.SinceSeconds = &secs
		}
	}}
}

func logsSinceTime(since time.Time) logsWindow {
	desc := "sinceTime=<zero>"
	if !since.IsZero() {
		desc = "sinceTime=" + since.UTC().Format(time.RFC3339Nano)
	}
	return logsWindow{desc: desc, apply: func(opts *corev1.PodLogOptions) {
		if !since.IsZero() {
			mt := metav1.NewTime(since)
			opts.SinceTime = &mt
		}
	}}
}

func logsByLabel(t *testing.T, ns, labelSel string, window logsWindow, strict bool) string {
	t.Helper()
	pods := ListPods(t, ns, labelSel, "")
	var out bytes.Buffer
	cs := K8s(t)
	for _, pod := range pods {
		for _, c := range pod.Spec.Containers {
			opts := &corev1.PodLogOptions{Container: c.Name}
			window.apply(opts)
			req := cs.CoreV1().Pods(ns).GetLogs(pod.Name, opts)
			stream, err := req.Stream(context.Background())
			if err != nil {
				if strict {
					t.Fatalf("stream logs ns=%s label=%q pod=%s container=%s %s: %v", ns, labelSel, pod.Name, c.Name, window.desc, err)
				}
				t.Logf("best-effort logs skipped stream ns=%s label=%q pod=%s container=%s %s: %v", ns, labelSel, pod.Name, c.Name, window.desc, err)
				continue
			}
			data, err := io.ReadAll(stream)
			closeErr := stream.Close()
			if err != nil {
				if strict {
					t.Fatalf("read logs ns=%s label=%q pod=%s container=%s %s: %v", ns, labelSel, pod.Name, c.Name, window.desc, err)
				}
				t.Logf("best-effort logs skipped read ns=%s label=%q pod=%s container=%s %s: %v", ns, labelSel, pod.Name, c.Name, window.desc, err)
				continue
			}
			if closeErr != nil {
				if strict {
					t.Fatalf("close logs ns=%s label=%q pod=%s container=%s %s: %v", ns, labelSel, pod.Name, c.Name, window.desc, closeErr)
				}
				t.Logf("best-effort logs skipped close ns=%s label=%q pod=%s container=%s %s: %v", ns, labelSel, pod.Name, c.Name, window.desc, closeErr)
				continue
			}
			prefix := fmt.Sprintf("[pod/%s/%s] ", pod.Name, c.Name)
			for _, line := range bytes.Split(data, []byte("\n")) {
				if len(line) == 0 {
					continue
				}
				out.WriteString(prefix)
				out.Write(line)
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}
