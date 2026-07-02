//go:build e2e

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

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	managerNamespace = "fabricops-system"
	managerName      = "fabricops-controller-manager"
	sampleName       = "fabricnetwork-sample"
	sampleNamespace  = "default"
)

var (
	repoRoot     string
	kindBin      string
	kubectlBin   string
	kindCluster  string
	managerImage string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FabricOps E2E Suite")
}

var _ = BeforeSuite(func() {
	repoRoot = mustRepoRoot()
	kindBin = envOrDefault("KIND", "kind")
	kubectlBin = envOrDefault("KUBECTL", "kubectl")
	kindCluster = envOrDefault("KIND_CLUSTER", "fabricops-test-e2e")
	managerImage = envOrDefault("IMG", "controller:latest")
})

var _ = Describe("Kind bundle install", Ordered, func() {
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpDiagnostics()
		}
	})

	It("reconciles the sample network and invokes the Node CCaaS chaincode", func() {
		By("using the target kind context")
		runCommand(30*time.Second, kubectlBin, "config", "use-context", "kind-"+kindCluster)

		By("building and loading the manager image")
		runCommand(10*time.Minute, "make", "docker-build", "IMG="+managerImage)
		runCommand(5*time.Minute, kindBin, "load", "docker-image", managerImage, "--name", kindCluster)

		By("generating and applying the install bundle")
		runCommand(5*time.Minute, "make", "build-installer", "IMG="+managerImage)
		runCommand(3*time.Minute, kubectlBin, "apply", "-f", "dist/install.yaml")
		runCommand(3*time.Minute, kubectlBin, "rollout", "status", "deployment/"+managerName, "-n", managerNamespace, "--timeout=120s")

		By("applying the sample FabricNetwork")
		runCommand(2*time.Minute, kubectlBin, "apply", "-k", "config/samples")

		By("waiting for FabricOps to provision the Fabric network")
		runCommand(25*time.Minute, kubectlBin, "wait", "fabricnetwork/"+sampleName, "-n", sampleNamespace, "--for=condition=Ready", "--timeout=20m")

		By("invoking and querying the sample Node CCaaS chaincode through both BankA peers")
		smokeID := fmt.Sprintf("e2e-%d", time.Now().Unix())
		runCommandWithEnv(3*time.Minute, []string{"SMOKE_ID=" + smokeID}, "config/samples/chaincodes/node_settlement/invoke_smoke.sh")

		status := runCommand(30*time.Second, kubectlBin, "get", "fabricnetwork", sampleName, "-n", sampleNamespace, "-o", "jsonpath={.status.phase}{\"\\n\"}{range .status.conditions[*]}{.type}={.status} {.reason}{\"\\n\"}{end}")
		Expect(status).To(ContainSubstring("Ready\n"))
		Expect(status).To(ContainSubstring("Ready=True FabricNetworkReady"))
	})
})

func runCommand(timeout time.Duration, name string, args ...string) string {
	return runCommandWithEnv(timeout, nil, name, args...)
}

func runCommandWithEnv(timeout time.Duration, extraEnv []string, name string, args ...string) string {
	GinkgoHelper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), extraEnv...)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	commandLine := strings.Join(append([]string{name}, args...), " ")
	fmt.Fprintf(GinkgoWriter, "\n$ %s\n", commandLine)

	err := cmd.Run()
	text := output.String()
	if text != "" {
		fmt.Fprintln(GinkgoWriter, text)
	}

	if ctx.Err() == context.DeadlineExceeded {
		Fail(fmt.Sprintf("command timed out after %s: %s\n%s", timeout, commandLine, text))
	}

	Expect(err).NotTo(HaveOccurred(), "command failed: %s\n%s", commandLine, text)
	return text
}

func runDiagnostic(timeout time.Duration, name string, args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	commandLine := strings.Join(append([]string{name}, args...), " ")
	fmt.Fprintf(GinkgoWriter, "\n# diagnostics: %s\n", commandLine)
	_ = cmd.Run()
	if output.Len() > 0 {
		fmt.Fprintln(GinkgoWriter, output.String())
	}
}

func dumpDiagnostics() {
	runDiagnostic(30*time.Second, kubectlBin, "get", "fabricnetwork", "-A", "-o", "wide")
	runDiagnostic(30*time.Second, kubectlBin, "get", "pods", "-A", "-o", "wide")
	runDiagnostic(30*time.Second, kubectlBin, "get", "jobs", "-A")
	runDiagnostic(30*time.Second, kubectlBin, "describe", "fabricnetwork", sampleName, "-n", sampleNamespace)
	runDiagnostic(30*time.Second, kubectlBin, "logs", "-n", managerNamespace, "deployment/"+managerName, "-c", "manager", "--tail=200")
	runDiagnostic(30*time.Second, kubectlBin, "get", "events", "-A", "--sort-by=.lastTimestamp")
}

func mustRepoRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		Fail("could not discover test filename")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
