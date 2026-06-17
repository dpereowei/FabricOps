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
	"reflect"
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
	labelIdentityKind           = "fabricops.my.domain/identity-kind"
	labelWorkload               = "fabricops.my.domain/workload"

	componentCA      = "ca"
	componentAdmin   = "admin"
	componentOrderer = "orderer"
	componentPeer    = "peer"

	containerCA      = "fabric-ca"
	containerOrderer = "orderer"
	containerPeer    = "peer"

	caPort            int32 = 7054
	ordererPort       int32 = 7050
	peerPort          int32 = 7051
	peerChaincodePort int32 = 7052

	caBootstrapEnvVar = "FABRIC_CA_SERVER_BOOTSTRAP_USER_PASS"

	ordererMSPPath = "/var/hyperledger/orderer/msp"
	ordererTLSPath = "/var/hyperledger/orderer/tls"
	peerMSPPath    = "/etc/hyperledger/fabric/peer/msp"
	peerTLSPath    = "/etc/hyperledger/fabric/peer/tls"

	secretKindMSP = "msp"
	secretKindTLS = "tls"

	mspConfigKey    = "config.yaml"
	mspCACertKey    = "cacert.pem"
	mspTLSCACertKey = "tlscacert.pem"
	mspSignCertKey  = "signcert.pem"
	mspKeyStoreKey  = "keystore.pem"

	caBootstrapUsernameKey = "username"
	caBootstrapPasswordKey = "password"
	caBootstrapUserPassKey = "user-pass"

	tlsCACertKey     = "ca.crt"
	tlsServerCertKey = "server.crt"
	tlsServerKeyKey  = "server.key"
	tlsClientCertKey = "client.crt"
	tlsClientKeyKey  = "client.key"
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

func serviceDNS(name, namespace string, port int32) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", name, namespace, port)
}

func identitySecretName(workloadName, kind string) string {
	return sanitizeName(fmt.Sprintf("%s-%s", workloadName, kind))
}

func mspSecretItems(tlsEnabled bool) []corev1.KeyToPath {
	items := []corev1.KeyToPath{
		{Key: mspConfigKey, Path: "config.yaml"},
		{Key: mspCACertKey, Path: "cacerts/ca.pem"},
		{Key: mspSignCertKey, Path: "signcerts/cert.pem"},
		{Key: mspKeyStoreKey, Path: "keystore/key.pem"},
	}

	if tlsEnabled {
		items = append(items, corev1.KeyToPath{
			Key:  mspTLSCACertKey,
			Path: "tlscacerts/tlsca.pem",
		})
	}

	return items
}

func tlsSecretItems() []corev1.KeyToPath {
	return []corev1.KeyToPath{
		{Key: tlsCACertKey, Path: "ca.crt"},
		{Key: tlsServerCertKey, Path: "server.crt"},
		{Key: tlsServerKeyKey, Path: "server.key"},
	}
}

func identityVolumes(workloadName string, tlsEnabled bool) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: secretKindMSP,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: identitySecretName(workloadName, secretKindMSP),
					Items:      mspSecretItems(tlsEnabled),
				},
			},
		},
	}

	if tlsEnabled {
		volumes = append(volumes, corev1.Volume{
			Name: secretKindTLS,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: identitySecretName(workloadName, secretKindTLS),
					Items:      tlsSecretItems(),
				},
			},
		})
	}

	return volumes
}

func identityVolumeMounts(mspPath, tlsPath string, tlsEnabled bool) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{
			Name:      secretKindMSP,
			MountPath: mspPath,
			ReadOnly:  true,
		},
	}

	if tlsEnabled {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      secretKindTLS,
			MountPath: tlsPath,
			ReadOnly:  true,
		})
	}

	return mounts
}

type identitySecretRequirement struct {
	namespace string
	name      string
	kind      string
	keys      []string
}

func mspSecretKeys(tlsEnabled bool) []string {
	keys := []string{
		mspConfigKey,
		mspCACertKey,
		mspSignCertKey,
		mspKeyStoreKey,
	}

	if tlsEnabled {
		keys = append(keys, mspTLSCACertKey)
	}

	return keys
}

func tlsSecretKeys() []string {
	return []string{
		tlsCACertKey,
		tlsServerCertKey,
		tlsServerKeyKey,
	}
}

func adminTLSSecretKeys() []string {
	return []string{
		tlsCACertKey,
		tlsClientCertKey,
		tlsClientKeyKey,
	}
}

func caBootstrapSecretKeys() []string {
	return []string{
		caBootstrapUsernameKey,
		caBootstrapPasswordKey,
		caBootstrapUserPassKey,
	}
}

func requiredIdentitySecrets(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) []identitySecretRequirement {
	tlsEnabled := net.Spec.Global.TLS
	requirements := []identitySecretRequirement{}
	adminName := adminIdentityName(org)

	requirements = append(requirements, identitySecretRequirement{
		namespace: namespace,
		name:      caBootstrapSecretName(org),
		kind:      secretKindCABootstrap,
		keys:      caBootstrapSecretKeys(),
	})

	requirements = append(requirements, identitySecretRequirement{
		namespace: namespace,
		name:      identitySecretName(adminName, secretKindMSP),
		kind:      secretKindAdminMSP,
		keys:      mspSecretKeys(tlsEnabled),
	})

	if tlsEnabled {
		requirements = append(requirements, identitySecretRequirement{
			namespace: namespace,
			name:      identitySecretName(adminName, secretKindTLS),
			kind:      secretKindAdminTLS,
			keys:      adminTLSSecretKeys(),
		})
	}

	for _, group := range org.Orderers {
		for i := 0; i < group.Instances; i++ {
			name := sanitizeName(fmt.Sprintf("%s%d", group.Prefix, i))
			requirements = append(requirements, identitySecretRequirement{
				namespace: namespace,
				name:      identitySecretName(name, secretKindMSP),
				kind:      secretKindMSP,
				keys:      mspSecretKeys(tlsEnabled),
			})

			if tlsEnabled {
				requirements = append(requirements, identitySecretRequirement{
					namespace: namespace,
					name:      identitySecretName(name, secretKindTLS),
					kind:      secretKindTLS,
					keys:      tlsSecretKeys(),
				})
			}
		}
	}

	if org.Peer == nil {
		return requirements
	}

	for i := 0; i < org.Peer.Instances; i++ {
		name := sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, i))
		requirements = append(requirements, identitySecretRequirement{
			namespace: namespace,
			name:      identitySecretName(name, secretKindMSP),
			kind:      secretKindMSP,
			keys:      mspSecretKeys(tlsEnabled),
		})

		if tlsEnabled {
			requirements = append(requirements, identitySecretRequirement{
				namespace: namespace,
				name:      identitySecretName(name, secretKindTLS),
				kind:      secretKindTLS,
				keys:      tlsSecretKeys(),
			})
		}
	}

	return requirements
}

func missingSecretKeys(secret corev1.Secret, keys []string) []string {
	missing := []string{}
	for _, key := range keys {
		if _, ok := secret.Data[key]; !ok {
			missing = append(missing, key)
		}
	}

	return missing
}

func (r *FabricNetworkReconciler) identityMaterialStatus(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) (bool, string, error) {
	missing := []string{}

	for _, requirement := range requiredIdentitySecrets(net, org, namespace) {
		var secret corev1.Secret
		err := r.Get(ctx, client.ObjectKey{
			Namespace: requirement.namespace,
			Name:      requirement.name,
		}, &secret)
		if apierrors.IsNotFound(err) {
			missing = append(missing, fmt.Sprintf("%s/%s", requirement.namespace, requirement.name))
			continue
		}
		if err != nil {
			return false, "", err
		}

		missingKeys := missingSecretKeys(secret, requirement.keys)
		if len(missingKeys) > 0 {
			missing = append(missing, fmt.Sprintf("%s/%s missing keys: %s", requirement.namespace, requirement.name, strings.Join(missingKeys, ",")))
			continue
		}

		if validationError := identitySecretValidationError(secret, requirement.kind, net.Spec.Global.TLS); validationError != "" {
			missing = append(missing, fmt.Sprintf("%s/%s invalid: %s", requirement.namespace, requirement.name, validationError))
		}
	}

	if len(missing) > 0 {
		return false, "Missing identity material: " + strings.Join(missing, "; "), nil
	}

	return true, "", nil
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
		changed := false
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
			changed = true
		}
		for key, value := range desired.Labels {
			if existing.Labels[key] != value {
				existing.Labels[key] = value
				changed = true
			}
		}
		if !reflect.DeepEqual(existing.Spec.Replicas, desired.Spec.Replicas) {
			existing.Spec.Replicas = desired.Spec.Replicas
			changed = true
		}
		if !reflect.DeepEqual(existing.Spec.Template.Labels, desired.Spec.Template.Labels) {
			existing.Spec.Template.Labels = desired.Spec.Template.Labels
			changed = true
		}
		if !reflect.DeepEqual(existing.Spec.Template.Spec.Containers, desired.Spec.Template.Spec.Containers) {
			existing.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
			changed = true
		}
		if !reflect.DeepEqual(existing.Spec.Template.Spec.Volumes, desired.Spec.Template.Spec.Volumes) {
			existing.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
			changed = true
		}
		if !changed {
			return nil
		}

		log := logf.FromContext(ctx)
		log.Info("Updating Deployment", "name", desired.Name, "namespace", desired.Namespace)
		return r.Update(ctx, &existing)
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
	status, err := r.deploymentWorkloadStatus(ctx, namespace, name)
	if err != nil {
		return false, err
	}

	return workloadReady(status), nil
}

func (r *FabricNetworkReconciler) deploymentWorkloadStatus(
	ctx context.Context,
	namespace, name string,
) (fabricopsv1alpha1.WorkloadStatus, error) {
	var deploy appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &deploy); err != nil {
		return fabricopsv1alpha1.WorkloadStatus{}, err
	}

	if deploy.Spec.Replicas == nil {
		return fabricopsv1alpha1.WorkloadStatus{}, nil
	}

	return fabricopsv1alpha1.WorkloadStatus{
		Desired: *deploy.Spec.Replicas,
		Ready:   deploy.Status.ReadyReplicas,
	}, nil
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
								{
									Name: caBootstrapEnvVar,
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: caBootstrapSecretName(org),
											},
											Key: caBootstrapUserPassKey,
										},
									},
								},
							},
							Command: []string{
								"sh", "-c",
								"fabric-ca-server start -b \"$" + caBootstrapEnvVar + "\" -d",
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
	tlsEnabled := net.Spec.Global.TLS
	env := []corev1.EnvVar{
		{Name: "ORDERER_GENERAL_LISTENADDRESS", Value: "0.0.0.0"},
		{Name: "ORDERER_GENERAL_LISTENPORT", Value: fmt.Sprintf("%d", ordererPort)},
		{Name: "ORDERER_GENERAL_LOCALMSPID", Value: org.Organization.MSPName},
		{Name: "ORDERER_GENERAL_LOCALMSPDIR", Value: ordererMSPPath},
	}

	if tlsEnabled {
		env = append(env,
			corev1.EnvVar{Name: "ORDERER_GENERAL_TLS_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "ORDERER_GENERAL_TLS_PRIVATEKEY", Value: ordererTLSPath + "/server.key"},
			corev1.EnvVar{Name: "ORDERER_GENERAL_TLS_CERTIFICATE", Value: ordererTLSPath + "/server.crt"},
			corev1.EnvVar{Name: "ORDERER_GENERAL_TLS_ROOTCAS", Value: "[" + ordererTLSPath + "/ca.crt]"},
			corev1.EnvVar{Name: "ORDERER_GENERAL_CLUSTER_CLIENTCERTIFICATE", Value: ordererTLSPath + "/server.crt"},
			corev1.EnvVar{Name: "ORDERER_GENERAL_CLUSTER_CLIENTPRIVATEKEY", Value: ordererTLSPath + "/server.key"},
			corev1.EnvVar{Name: "ORDERER_GENERAL_CLUSTER_ROOTCAS", Value: "[" + ordererTLSPath + "/ca.crt]"},
		)
	}

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
					Volumes: identityVolumes(name, net.Spec.Global.TLS),
					Containers: []corev1.Container{
						{
							Name:  containerOrderer,
							Image: fabricComponentImage("orderer", net.Spec.Global.FabricVersion),
							Env:   env,
							Ports: []corev1.ContainerPort{
								{ContainerPort: ordererPort, Name: "orderer"},
							},
							VolumeMounts: identityVolumeMounts(ordererMSPPath, ordererTLSPath, tlsEnabled),
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
	peerAddress := serviceDNS(name, namespace, peerPort)
	chaincodeAddress := serviceDNS(name, namespace, peerChaincodePort)
	tlsEnabled := net.Spec.Global.TLS
	selector := map[string]string{
		labelFabricNetwork:          sanitizeName(net.Name),
		labelFabricNetworkNamespace: sanitizeName(net.Namespace),
		labelOrg:                    sanitizeName(org.Organization.Name),
		labelComponent:              componentPeer,
		labelInstance:               fmt.Sprintf("%d", instance),
	}
	env := []corev1.EnvVar{
		{Name: "CORE_PEER_ID", Value: name},
		{Name: "CORE_PEER_ADDRESS", Value: peerAddress},
		{Name: "CORE_PEER_LISTENADDRESS", Value: fmt.Sprintf("0.0.0.0:%d", peerPort)},
		{Name: "CORE_PEER_CHAINCODEADDRESS", Value: chaincodeAddress},
		{Name: "CORE_PEER_CHAINCODELISTENADDRESS", Value: fmt.Sprintf("0.0.0.0:%d", peerChaincodePort)},
		{Name: "CORE_PEER_GOSSIP_ENDPOINT", Value: peerAddress},
		{Name: "CORE_PEER_GOSSIP_EXTERNALENDPOINT", Value: peerAddress},
		{Name: "CORE_PEER_LOCALMSPID", Value: org.Organization.MSPName},
		{Name: "CORE_PEER_MSPCONFIGPATH", Value: peerMSPPath},
	}

	if tlsEnabled {
		env = append(env,
			corev1.EnvVar{Name: "CORE_PEER_TLS_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "CORE_PEER_TLS_CERT_FILE", Value: peerTLSPath + "/server.crt"},
			corev1.EnvVar{Name: "CORE_PEER_TLS_KEY_FILE", Value: peerTLSPath + "/server.key"},
			corev1.EnvVar{Name: "CORE_PEER_TLS_ROOTCERT_FILE", Value: peerTLSPath + "/ca.crt"},
		)
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
					Volumes: identityVolumes(name, net.Spec.Global.TLS),
					Containers: []corev1.Container{
						{
							Name:  containerPeer,
							Image: fabricComponentImage("peer", net.Spec.Global.FabricVersion),
							Env:   env,
							Ports: []corev1.ContainerPort{
								{ContainerPort: peerPort, Name: "peer"},
								{ContainerPort: peerChaincodePort, Name: "chaincode"},
							},
							VolumeMounts: identityVolumeMounts(peerMSPPath, peerTLSPath, tlsEnabled),
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
				{
					Name:       "chaincode",
					Port:       peerChaincodePort,
					TargetPort: intstr.FromInt32(peerChaincodePort),
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
) (fabricopsv1alpha1.WorkloadStatus, error) {
	status := fabricopsv1alpha1.WorkloadStatus{}

	for _, group := range org.Orderers {
		for i := 0; i < group.Instances; i++ {
			deploy := buildOrdererDeployment(net, org, group, i, namespace)
			if err := r.ensureDeployment(ctx, deploy); err != nil {
				return status, err
			}

			svc := buildOrdererService(net, org, group, i, namespace)
			if err := r.ensureService(ctx, svc); err != nil {
				return status, err
			}

			deploymentStatus, err := r.deploymentWorkloadStatus(ctx, namespace, deploy.Name)
			if err != nil {
				return status, err
			}
			status.Desired += deploymentStatus.Desired
			status.Ready += deploymentStatus.Ready
		}
	}

	return status, nil
}

func (r *FabricNetworkReconciler) reconcilePeers(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) (fabricopsv1alpha1.WorkloadStatus, error) {
	status := fabricopsv1alpha1.WorkloadStatus{}

	if org.Peer == nil {
		return status, nil
	}

	for i := 0; i < org.Peer.Instances; i++ {
		deploy := buildPeerDeployment(net, org, i, namespace)
		if err := r.ensureDeployment(ctx, deploy); err != nil {
			return status, err
		}

		svc := buildPeerService(net, org, i, namespace)
		if err := r.ensureService(ctx, svc); err != nil {
			return status, err
		}

		deploymentStatus, err := r.deploymentWorkloadStatus(ctx, namespace, deploy.Name)
		if err != nil {
			return status, err
		}
		status.Desired += deploymentStatus.Desired
		status.Ready += deploymentStatus.Ready
	}

	return status, nil
}
