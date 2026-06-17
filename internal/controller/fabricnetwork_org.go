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
	"fmt"
	"hash/fnv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

const (
	maxKubernetesNameLength = 63

	labelFabricNetwork          = "fabricops.my.domain/fabricnetwork"
	labelFabricNetworkNamespace = "fabricops.my.domain/fabricnetwork-namespace"
	labelOrg                    = "fabricops.my.domain/org"
	labelComponent              = "fabricops.my.domain/component"
	labelOrdererGroup           = "fabricops.my.domain/orderer-group"
	labelInstance               = "fabricops.my.domain/instance"

	componentCA      = "ca"
	componentOrderer = "orderer"
	componentPeer    = "peer"

	containerCA      = "fabric-ca"
	containerOrderer = "orderer"
	containerPeer    = "peer"

	caPort      int32 = 7054
	ordererPort int32 = 7050
	peerPort    int32 = 7051
)

func sanitizeName(name string) string {
	var b strings.Builder
	lastDash := false

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}

		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "resource"
	}

	if len(s) <= maxKubernetesNameLength {
		return s
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(s))
	suffix := fmt.Sprintf("%08x", hash.Sum32())
	prefixLength := maxKubernetesNameLength - len(suffix) - 1
	prefix := strings.TrimRight(s[:prefixLength], "-")
	if prefix == "" {
		prefix = s[:prefixLength]
	}

	return prefix + "-" + suffix
}

func networkNamespaceSlug(net *fabricopsv1alpha1.FabricNetwork) string {
	networkName := compactNetworkName(net.Name)
	controlNamespace := sanitizeName(net.Namespace)

	if controlNamespace == "" || controlNamespace == "default" {
		return networkName
	}

	return sanitizeName(controlNamespace + "-" + networkName)
}

func compactNetworkName(name string) string {
	s := sanitizeName(name)
	for _, prefix := range []string{"fabricnetwork-", "fabric-network-", "network-"} {
		if trimmed := strings.TrimPrefix(s, prefix); trimmed != s && trimmed != "" {
			return trimmed
		}
	}

	return s
}

func orgLabels(net *fabricopsv1alpha1.FabricNetwork, org fabricopsv1alpha1.Org, component string) map[string]string {
	return map[string]string{
		labelFabricNetwork:             sanitizeName(net.Name),
		labelFabricNetworkNamespace:    sanitizeName(net.Namespace),
		labelOrg:                       sanitizeName(org.Organization.Name),
		labelComponent:                 component,
		"app.kubernetes.io/managed-by": "fabricops",
	}
}

func caImage() string {
	return "hyperledger/fabric-ca:1.5.15"
}

func fabricComponentImage(component, version string) string {
	if version == "" {
		version = "2.5.12"
	}
	return fmt.Sprintf("hyperledger/fabric-%s:%s", component, version)
}

func (r *FabricNetworkReconciler) ensureDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) error {
	var existing appsv1.Deployment
	key := client.ObjectKeyFromObject(desired)

	err := r.Get(ctx, key, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	log := logf.FromContext(ctx)
	log.Info("Creating Deployment", "name", desired.Name, "namespace", desired.Namespace)
	return r.Create(ctx, desired)
}

func (r *FabricNetworkReconciler) ensureService(
	ctx context.Context,
	desired *corev1.Service,
) error {
	var existing corev1.Service
	key := client.ObjectKeyFromObject(desired)

	err := r.Get(ctx, key, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	log := logf.FromContext(ctx)
	log.Info("Creating Service", "name", desired.Name, "namespace", desired.Namespace)
	return r.Create(ctx, desired)
}

func (r *FabricNetworkReconciler) isDeploymentReady(
	ctx context.Context,
	namespace, name string,
) (bool, error) {
	var deploy appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &deploy); err != nil {
		return false, err
	}

	if deploy.Spec.Replicas == nil {
		return false, nil
	}

	return deploy.Status.ReadyReplicas >= *deploy.Spec.Replicas, nil
}

func buildCADeployment(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) *appsv1.Deployment {
	name := sanitizeName(org.Organization.Name + "-ca")
	replicas := int32(1)
	labels := orgLabels(net, org, componentCA)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerCA,
							Image: caImage(),
							Env: []corev1.EnvVar{
								{Name: "FABRIC_CA_HOME", Value: "/etc/hyperledger/fabric-ca-server"},
								{Name: "FABRIC_CA_SERVER_CA_NAME", Value: sanitizeName(org.Organization.Name)},
								{Name: "FABRIC_CA_SERVER_PORT", Value: fmt.Sprintf("%d", caPort)},
							},
							Command: []string{
								"sh", "-c",
								"fabric-ca-server start -b admin:adminpw -d",
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: caPort, Name: "ca"},
							},
						},
					},
				},
			},
		},
	}
}

func buildCAService(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) *corev1.Service {
	name := sanitizeName(org.Organization.Name + "-ca")
	labels := orgLabels(net, org, componentCA)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "ca",
					Port:       caPort,
					TargetPort: intstr.FromInt32(caPort),
				},
			},
		},
	}
}

func buildOrdererDeployment(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	group fabricopsv1alpha1.OrdererGroup,
	instance int,
	namespace string,
) *appsv1.Deployment {
	name := sanitizeName(fmt.Sprintf("%s%d", group.Prefix, instance))
	replicas := int32(1)
	labels := orgLabels(net, org, componentOrderer)
	labels[labelOrdererGroup] = sanitizeName(group.GroupName)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelFabricNetwork:          sanitizeName(net.Name),
					labelFabricNetworkNamespace: sanitizeName(net.Namespace),
					labelOrg:                    sanitizeName(org.Organization.Name),
					labelComponent:              componentOrderer,
					labelOrdererGroup:           sanitizeName(group.GroupName),
					labelInstance:               fmt.Sprintf("%d", instance),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelFabricNetwork:          sanitizeName(net.Name),
						labelFabricNetworkNamespace: sanitizeName(net.Namespace),
						labelOrg:                    sanitizeName(org.Organization.Name),
						labelComponent:              componentOrderer,
						labelOrdererGroup:           sanitizeName(group.GroupName),
						labelInstance:               fmt.Sprintf("%d", instance),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerOrderer,
							Image: fabricComponentImage("orderer", net.Spec.Global.FabricVersion),
							Env: []corev1.EnvVar{
								{Name: "ORDERER_GENERAL_LISTENADDRESS", Value: "0.0.0.0"},
								{Name: "ORDERER_GENERAL_LISTENPORT", Value: fmt.Sprintf("%d", ordererPort)},
								{Name: "ORDERER_GENERAL_LOCALMSPID", Value: org.Organization.MSPName},
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: ordererPort, Name: "orderer"},
							},
						},
					},
				},
			},
		},
	}
}

func buildOrdererService(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	group fabricopsv1alpha1.OrdererGroup,
	instance int,
	namespace string,
) *corev1.Service {
	name := sanitizeName(fmt.Sprintf("%s%d", group.Prefix, instance))
	selector := map[string]string{
		labelFabricNetwork:          sanitizeName(net.Name),
		labelFabricNetworkNamespace: sanitizeName(net.Namespace),
		labelOrg:                    sanitizeName(org.Organization.Name),
		labelComponent:              componentOrderer,
		labelOrdererGroup:           sanitizeName(group.GroupName),
		labelInstance:               fmt.Sprintf("%d", instance),
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    selector,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{
				{
					Name:       "orderer",
					Port:       ordererPort,
					TargetPort: intstr.FromInt32(ordererPort),
				},
			},
		},
	}
}

func buildPeerDeployment(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	instance int,
	namespace string,
) *appsv1.Deployment {
	name := sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, instance))
	replicas := int32(1)
	selector := map[string]string{
		labelFabricNetwork:          sanitizeName(net.Name),
		labelFabricNetworkNamespace: sanitizeName(net.Namespace),
		labelOrg:                    sanitizeName(org.Organization.Name),
		labelComponent:              componentPeer,
		labelInstance:               fmt.Sprintf("%d", instance),
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    selector,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerPeer,
							Image: fabricComponentImage("peer", net.Spec.Global.FabricVersion),
							Env: []corev1.EnvVar{
								{Name: "CORE_PEER_ID", Value: name},
								{Name: "CORE_PEER_ADDRESS", Value: fmt.Sprintf("%s:%d", name, peerPort)},
								{Name: "CORE_PEER_LISTENADDRESS", Value: "0.0.0.0:7051"},
								{Name: "CORE_PEER_LOCALMSPID", Value: org.Organization.MSPName},
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: peerPort, Name: "peer"},
							},
						},
					},
				},
			},
		},
	}
}

func buildPeerService(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	instance int,
	namespace string,
) *corev1.Service {
	name := sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, instance))
	selector := map[string]string{
		labelFabricNetwork:          sanitizeName(net.Name),
		labelFabricNetworkNamespace: sanitizeName(net.Namespace),
		labelOrg:                    sanitizeName(org.Organization.Name),
		labelComponent:              componentPeer,
		labelInstance:               fmt.Sprintf("%d", instance),
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    selector,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{
				{
					Name:       "peer",
					Port:       peerPort,
					TargetPort: intstr.FromInt32(peerPort),
				},
			},
		},
	}
}

func (r *FabricNetworkReconciler) reconcileCA(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) (bool, error) {
	deploy := buildCADeployment(net, org, namespace)
	if err := r.ensureDeployment(ctx, deploy); err != nil {
		return false, err
	}

	svc := buildCAService(net, org, namespace)
	if err := r.ensureService(ctx, svc); err != nil {
		return false, err
	}

	ready, err := r.isDeploymentReady(ctx, namespace, deploy.Name)
	if err != nil {
		return false, err
	}

	return ready, nil
}

func (r *FabricNetworkReconciler) reconcileOrderers(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) error {
	for _, group := range org.Orderers {
		for i := 0; i < group.Instances; i++ {
			deploy := buildOrdererDeployment(net, org, group, i, namespace)
			if err := r.ensureDeployment(ctx, deploy); err != nil {
				return err
			}

			svc := buildOrdererService(net, org, group, i, namespace)
			if err := r.ensureService(ctx, svc); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *FabricNetworkReconciler) reconcilePeers(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) error {
	if org.Peer == nil {
		return nil
	}

	for i := 0; i < org.Peer.Instances; i++ {
		deploy := buildPeerDeployment(net, org, i, namespace)
		if err := r.ensureDeployment(ctx, deploy); err != nil {
			return err
		}

		svc := buildPeerService(net, org, i, namespace)
		if err := r.ensureService(ctx, svc); err != nil {
			return err
		}
	}

	return nil
}
