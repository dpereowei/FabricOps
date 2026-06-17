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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			Expect(network.Status.Phase).To(Equal(fabricopsv1alpha1.PhaseCreating))
			Expect(network.Status.Message).To(Equal("Waiting for Fabric components to become ready"))
			Expect(network.Status.OrgStatus).To(HaveLen(2))
			Expect(network.Status.OrgStatus[0].Name).To(Equal("Orderer"))
			Expect(network.Status.OrgStatus[0].Namespace).To(Equal(ordererNamespace))
			Expect(network.Status.OrgStatus[0].IdentityReady).To(BeTrue())
			Expect(network.Status.OrgStatus[0].IdentityError).To(BeEmpty())
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
			Expect(network.Status.OrgStatus[1].IdentityReady).To(BeTrue())
			Expect(network.Status.OrgStatus[1].IdentityError).To(BeEmpty())
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
			Expect(ready.Reason).To(Equal("ComponentsNotReady"))

			identity := apiMeta.FindStatusCondition(network.Status.Conditions, conditionIdentityMaterialReady)
			Expect(identity).NotTo(BeNil())
			Expect(identity.Status).To(Equal(metav1.ConditionTrue))
			Expect(identity.Reason).To(Equal("IdentityMaterialPresent"))

			expectIdentitySecret(ctx, ordererNamespace, caBootstrapSecretName(network.Spec.Orgs[0]), secretKindCABootstrap, true)
			expectIdentitySecret(ctx, ordererNamespace, orgIdentitySecretName(network.Spec.Orgs[0]), secretKindOrgCA, true)
			expectIdentitySecret(ctx, ordererNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[0]), secretKindMSP), secretKindAdminMSP, true)
			expectIdentitySecret(ctx, ordererNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[0]), secretKindTLS), secretKindAdminTLS, true)
			expectIdentitySecret(ctx, ordererNamespace, "orderer0-msp", secretKindMSP, true)
			expectIdentitySecret(ctx, ordererNamespace, "orderer0-tls", secretKindTLS, true)
			expectIdentitySecret(ctx, bankNamespace, caBootstrapSecretName(network.Spec.Orgs[1]), secretKindCABootstrap, true)
			expectIdentitySecret(ctx, bankNamespace, orgIdentitySecretName(network.Spec.Orgs[1]), secretKindOrgCA, true)
			expectIdentitySecret(ctx, bankNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[1]), secretKindMSP), secretKindAdminMSP, true)
			expectIdentitySecret(ctx, bankNamespace, identitySecretName(adminIdentityName(network.Spec.Orgs[1]), secretKindTLS), secretKindAdminTLS, true)
			expectIdentitySecret(ctx, bankNamespace, "peer0-msp", secretKindMSP, true)
			expectIdentitySecret(ctx, bankNamespace, "peer0-tls", secretKindTLS, true)
		})

		It("should recreate missing and malformed generated identity secrets", func() {
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

			By("Deleting one generated secret and corrupting another")
			var peerTLS corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "peer0-tls",
			}, &peerTLS)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &peerTLS)).To(Succeed())

			var ordererMSP corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ordererNamespace,
				Name:      "orderer0-msp",
			}, &ordererMSP)).To(Succeed())
			ordererMSP.Data[mspSignCertKey] = []byte("not a pem certificate")
			Expect(k8sClient.Update(ctx, &ordererMSP)).To(Succeed())

			var ordererAdminTLS corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ordererNamespace,
				Name:      "orderer-admin-tls",
			}, &ordererAdminTLS)).To(Succeed())
			ordererAdminTLS.Data[tlsClientCertKey] = []byte("not a pem certificate")
			Expect(k8sClient.Update(ctx, &ordererAdminTLS)).To(Succeed())

			var bankCABootstrap corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "banka-ca-bootstrap",
			}, &bankCABootstrap)).To(Succeed())
			bankCABootstrap.Data[caBootstrapUserPassKey] = []byte("admin:not-the-current-password")
			Expect(k8sClient.Update(ctx, &bankCABootstrap)).To(Succeed())

			By("Reconciling again")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			expectIdentitySecret(ctx, bankNamespace, "peer0-tls", secretKindTLS, true)
			expectIdentitySecret(ctx, ordererNamespace, "orderer0-msp", secretKindMSP, true)
			expectIdentitySecret(ctx, ordererNamespace, "orderer-admin-tls", secretKindAdminTLS, true)
			expectIdentitySecret(ctx, bankNamespace, "banka-ca-bootstrap", secretKindCABootstrap, true)

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			Expect(network.Status.OrgStatus[0].IdentityReady).To(BeTrue())
			Expect(network.Status.OrgStatus[1].IdentityReady).To(BeTrue())
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
			markDeploymentReady(ctx, ordererNamespace, "orderer0")
			markDeploymentReady(ctx, bankNamespace, "banka-ca")
			markDeploymentReady(ctx, bankNamespace, "peer0")

			By("Reconciling after workloads report readiness")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var network fabricopsv1alpha1.FabricNetwork
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
	deleteAllDeployments(ctx, namespace)
	deleteAllServices(ctx, namespace)
	deleteAllSecrets(ctx, namespace)
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

	if kind == secretKindOrgCA {
		Expect(orgIdentitySecretValidationError(*secret)).To(BeEmpty())
		return
	}

	Expect(identitySecretValidationError(*secret, kind, tlsEnabled)).To(BeEmpty())
	Expect(secret.Labels[labelIdentityKind]).To(Equal(kind))
}

func servicePorts(service corev1.Service) []int32 {
	ports := make([]int32, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, port.Port)
	}
	return ports
}
