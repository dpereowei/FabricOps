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
	"encoding/json"
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
	managerNamespace                  = "fabricops-system"
	managerName                       = "fabricops-controller-manager"
	sampleName                        = "fabricnetwork-sample"
	sampleNamespace                   = "default"
	nodeSettlementImageDefault        = "ghcr.io/dpereowei/fabricops-node-settlement:0.1.0"
	nodeSettlementUpgradeImageDefault = "ghcr.io/dpereowei/fabricops-node-settlement:0.2.0"
)

var (
	repoRoot         string
	kindBin          string
	kubectlBin       string
	kindCluster      string
	managerImage     string
	nodeImage        string
	nodeUpgradeImage string
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
	nodeImage = envOrDefault("NODE_SETTLEMENT_IMAGE", nodeSettlementImageDefault)
	nodeUpgradeImage = envOrDefault("NODE_SETTLEMENT_UPGRADE_IMAGE", nodeSettlementUpgradeImageDefault)
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

		By("building and loading the local Node settlement chaincode images")
		runCommand(10*time.Minute, "docker", "build", "-t", nodeImage, "-t", nodeUpgradeImage, "config/samples/chaincodes/node_settlement")
		runCommand(5*time.Minute, kindBin, "load", "docker-image", nodeImage, "--name", kindCluster)
		runCommand(5*time.Minute, kindBin, "load", "docker-image", nodeUpgradeImage, "--name", kindCluster)

		By("generating and applying the install bundle")
		runCommand(5*time.Minute, "make", "build-installer", "IMG="+managerImage)
		runCommand(3*time.Minute, kubectlBin, "apply", "-f", "dist/install.yaml")
		runCommand(3*time.Minute, kubectlBin, "rollout", "status", "deployment/"+managerName, "-n", managerNamespace, "--timeout=120s")

		By("applying the sample FabricNetwork")
		runCommand(2*time.Minute, kubectlBin, "apply", "-k", "config/samples")

		By("waiting for FabricOps to provision the Fabric network")
		runCommand(25*time.Minute, kubectlBin, "wait", "fabricnetwork/"+sampleName, "-n", sampleNamespace, "--for=condition=Ready", "--timeout=20m")

		By("invoking and querying the sample Node CCaaS chaincode through BankA and BankB endorsement sets")
		smokeID := fmt.Sprintf("e2e-%d", time.Now().Unix())
		runCommandWithEnv(5*time.Minute, []string{
			"SMOKE_ID=" + smokeID,
			"PRIVATE_SMOKE_ENABLED=true",
		}, "config/samples/chaincodes/node_settlement/invoke_smoke.sh")

		status := runCommand(30*time.Second, kubectlBin, "get", "fabricnetwork", sampleName, "-n", sampleNamespace, "-o", "jsonpath={.status.phase}{\"\\n\"}{range .status.conditions[*]}{.type}={.status} {.reason}{\"\\n\"}{end}")
		Expect(status).To(ContainSubstring("Ready\n"))
		Expect(status).To(ContainSubstring("Ready=True FabricNetworkReady"))

		By("upgrading the sample chaincode declaratively")
		patch := fmt.Sprintf(
			`[{"op":"replace","path":"/spec/chaincodes/0/version","value":"0.0.2"},{"op":"replace","path":"/spec/chaincodes/0/sequence","value":2},{"op":"replace","path":"/spec/chaincodes/0/image","value":%q}]`,
			nodeUpgradeImage,
		)
		runCommand(2*time.Minute, kubectlBin, "patch", "fabricnetwork", sampleName, "-n", sampleNamespace, "--type=json", "-p", patch)
		upgradedGeneration := getFabricNetworkProbe().Metadata.Generation

		By("waiting for FabricOps to complete the chaincode upgrade")
		waitForFabricNetworkReadyGeneration(upgradedGeneration, 25*time.Minute)

		By("verifying the upgraded chaincode status and workload image")
		upgraded := getFabricNetworkProbe()
		Expect(upgraded.Status.ChaincodeStatus).To(HaveLen(1))
		Expect(upgraded.Status.ChaincodeStatus[0].Version).To(Equal("0.0.2"))
		Expect(upgraded.Status.ChaincodeStatus[0].Sequence).To(Equal(int32(2)))
		Expect(upgraded.Status.ChaincodeStatus[0].PackageLabel).To(Equal("settlement_settlement_0.0.2"))
		Expect(upgraded.Status.ChaincodeStatus[0].Ready).To(BeTrue())
		for _, item := range []struct {
			namespace string
			name      string
		}{
			{namespace: "fo-sample-banka", name: "settlement-settlement-banka-peer0-ccaas"},
			{namespace: "fo-sample-banka", name: "settlement-settlement-banka-peer1-ccaas"},
			{namespace: "fo-sample-bankb", name: "settlement-settlement-bankb-peer0-ccaas"},
		} {
			image := strings.TrimSpace(runCommand(
				30*time.Second,
				kubectlBin,
				"get",
				"deployment",
				item.name,
				"-n",
				item.namespace,
				"-o",
				"jsonpath={.spec.template.spec.containers[?(@.name==\"chaincode\")].image}",
			))
			Expect(image).To(Equal(nodeUpgradeImage))
		}

		By("invoking and querying the upgraded chaincode")
		upgradeSmokeID := smokeID + "-upgrade"
		runCommandWithEnv(5*time.Minute, []string{
			"SMOKE_ID=" + upgradeSmokeID,
		}, "config/samples/chaincodes/node_settlement/invoke_smoke.sh")
	})
})

type fabricNetworkProbe struct {
	Metadata struct {
		Generation int64 `json:"generation"`
	} `json:"metadata"`
	Status struct {
		Phase      string `json:"phase"`
		Conditions []struct {
			Type               string `json:"type"`
			Status             string `json:"status"`
			Reason             string `json:"reason"`
			ObservedGeneration int64  `json:"observedGeneration"`
		} `json:"conditions"`
		ChaincodeStatus []struct {
			Name         string `json:"name"`
			Version      string `json:"version"`
			PackageLabel string `json:"packageLabel"`
			Sequence     int32  `json:"sequence"`
			Ready        bool   `json:"ready"`
		} `json:"chaincodeStatus"`
	} `json:"status"`
}

func getFabricNetworkProbe() fabricNetworkProbe {
	GinkgoHelper()

	output := runCommand(30*time.Second, kubectlBin, "get", "fabricnetwork", sampleName, "-n", sampleNamespace, "-o", "json")
	var probe fabricNetworkProbe
	Expect(json.Unmarshal([]byte(output), &probe)).To(Succeed())
	return probe
}

func waitForFabricNetworkReadyGeneration(generation int64, timeout time.Duration) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		probe := getFabricNetworkProbe()
		g.Expect(probe.Status.Phase).To(Equal("Ready"))
		for _, condition := range probe.Status.Conditions {
			if condition.Type == "Ready" {
				g.Expect(condition.Status).To(Equal("True"))
				g.Expect(condition.Reason).To(Equal("FabricNetworkReady"))
				g.Expect(condition.ObservedGeneration).To(Equal(generation))
				return
			}
		}
		g.Expect(probe.Status.Conditions).To(ContainElement(HaveField("Type", "Ready")))
	}, timeout, 10*time.Second).Should(Succeed())
}

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
