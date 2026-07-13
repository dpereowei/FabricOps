/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

func TestWaitForFabricNetworkReadyReturnsOnReadyCondition(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := waitForFabricNetworkReady(
		context.Background(),
		kubeOptions{namespace: "default"},
		"sample",
		time.Second,
		time.Millisecond,
		&stdout,
		&stderr,
		func(context.Context, kubeOptions, string) (*fabricopsv1alpha1.FabricNetwork, error) {
			return fabricNetworkWithReadyStatus(metav1.ConditionTrue, "Ready", ""), nil
		},
	)
	if err != nil {
		t.Fatalf("waitForFabricNetworkReady() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "FabricNetwork default/sample is Ready" {
		t.Fatalf("stdout = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestWaitForFabricNetworkReadyPrintsDiagnosticsOnTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := waitForFabricNetworkReady(
		context.Background(),
		kubeOptions{namespace: "default"},
		"sample",
		time.Nanosecond,
		time.Millisecond,
		&stdout,
		&stderr,
		func(context.Context, kubeOptions, string) (*fabricopsv1alpha1.FabricNetwork, error) {
			return fabricNetworkWithReadyStatus(metav1.ConditionFalse, "ChannelsPending", "Waiting for peer join Jobs"), nil
		},
	)
	if err == nil {
		t.Fatal("waitForFabricNetworkReady() error = nil, want timeout")
	}
	if !strings.Contains(err.Error(), "timed out waiting for FabricNetwork default/sample to be Ready") {
		t.Fatalf("error = %v", err)
	}
	for _, want := range []string{
		"FabricNetwork: default/sample",
		"Ready: False (ChannelsPending)",
		"Message: Waiting for peer join Jobs",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr does not contain %q\nstderr:\n%s", want, stderr.String())
		}
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestWaitForFabricNetworkReadyPrintsLastErrorOnTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := waitForFabricNetworkReady(
		context.Background(),
		kubeOptions{namespace: "default"},
		"missing",
		time.Nanosecond,
		time.Millisecond,
		&stdout,
		&stderr,
		func(context.Context, kubeOptions, string) (*fabricopsv1alpha1.FabricNetwork, error) {
			return nil, errors.New("not found")
		},
	)
	if err == nil {
		t.Fatal("waitForFabricNetworkReady() error = nil, want timeout")
	}
	if !strings.Contains(stderr.String(), "Last error: not found") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunWaitRejectsUnsupportedWaitTarget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"wait", "--for", "delete", "sample"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(wait --for delete) error = nil, want rejection")
	}
	if !strings.Contains(err.Error(), `unsupported wait target "delete"`) {
		t.Fatalf("error = %v", err)
	}
}

func fabricNetworkWithReadyStatus(
	conditionStatus metav1.ConditionStatus,
	reason string,
	message string,
) *fabricopsv1alpha1.FabricNetwork {
	phase := fabricopsv1alpha1.PhasePending
	if conditionStatus == metav1.ConditionTrue {
		phase = fabricopsv1alpha1.PhaseReady
	}
	return &fabricopsv1alpha1.FabricNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: "default"},
		Status: fabricopsv1alpha1.FabricNetworkStatus{
			Phase:   phase,
			Message: message,
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  conditionStatus,
					Reason:  reason,
					Message: message,
				},
			},
		},
	}
}
