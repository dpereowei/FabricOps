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

package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

var _ = Describe("FabricNetwork Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "fabricnetwork-test"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			deleteFabricNetworkIfExists(ctx, typeNamespacedName)
			cleanupOrgNamespaceResources(ctx, "fo-test-orderer")
			cleanupOrgNamespaceResources(ctx, "fo-test-banka")

			By("creating the custom resource for the Kind FabricNetwork")
			resource := &fabricopsv1alpha1.FabricNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: fabricopsv1alpha1.FabricNetworkSpec{
					Global: fabricopsv1alpha1.GlobalConfig{
						FabricVersion: "3.1.0",
						TLS:           true,
					},
					Orgs: []fabricopsv1alpha1.Org{
						{
							Organization: fabricopsv1alpha1.OrgMeta{
								Name:    "Orderer",
								Domain:  "orderer.example.com",
								MSPName: "OrdererMSP",
							},
							CA: fabricopsv1alpha1.CAConfig{DB: "sqlite"},
							Orderers: []fabricopsv1alpha1.OrdererGroup{
								{
									GroupName: "group1",
									Type:      "raft",
									Instances: 1,
									Prefix:    componentOrderer,
								},
							},
						},
						{
							Organization: fabricopsv1alpha1.OrgMeta{
								Name:    "BankA",
								Domain:  "banka.example.com",
								MSPName: "BankAMSP",
							},
							CA: fabricopsv1alpha1.CAConfig{DB: "sqlite"},
							Peer: &fabricopsv1alpha1.PeerConfig{
								Instances: 1,
								DB:        "CouchDB",
								Prefix:    componentPeer,
							},
						},
					},
					Channels:   []fabricopsv1alpha1.Channel{},
					Chaincodes: []fabricopsv1alpha1.Chaincode{},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance FabricNetwork")
			deleteFabricNetworkIfExists(ctx, typeNamespacedName)

			for _, namespaceName := range []string{
				"fo-test-orderer",
				"fo-test-banka",
			} {
				cleanupOrgNamespaceResources(ctx, namespaceName)
			}
		})

		It("should create org infrastructure resources in per-org namespaces", func() {
			By("Reconciling the created resource")
			controllerReconciler := &FabricNetworkReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ordererNamespace := "fo-test-orderer"
			bankNamespace := "fo-test-banka"

			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ordererNamespace}, &ns)).To(Succeed())
			Expect(ns.Labels[labelFabricNetwork]).To(Equal(resourceName))
			Expect(ns.Labels[labelFabricNetworkNamespace]).To(Equal(resourceNamespace))
			Expect(ns.Labels[labelOrg]).To(Equal("orderer"))

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bankNamespace}, &ns)).To(Succeed())
			Expect(ns.Labels[labelFabricNetwork]).To(Equal(resourceName))
			Expect(ns.Labels[labelFabricNetworkNamespace]).To(Equal(resourceNamespace))
			Expect(ns.Labels[labelOrg]).To(Equal("banka"))

			expectDeploymentNotFound(ctx, ordererNamespace, "orderer0")
			expectServiceNotFound(ctx, ordererNamespace, "orderer0")

			var caDeploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "banka-ca",
			}, &caDeploy)).To(Succeed())
			caContainer := caDeploy.Spec.Template.Spec.Containers[0]
			caEnv := envMap(caContainer)
			Expect(caEnv["FABRIC_CA_HOME"]).To(Equal("/etc/hyperledger/fabric-ca-server"))
			Expect(caEnv["FABRIC_CA_SERVER_CA_NAME"]).To(Equal("banka"))
			Expect(caEnv["FABRIC_CA_SERVER_PORT"]).To(Equal("7054"))
			Expect(envSecretRefs(caContainer)).To(HaveKeyWithValue(caBootstrapEnvVar, "banka-ca-bootstrap/user-pass"))
			Expect(caContainer.Command).To(Equal([]string{
				"sh", "-c",
				"fabric-ca-server start -b \"$FABRIC_CA_SERVER_BOOTSTRAP_USER_PASS\" -d",
			}))

			var caSvc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "banka-ca",
			}, &caSvc)).To(Succeed())

			expectDeploymentNotFound(ctx, bankNamespace, "peer0")
			expectServiceNotFound(ctx, bankNamespace, "peer0")

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			Expect(network.Status.Phase).To(Equal(fabricopsv1alpha1.PhaseCreating))
			Expect(network.Status.Message).To(Equal("Waiting for required Fabric identity material"))
			Expect(network.Status.OrgStatus).To(HaveLen(2))
			Expect(network.Status.OrgStatus[0].Name).To(Equal("Orderer"))
			Expect(network.Status.OrgStatus[0].Namespace).To(Equal(ordererNamespace))
			Expect(network.Status.OrgStatus[0].IdentityReady).To(BeFalse())
			Expect(network.Status.OrgStatus[0].IdentityError).To(ContainSubstring("orderer-admin-msp"))
			Expect(network.Status.OrgStatus[0].IdentityError).To(ContainSubstring("orderer0-msp"))
			Expect(network.Status.OrgStatus[0].CAReady).To(BeFalse())
			Expect(network.Status.OrgStatus[0].Orderers.Desired).To(Equal(int32(1)))
			Expect(network.Status.OrgStatus[0].Orderers.Ready).To(Equal(int32(0)))
			Expect(network.Status.OrgStatus[0].OrderersReady).To(BeFalse())
			Expect(network.Status.OrgStatus[0].Peers.Desired).To(Equal(int32(0)))
			Expect(network.Status.OrgStatus[0].Peers.Ready).To(Equal(int32(0)))
			Expect(network.Status.OrgStatus[0].PeersReady).To(BeTrue())
			Expect(network.Status.OrgStatus[0].Ready).To(BeFalse())
			Expect(network.Status.OrgStatus[1].Name).To(Equal("BankA"))
			Expect(network.Status.OrgStatus[1].Namespace).To(Equal(bankNamespace))
			Expect(network.Status.OrgStatus[1].IdentityReady).To(BeFalse())
			Expect(network.Status.OrgStatus[1].IdentityError).To(ContainSubstring("banka-admin-msp"))
			Expect(network.Status.OrgStatus[1].IdentityError).To(ContainSubstring("peer0-msp"))
			Expect(network.Status.OrgStatus[1].CAReady).To(BeFalse())
			Expect(network.Status.OrgStatus[1].Orderers.Desired).To(Equal(int32(0)))
			Expect(network.Status.OrgStatus[1].Orderers.Ready).To(Equal(int32(0)))
			Expect(network.Status.OrgStatus[1].OrderersReady).To(BeTrue())
			Expect(network.Status.OrgStatus[1].Peers.Desired).To(Equal(int32(1)))
			Expect(network.Status.OrgStatus[1].Peers.Ready).To(Equal(int32(0)))
			Expect(network.Status.OrgStatus[1].PeersReady).To(BeFalse())
			Expect(network.Status.OrgStatus[1].Ready).To(BeFalse())

			ready := apiMeta.FindStatusCondition(network.Status.Conditions, conditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("IdentityMaterialMissing"))

			identity := apiMeta.FindStatusCondition(network.Status.Conditions, conditionIdentityMaterialReady)
			Expect(identity).NotTo(BeNil())
			Expect(identity.Status).To(Equal(metav1.ConditionFalse))
			Expect(identity.Reason).To(Equal("IdentityMaterialMissing"))

			expectIdentitySecret(ctx, ordererNamespace, caBootstrapSecretName(network.Spec.Orgs[0]), secretKindCABootstrap, true)
			expectIdentitySecret(ctx, ordererNamespace, adminEnrollmentSecretName(network.Spec.Orgs[0]), secretKindAdminEnroll, true)
			expectIdentitySecret(ctx, ordererNamespace, "orderer0-enrollment", secretKindWorkloadEnroll, true)
			expectSecretNotFound(ctx, ordererNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[0]), secretKindMSP))
			expectSecretNotFound(ctx, ordererNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[0]), secretKindTLS))
			expectSecretNotFound(ctx, ordererNamespace, "orderer0-msp")
			expectSecretNotFound(ctx, ordererNamespace, "orderer0-tls")
			expectIdentitySecret(ctx, bankNamespace, caBootstrapSecretName(network.Spec.Orgs[1]), secretKindCABootstrap, true)
			expectIdentitySecret(ctx, bankNamespace, adminEnrollmentSecretName(network.Spec.Orgs[1]), secretKindAdminEnroll, true)
			expectIdentitySecret(ctx, bankNamespace, "peer0-enrollment", secretKindWorkloadEnroll, true)
			expectSecretNotFound(ctx, bankNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[1]), secretKindMSP))
			expectSecretNotFound(ctx, bankNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[1]), secretKindTLS))
			expectSecretNotFound(ctx, bankNamespace, "peer0-msp")
			expectSecretNotFound(ctx, bankNamespace, "peer0-tls")
		})

		It("should not generate fallback identity secrets after enrollment failure", func() {
			By("Reconciling the created resource")
			controllerReconciler := &FabricNetworkReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ordererNamespace := "fo-test-orderer"
			bankNamespace := "fo-test-banka"
			markDeploymentReady(ctx, ordererNamespace, "orderer-ca")
			markDeploymentReady(ctx, bankNamespace, "banka-ca")

			By("Reconciling after CAs report readiness")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())

			By("Marking enrollment jobs as failed")
			markJobFailed(ctx, ordererNamespace, adminEnrollmentJobName(network.Spec.Orgs[0]))
			markJobFailed(ctx, ordererNamespace, workloadEnrollmentJobName("orderer0"))
			markJobFailed(ctx, bankNamespace, adminEnrollmentJobName(network.Spec.Orgs[1]))
			markJobFailed(ctx, bankNamespace, workloadEnrollmentJobName("peer0"))

			By("Reconciling after enrollment failures")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			expectSecretNotFound(ctx, ordererNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[0]), secretKindMSP))
			expectSecretNotFound(ctx, ordererNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[0]), secretKindTLS))
			expectSecretNotFound(ctx, ordererNamespace, "orderer0-msp")
			expectSecretNotFound(ctx, ordererNamespace, "orderer0-tls")
			expectSecretNotFound(ctx, bankNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[1]), secretKindMSP))
			expectSecretNotFound(ctx, bankNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[1]), secretKindTLS))
			expectSecretNotFound(ctx, bankNamespace, "peer0-msp")
			expectSecretNotFound(ctx, bankNamespace, "peer0-tls")
			expectDeploymentNotFound(ctx, ordererNamespace, "orderer0")
			expectDeploymentNotFound(ctx, bankNamespace, "peer0")

			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			Expect(network.Status.OrgStatus[0].IdentityReady).To(BeFalse())
			Expect(network.Status.OrgStatus[0].IdentityError).To(ContainSubstring("orderer-admin-msp"))
			Expect(network.Status.OrgStatus[0].IdentityError).To(ContainSubstring("orderer0-msp"))
			Expect(network.Status.OrgStatus[1].IdentityReady).To(BeFalse())
			Expect(network.Status.OrgStatus[1].IdentityError).To(ContainSubstring("banka-admin-msp"))
			Expect(network.Status.OrgStatus[1].IdentityError).To(ContainSubstring("peer0-msp"))
		})

		It("should create admin enrollment resources after org CAs are ready", func() {
			By("Reconciling the created resource")
			controllerReconciler := &FabricNetworkReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ordererNamespace := "fo-test-orderer"
			bankNamespace := "fo-test-banka"
			markDeploymentReady(ctx, ordererNamespace, "orderer-ca")
			markDeploymentReady(ctx, bankNamespace, "banka-ca")

			By("Reconciling after CAs report readiness")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			bankOrg := network.Spec.Orgs[1]

			var serviceAccount corev1.ServiceAccount
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      enrollmentServiceAccountName(bankOrg),
			}, &serviceAccount)).To(Succeed())

			var role rbacv1.Role
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      enrollmentServiceAccountName(bankOrg),
			}, &role)).To(Succeed())
			Expect(role.Rules).To(ContainElement(rbacv1.PolicyRule{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "create", "update", "patch"},
			}))

			var roleBinding rbacv1.RoleBinding
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      enrollmentServiceAccountName(bankOrg),
			}, &roleBinding)).To(Succeed())
			Expect(roleBinding.RoleRef.Name).To(Equal(enrollmentServiceAccountName(bankOrg)))
			Expect(roleBinding.Subjects).To(ContainElement(rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      enrollmentServiceAccountName(bankOrg),
				Namespace: bankNamespace,
			}))

			var job batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      adminEnrollmentJobName(bankOrg),
			}, &job)).To(Succeed())
			Expect(job.Spec.Template.Spec.ServiceAccountName).To(Equal(enrollmentServiceAccountName(bankOrg)))
			Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))

			enrollContainer := job.Spec.Template.Spec.InitContainers[0]
			Expect(enrollContainer.Name).To(Equal(enrollAdminContainerName))
			Expect(enrollContainer.Image).To(Equal(caImage()))
			Expect(envMap(enrollContainer)[envCAAddress]).To(Equal("banka-ca.fo-test-banka.svc.cluster.local:7054"))
			Expect(envMap(enrollContainer)[envAdminName]).To(Equal("banka-admin"))
			Expect(envSecretRefs(enrollContainer)).To(HaveKeyWithValue(envCABootstrapUserPass, "banka-ca-bootstrap/user-pass"))
			Expect(envSecretRefs(enrollContainer)).To(HaveKeyWithValue(envAdminUsername, "banka-admin-enrollment/username"))
			Expect(envSecretRefs(enrollContainer)).To(HaveKeyWithValue(envAdminPassword, "banka-admin-enrollment/password"))
			Expect(enrollContainer.Command[2]).To(ContainSubstring("fabric-ca-client register"))
			Expect(enrollContainer.Command[2]).To(ContainSubstring("fabric-ca-client enroll"))

			publishContainer := job.Spec.Template.Spec.Containers[0]
			Expect(publishContainer.Name).To(Equal(publishAdminContainerName))
			Expect(publishContainer.Image).To(Equal(kubectlImage()))
			Expect(envMap(publishContainer)[envAdminMSPSecret]).To(Equal("banka-admin-msp"))
			Expect(envMap(publishContainer)[envAdminTLSSecret]).To(Equal("banka-admin-tls"))
			Expect(envMap(publishContainer)[envTLSEnabled]).To(Equal("true"))
			Expect(publishContainer.Command[2]).To(ContainSubstring("kubectl -n \"$POD_NAMESPACE\" create secret generic"))
			Expect(publishContainer.Command[2]).To(ContainSubstring(labelIdentitySource + "=" + identitySourceFabricCA))

			var ordererJob batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ordererNamespace,
				Name:      workloadEnrollmentJobName("orderer0"),
			}, &ordererJob)).To(Succeed())
			ordererEnrollContainer := ordererJob.Spec.Template.Spec.InitContainers[0]
			Expect(ordererEnrollContainer.Name).To(Equal(enrollWorkloadContainerName))
			Expect(envMap(ordererEnrollContainer)[envWorkloadName]).To(Equal("orderer0"))
			Expect(envMap(ordererEnrollContainer)[envWorkloadType]).To(Equal(componentOrderer))
			Expect(envSecretRefs(ordererEnrollContainer)).To(HaveKeyWithValue(envWorkloadUsername, "orderer0-enrollment/username"))
			Expect(envSecretRefs(ordererEnrollContainer)).To(HaveKeyWithValue(envWorkloadPassword, "orderer0-enrollment/password"))

			var peerJob batchv1.Job
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      workloadEnrollmentJobName("peer0"),
			}, &peerJob)).To(Succeed())
			Expect(peerJob.Spec.Template.Spec.ServiceAccountName).To(Equal(enrollmentServiceAccountName(bankOrg)))
			Expect(peerJob.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
			Expect(peerJob.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(peerJob.Spec.Template.Spec.Containers).To(HaveLen(1))

			workloadEnrollContainer := peerJob.Spec.Template.Spec.InitContainers[0]
			Expect(workloadEnrollContainer.Name).To(Equal(enrollWorkloadContainerName))
			Expect(workloadEnrollContainer.Image).To(Equal(caImage()))
			Expect(envMap(workloadEnrollContainer)[envCAAddress]).To(Equal("banka-ca.fo-test-banka.svc.cluster.local:7054"))
			Expect(envMap(workloadEnrollContainer)[envWorkloadName]).To(Equal("peer0"))
			Expect(envMap(workloadEnrollContainer)[envWorkloadType]).To(Equal(componentPeer))
			Expect(envMap(workloadEnrollContainer)[envWorkloadCSRHosts]).To(ContainSubstring("peer0.fo-test-banka.svc.cluster.local"))
			Expect(envMap(workloadEnrollContainer)[envTLSEnabled]).To(Equal("true"))
			Expect(envSecretRefs(workloadEnrollContainer)).To(HaveKeyWithValue(envCABootstrapUserPass, "banka-ca-bootstrap/user-pass"))
			Expect(envSecretRefs(workloadEnrollContainer)).To(HaveKeyWithValue(envWorkloadUsername, "peer0-enrollment/username"))
			Expect(envSecretRefs(workloadEnrollContainer)).To(HaveKeyWithValue(envWorkloadPassword, "peer0-enrollment/password"))
			Expect(workloadEnrollContainer.Command[2]).To(ContainSubstring("fabric-ca-client register"))
			Expect(workloadEnrollContainer.Command[2]).To(ContainSubstring("--id.type \"$FABRICOPS_WORKLOAD_TYPE\""))
			Expect(workloadEnrollContainer.Command[2]).To(ContainSubstring("fabric-ca-client enroll"))

			workloadPublishContainer := peerJob.Spec.Template.Spec.Containers[0]
			Expect(workloadPublishContainer.Name).To(Equal(publishWorkloadContainerName))
			Expect(workloadPublishContainer.Image).To(Equal(kubectlImage()))
			Expect(envMap(workloadPublishContainer)[envWorkloadMSPSecret]).To(Equal("peer0-msp"))
			Expect(envMap(workloadPublishContainer)[envWorkloadTLSSecret]).To(Equal("peer0-tls"))
			Expect(envMap(workloadPublishContainer)[envTLSEnabled]).To(Equal("true"))
			Expect(workloadPublishContainer.Command[2]).To(ContainSubstring("kubectl -n \"$POD_NAMESPACE\" create secret generic"))
			Expect(workloadPublishContainer.Command[2]).To(ContainSubstring(labelIdentitySource + "=" + identitySourceFabricCA))
		})

		It("should mark the network ready only after all org workloads are ready", func() {
			By("Reconciling the created resource")
			controllerReconciler := &FabricNetworkReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ordererNamespace := "fo-test-orderer"
			bankNamespace := "fo-test-banka"

			markDeploymentReady(ctx, ordererNamespace, "orderer-ca")
			markDeploymentReady(ctx, bankNamespace, "banka-ca")

			By("Reconciling after CAs report readiness")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			writeEnrolledOrgIdentitySecrets(ctx, &network, network.Spec.Orgs[0], ordererNamespace)
			writeEnrolledOrgIdentitySecrets(ctx, &network, network.Spec.Orgs[1], bankNamespace)

			By("Reconciling after enrolled identities are published")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var ordererDeploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ordererNamespace,
				Name:      "orderer0",
			}, &ordererDeploy)).To(Succeed())
			ordererContainer := ordererDeploy.Spec.Template.Spec.Containers[0]
			ordererEnv := envMap(ordererContainer)
			Expect(ordererEnv["ORDERER_GENERAL_LISTENADDRESS"]).To(Equal("0.0.0.0"))
			Expect(ordererEnv["ORDERER_GENERAL_LISTENPORT"]).To(Equal("7050"))
			Expect(ordererEnv).NotTo(HaveKey("ORDERER_GENERAL_CLUSTER_LISTENADDRESS"))
			Expect(ordererEnv).NotTo(HaveKey("ORDERER_GENERAL_CLUSTER_LISTENPORT"))
			Expect(ordererEnv["ORDERER_GENERAL_LOCALMSPID"]).To(Equal("OrdererMSP"))
			Expect(ordererEnv["ORDERER_GENERAL_LOCALMSPDIR"]).To(Equal(ordererMSPPath))
			Expect(ordererEnv["ORDERER_GENERAL_TLS_ENABLED"]).To(Equal("true"))
			Expect(ordererEnv["ORDERER_GENERAL_TLS_PRIVATEKEY"]).To(Equal(ordererTLSPath + "/server.key"))
			Expect(ordererEnv["ORDERER_GENERAL_TLS_CERTIFICATE"]).To(Equal(ordererTLSPath + "/server.crt"))
			Expect(ordererEnv["ORDERER_GENERAL_TLS_ROOTCAS"]).To(Equal("[" + ordererTLSPath + "/ca.crt]"))
			Expect(ordererEnv["ORDERER_GENERAL_CLUSTER_CLIENTCERTIFICATE"]).To(Equal(ordererTLSPath + "/server.crt"))
			Expect(ordererEnv["ORDERER_GENERAL_CLUSTER_CLIENTPRIVATEKEY"]).To(Equal(ordererTLSPath + "/server.key"))
			Expect(ordererEnv["ORDERER_GENERAL_CLUSTER_ROOTCAS"]).To(Equal("[" + ordererTLSPath + "/ca.crt]"))
			Expect(containerPorts(ordererContainer)).To(ContainElements(int32(7050)))
			Expect(secretVolumeNames(ordererDeploy.Spec.Template.Spec)).To(HaveKeyWithValue(secretKindMSP, "orderer0-msp"))
			Expect(secretVolumeNames(ordererDeploy.Spec.Template.Spec)).To(HaveKeyWithValue(secretKindTLS, "orderer0-tls"))
			Expect(secretVolumeItemKeys(ordererDeploy.Spec.Template.Spec, secretKindMSP)).To(ContainElements(mspConfigKey, mspCACertKey, mspTLSCACertKey, mspSignCertKey, mspKeyStoreKey))
			Expect(secretVolumeItemKeys(ordererDeploy.Spec.Template.Spec, secretKindTLS)).To(ContainElements(tlsCACertKey, tlsServerCertKey, tlsServerKeyKey))
			Expect(volumeMountPaths(ordererContainer)).To(HaveKeyWithValue(secretKindMSP, ordererMSPPath))
			Expect(volumeMountPaths(ordererContainer)).To(HaveKeyWithValue(secretKindTLS, ordererTLSPath))

			var ordererSvc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ordererNamespace,
				Name:      "orderer0",
			}, &ordererSvc)).To(Succeed())
			Expect(servicePorts(ordererSvc)).To(ContainElements(int32(7050)))

			var peerDeploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "peer0",
			}, &peerDeploy)).To(Succeed())
			peerContainer := peerDeploy.Spec.Template.Spec.Containers[0]
			peerEnv := envMap(peerContainer)
			Expect(peerEnv["CORE_PEER_ID"]).To(Equal("peer0"))
			Expect(peerEnv["CORE_PEER_ADDRESS"]).To(Equal("peer0.fo-test-banka.svc.cluster.local:7051"))
			Expect(peerEnv["CORE_PEER_LISTENADDRESS"]).To(Equal("0.0.0.0:7051"))
			Expect(peerEnv["CORE_PEER_CHAINCODEADDRESS"]).To(Equal("peer0.fo-test-banka.svc.cluster.local:7052"))
			Expect(peerEnv["CORE_PEER_CHAINCODELISTENADDRESS"]).To(Equal("0.0.0.0:7052"))
			Expect(peerEnv["CORE_PEER_GOSSIP_ENDPOINT"]).To(Equal("peer0.fo-test-banka.svc.cluster.local:7051"))
			Expect(peerEnv["CORE_PEER_GOSSIP_EXTERNALENDPOINT"]).To(Equal("peer0.fo-test-banka.svc.cluster.local:7051"))
			Expect(peerEnv["CORE_PEER_LOCALMSPID"]).To(Equal("BankAMSP"))
			Expect(peerEnv["CORE_PEER_MSPCONFIGPATH"]).To(Equal(peerMSPPath))
			Expect(peerEnv["CORE_PEER_TLS_ENABLED"]).To(Equal("true"))
			Expect(peerEnv["CORE_PEER_TLS_CERT_FILE"]).To(Equal(peerTLSPath + "/server.crt"))
			Expect(peerEnv["CORE_PEER_TLS_KEY_FILE"]).To(Equal(peerTLSPath + "/server.key"))
			Expect(peerEnv["CORE_PEER_TLS_ROOTCERT_FILE"]).To(Equal(peerTLSPath + "/ca.crt"))
			Expect(containerPorts(peerContainer)).To(ContainElements(int32(7051), int32(7052)))
			Expect(secretVolumeNames(peerDeploy.Spec.Template.Spec)).To(HaveKeyWithValue(secretKindMSP, "peer0-msp"))
			Expect(secretVolumeNames(peerDeploy.Spec.Template.Spec)).To(HaveKeyWithValue(secretKindTLS, "peer0-tls"))
			Expect(secretVolumeItemKeys(peerDeploy.Spec.Template.Spec, secretKindMSP)).To(ContainElements(mspConfigKey, mspCACertKey, mspTLSCACertKey, mspSignCertKey, mspKeyStoreKey))
			Expect(secretVolumeItemKeys(peerDeploy.Spec.Template.Spec, secretKindTLS)).To(ContainElements(tlsCACertKey, tlsServerCertKey, tlsServerKeyKey))
			Expect(volumeMountPaths(peerContainer)).To(HaveKeyWithValue(secretKindMSP, peerMSPPath))
			Expect(volumeMountPaths(peerContainer)).To(HaveKeyWithValue(secretKindTLS, peerTLSPath))

			var peerSvc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "peer0",
			}, &peerSvc)).To(Succeed())
			Expect(servicePorts(peerSvc)).To(ContainElements(int32(7051), int32(7052)))

			markDeploymentReady(ctx, ordererNamespace, "orderer0")
			markDeploymentReady(ctx, bankNamespace, "peer0")

			By("Reconciling after workloads report readiness")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			Expect(network.Status.Phase).To(Equal(fabricopsv1alpha1.PhaseReady))
			Expect(network.Status.Message).To(Equal("All Fabric components are ready"))
			Expect(network.Status.OrgStatus).To(HaveLen(2))
			Expect(network.Status.OrgStatus[0].Ready).To(BeTrue())
			Expect(network.Status.OrgStatus[0].IdentityReady).To(BeTrue())
			Expect(network.Status.OrgStatus[0].CAReady).To(BeTrue())
			Expect(network.Status.OrgStatus[0].Orderers.Ready).To(Equal(int32(1)))
			Expect(network.Status.OrgStatus[0].OrderersReady).To(BeTrue())
			Expect(network.Status.OrgStatus[1].Ready).To(BeTrue())
			Expect(network.Status.OrgStatus[1].IdentityReady).To(BeTrue())
			Expect(network.Status.OrgStatus[1].CAReady).To(BeTrue())
			Expect(network.Status.OrgStatus[1].Peers.Ready).To(Equal(int32(1)))
			Expect(network.Status.OrgStatus[1].PeersReady).To(BeTrue())

			ready := apiMeta.FindStatusCondition(network.Status.Conditions, conditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(ready.Reason).To(Equal("ComponentsReady"))

			identity := apiMeta.FindStatusCondition(network.Status.Conditions, conditionIdentityMaterialReady)
			Expect(identity).NotTo(BeNil())
			Expect(identity.Status).To(Equal(metav1.ConditionTrue))
			Expect(identity.Reason).To(Equal("IdentityMaterialPresent"))
		})
	})
})

func deleteFabricNetworkIfExists(ctx context.Context, name types.NamespacedName) {
	resource := &fabricopsv1alpha1.FabricNetwork{}
	err := k8sClient.Get(ctx, name, resource)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
}

func cleanupOrgNamespaceResources(ctx context.Context, namespace string) {
	deleteAllJobs(ctx, namespace)
	deleteAllDeployments(ctx, namespace)
	deleteAllServices(ctx, namespace)
	deleteAllSecrets(ctx, namespace)
	deleteAllRoleBindings(ctx, namespace)
	deleteAllRoles(ctx, namespace)
	deleteAllServiceAccounts(ctx, namespace)
}

func deleteAllJobs(ctx context.Context, namespace string) {
	var jobs batchv1.JobList
	err := k8sClient.List(ctx, &jobs, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range jobs.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &jobs.Items[i]))).To(Succeed())
	}
}

func deleteAllDeployments(ctx context.Context, namespace string) {
	var deployments appsv1.DeploymentList
	err := k8sClient.List(ctx, &deployments, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range deployments.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &deployments.Items[i]))).To(Succeed())
	}
}

func deleteAllServices(ctx context.Context, namespace string) {
	var services corev1.ServiceList
	err := k8sClient.List(ctx, &services, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range services.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &services.Items[i]))).To(Succeed())
	}
}

func deleteAllSecrets(ctx context.Context, namespace string) {
	var secrets corev1.SecretList
	err := k8sClient.List(ctx, &secrets, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range secrets.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &secrets.Items[i]))).To(Succeed())
	}
}

func deleteAllServiceAccounts(ctx context.Context, namespace string) {
	var serviceAccounts corev1.ServiceAccountList
	err := k8sClient.List(ctx, &serviceAccounts, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range serviceAccounts.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &serviceAccounts.Items[i]))).To(Succeed())
	}
}

func deleteAllRoles(ctx context.Context, namespace string) {
	var roles rbacv1.RoleList
	err := k8sClient.List(ctx, &roles, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range roles.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &roles.Items[i]))).To(Succeed())
	}
}

func deleteAllRoleBindings(ctx context.Context, namespace string) {
	var roleBindings rbacv1.RoleBindingList
	err := k8sClient.List(ctx, &roleBindings, client.InNamespace(namespace))
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	for i := range roleBindings.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &roleBindings.Items[i]))).To(Succeed())
	}
}

func markDeploymentReady(ctx context.Context, namespace, name string) {
	var deploy appsv1.Deployment
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &deploy)).To(Succeed())
	Expect(deploy.Spec.Replicas).NotTo(BeNil())

	deploy.Status.Replicas = *deploy.Spec.Replicas
	deploy.Status.ReadyReplicas = *deploy.Spec.Replicas
	deploy.Status.AvailableReplicas = *deploy.Spec.Replicas
	deploy.Status.UpdatedReplicas = *deploy.Spec.Replicas
	Expect(k8sClient.Status().Update(ctx, &deploy)).To(Succeed())
}

func markJobFailed(ctx context.Context, namespace, name string) {
	var job batchv1.Job
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &job)).To(Succeed())

	now := metav1.Now()
	job.Status.Failed = 1
	job.Status.StartTime = &now
	job.Status.Conditions = append(job.Status.Conditions,
		batchv1.JobCondition{
			Type:               batchv1.JobFailureTarget,
			Status:             corev1.ConditionTrue,
			Reason:             "BackoffLimitExceeded",
			LastTransitionTime: now,
		},
		batchv1.JobCondition{
			Type:               batchv1.JobFailed,
			Status:             corev1.ConditionTrue,
			Reason:             "BackoffLimitExceeded",
			LastTransitionTime: now,
		},
	)
	Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
}

func writeEnrolledOrgIdentitySecrets(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) {
	authority, err := generateTestIdentityAuthority(org)
	Expect(err).NotTo(HaveOccurred())

	adminName := adminIdentityName(org)
	adminMSP, err := buildTestMSPSecret(net, org, namespace, adminName, componentAdmin, secretKindAdminMSP, authority)
	Expect(err).NotTo(HaveOccurred())
	adminMSP.Labels[labelIdentitySource] = identitySourceFabricCA
	upsertSecret(ctx, adminMSP)

	if net.Spec.Global.TLS {
		adminTLS, err := buildTestAdminTLSSecret(net, org, namespace, adminName, authority)
		Expect(err).NotTo(HaveOccurred())
		adminTLS.Labels[labelIdentitySource] = identitySourceFabricCA
		upsertSecret(ctx, adminTLS)
	}

	for _, group := range org.Orderers {
		for i := 0; i < group.Instances; i++ {
			name := sanitizeName(fmt.Sprintf("%s%d", group.Prefix, i))
			writeEnrolledWorkloadIdentitySecrets(ctx, net, org, namespace, name, componentOrderer, authority)
		}
	}

	if org.Peer == nil {
		return
	}

	for i := 0; i < org.Peer.Instances; i++ {
		name := sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, i))
		writeEnrolledWorkloadIdentitySecrets(ctx, net, org, namespace, name, componentPeer, authority)
	}
}

func writeEnrolledWorkloadIdentitySecrets(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	workloadName string,
	component string,
	authority *testIdentityAuthority,
) {
	msp, err := buildTestMSPSecret(net, org, namespace, workloadName, component, secretKindMSP, authority)
	Expect(err).NotTo(HaveOccurred())
	msp.Labels[labelIdentitySource] = identitySourceFabricCA
	upsertSecret(ctx, msp)

	if !net.Spec.Global.TLS {
		return
	}

	tls, err := buildTestWorkloadTLSSecret(net, org, namespace, workloadName, component, workloadDNSNames(workloadName, namespace), authority)
	Expect(err).NotTo(HaveOccurred())
	tls.Labels[labelIdentitySource] = identitySourceFabricCA
	upsertSecret(ctx, tls)
}

type testIdentityAuthority struct {
	mspCACertPEM []byte
	mspCAKey     *ecdsa.PrivateKey
	tlsCACertPEM []byte
	tlsCAKey     *ecdsa.PrivateKey
}

func generateTestIdentityAuthority(org fabricopsv1alpha1.Org) (*testIdentityAuthority, error) {
	mspCert, mspKey, err := generateTestCA("ca."+org.Organization.Domain, org.Organization.MSPName)
	if err != nil {
		return nil, err
	}

	tlsCert, tlsKey, err := generateTestCA("tlsca."+org.Organization.Domain, org.Organization.MSPName)
	if err != nil {
		return nil, err
	}

	return &testIdentityAuthority{
		mspCACertPEM: mspCert,
		mspCAKey:     mspKey,
		tlsCACertPEM: tlsCert,
		tlsCAKey:     tlsKey,
	}, nil
}

func buildTestMSPSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	workloadName string,
	component string,
	kind string,
	authority *testIdentityAuthority,
) (*corev1.Secret, error) {
	certPEM, keyPEM, err := issueTestCertificate(
		workloadName,
		component,
		nil,
		authority.mspCACertPEM,
		authority.mspCAKey,
		x509.KeyUsageDigitalSignature,
		nil,
	)
	if err != nil {
		return nil, err
	}

	data := map[string][]byte{
		mspConfigKey:   []byte(mspConfigYAML()),
		mspCACertKey:   authority.mspCACertPEM,
		mspSignCertKey: certPEM,
		mspKeyStoreKey: keyPEM,
	}
	if net.Spec.Global.TLS {
		data[mspTLSCACertKey] = authority.tlsCACertPEM
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identitySecretName(workloadName, secretKindMSP),
			Namespace: namespace,
			Labels: identityLabels(net, org, component, workloadName, map[string]string{
				labelIdentityKind: kind,
				labelWorkload:     workloadName,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}, nil
}

func buildTestAdminTLSSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	adminName string,
	authority *testIdentityAuthority,
) (*corev1.Secret, error) {
	certPEM, keyPEM, err := issueTestCertificate(
		adminName,
		componentAdmin,
		nil,
		authority.tlsCACertPEM,
		authority.tlsCAKey,
		x509.KeyUsageDigitalSignature,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	)
	if err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identitySecretName(adminName, secretKindTLS),
			Namespace: namespace,
			Labels: identityLabels(net, org, componentAdmin, adminName, map[string]string{
				labelIdentityKind: secretKindAdminTLS,
				labelWorkload:     adminName,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			tlsCACertKey:     authority.tlsCACertPEM,
			tlsClientCertKey: certPEM,
			tlsClientKeyKey:  keyPEM,
		},
	}, nil
}

func buildTestWorkloadTLSSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	workloadName string,
	component string,
	dnsNames []string,
	authority *testIdentityAuthority,
) (*corev1.Secret, error) {
	certPEM, keyPEM, err := issueTestCertificate(
		workloadName,
		component,
		dnsNames,
		authority.tlsCACertPEM,
		authority.tlsCAKey,
		x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	)
	if err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identitySecretName(workloadName, secretKindTLS),
			Namespace: namespace,
			Labels: identityLabels(net, org, component, workloadName, map[string]string{
				labelIdentityKind: secretKindTLS,
				labelWorkload:     workloadName,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			tlsCACertKey:     authority.tlsCACertPEM,
			tlsServerCertKey: certPEM,
			tlsServerKeyKey:  keyPEM,
		},
	}, nil
}

func generateTestCA(commonName, organization string) ([]byte, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomTestSerial()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{organization},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	return pemEncodeTestCertificate(certDER), key, nil
}

func issueTestCertificate(
	commonName string,
	organizationalUnit string,
	dnsNames []string,
	caCertPEM []byte,
	caKey *ecdsa.PrivateKey,
	keyUsage x509.KeyUsage,
	usages []x509.ExtKeyUsage,
) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	caCert, err := parsePEMCertificate(caCertPEM)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomTestSerial()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			OrganizationalUnit: []string{organizationalUnit},
		},
		DNSNames:              dnsNames,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	return pemEncodeTestCertificate(certDER), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

func randomTestSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func pemEncodeTestCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func upsertSecret(ctx context.Context, desired *corev1.Secret) {
	var existing corev1.Secret
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: desired.Namespace,
		Name:      desired.Name,
	}, &existing)
	if errors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, desired)).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())

	existing.Labels = desired.Labels
	existing.Type = desired.Type
	existing.Data = desired.Data
	Expect(k8sClient.Update(ctx, &existing)).To(Succeed())
}

func envMap(container corev1.Container) map[string]string {
	env := map[string]string{}
	for _, item := range container.Env {
		env[item.Name] = item.Value
	}
	return env
}

func envSecretRefs(container corev1.Container) map[string]string {
	env := map[string]string{}
	for _, item := range container.Env {
		if item.ValueFrom == nil || item.ValueFrom.SecretKeyRef == nil {
			continue
		}
		env[item.Name] = item.ValueFrom.SecretKeyRef.Name + "/" + item.ValueFrom.SecretKeyRef.Key
	}
	return env
}

func secretVolumeNames(podSpec corev1.PodSpec) map[string]string {
	secrets := map[string]string{}
	for _, volume := range podSpec.Volumes {
		if volume.Secret == nil {
			continue
		}
		secrets[volume.Name] = volume.Secret.SecretName
	}
	return secrets
}

func secretVolumeItemKeys(podSpec corev1.PodSpec, volumeName string) []string {
	for _, volume := range podSpec.Volumes {
		if volume.Name != volumeName || volume.Secret == nil {
			continue
		}

		keys := make([]string, 0, len(volume.Secret.Items))
		for _, item := range volume.Secret.Items {
			keys = append(keys, item.Key)
		}
		return keys
	}
	return nil
}

func volumeMountPaths(container corev1.Container) map[string]string {
	mounts := map[string]string{}
	for _, mount := range container.VolumeMounts {
		mounts[mount.Name] = mount.MountPath
	}
	return mounts
}

func containerPorts(container corev1.Container) []int32 {
	ports := make([]int32, 0, len(container.Ports))
	for _, port := range container.Ports {
		ports = append(ports, port.ContainerPort)
	}
	return ports
}

func expectIdentitySecret(ctx context.Context, namespace, name, kind string, tlsEnabled bool) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, secret)).To(Succeed())

	Expect(identitySecretValidationError(*secret, kind, tlsEnabled)).To(BeEmpty())
	Expect(secret.Labels[labelIdentityKind]).To(Equal(kind))
}

func expectIdentitySecretSource(ctx context.Context, namespace, name, kind string, tlsEnabled bool, source string) {
	expectIdentitySecret(ctx, namespace, name, kind, tlsEnabled)

	var secret corev1.Secret
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &secret)).To(Succeed())
	Expect(secret.Labels[labelIdentitySource]).To(Equal(source))
}

func expectSecretNotFound(ctx context.Context, namespace, name string) {
	var secret corev1.Secret
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &secret)
	Expect(errors.IsNotFound(err)).To(BeTrue())
}

func expectDeploymentNotFound(ctx context.Context, namespace, name string) {
	var deploy appsv1.Deployment
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &deploy)
	Expect(errors.IsNotFound(err)).To(BeTrue())
}

func expectServiceNotFound(ctx context.Context, namespace, name string) {
	var service corev1.Service
	err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, &service)
	Expect(errors.IsNotFound(err)).To(BeTrue())
}

func servicePorts(service corev1.Service) []int32 {
	ports := make([]int32, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, port.Port)
	}
	return ports
}
