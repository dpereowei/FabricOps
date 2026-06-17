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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
		fabricnetwork := &fabricopsv1alpha1.FabricNetwork{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind FabricNetwork")
			err := k8sClient.Get(ctx, typeNamespacedName, fabricnetwork)
			if err != nil && errors.IsNotFound(err) {
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
			}
		})

		AfterEach(func() {
			resource := &fabricopsv1alpha1.FabricNetwork{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance FabricNetwork")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			for _, namespaceName := range []string{
				"fo-test-orderer",
				"fo-test-banka",
			} {
				namespace := &corev1.Namespace{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)
				if err == nil {
					Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
				}
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

			var ordererSvc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ordererNamespace,
				Name:      "orderer0",
			}, &ordererSvc)).To(Succeed())

			var caDeploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "banka-ca",
			}, &caDeploy)).To(Succeed())

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

			var peerSvc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: bankNamespace,
				Name:      "peer0",
			}, &peerSvc)).To(Succeed())

			var network fabricopsv1alpha1.FabricNetwork
			Expect(k8sClient.Get(ctx, typeNamespacedName, &network)).To(Succeed())
			Expect(network.Status.Phase).To(Equal(fabricopsv1alpha1.PhaseCreating))
			Expect(network.Status.OrgStatus).To(HaveLen(2))
			Expect(network.Status.OrgStatus[0].Name).To(Equal("Orderer"))
			Expect(network.Status.OrgStatus[0].Namespace).To(Equal(ordererNamespace))
			Expect(network.Status.OrgStatus[0].CAReady).To(BeFalse())
			Expect(network.Status.OrgStatus[1].Name).To(Equal("BankA"))
			Expect(network.Status.OrgStatus[1].Namespace).To(Equal(bankNamespace))
			Expect(network.Status.OrgStatus[1].CAReady).To(BeFalse())
		})
	})
})
