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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

const (
	chaincodeMetadataKey       = "metadata.json"
	chaincodeConnectionKey     = "connection.json"
	chaincodePackageLabelKey   = "packageLabel"
	chaincodePackageFileKey    = "packageFile"
	chaincodeConnectionAddrKey = "address"

	chaincodePackageIDKey       = "packageID"
	chaincodeChaincodeIDKey     = "chaincodeID"
	chaincodePackageHashKey     = "packageHash"
	chaincodeQueryInstalledKey  = "queryinstalled.json"
	chaincodePackageIDFile      = "package-id"
	chaincodeChaincodeIDFile    = "chaincode-id"
	chaincodePackageHashFile    = "package-hash"
	chaincodeQueryInstalledFile = "queryinstalled.json"
	chaincodePackageArchiveMode = 0o644

	chaincodeWorkDir         = "/fabricops/chaincode"
	chaincodePackageInputDir = chaincodeWorkDir + "/package"
	chaincodePackageBuildDir = chaincodeWorkDir + "/build"
	chaincodeOutputDir       = chaincodeWorkDir + "/output"
	chaincodeAdminMSPPath    = chaincodeWorkDir + "/crypto/msp"
	chaincodeAdminTLSPath    = chaincodeWorkDir + "/crypto/tls"

	chaincodePackageVolumeName = "chaincode-package"
	chaincodeOutputVolumeName  = "chaincode-output"
	chaincodeAdminMSPVolume    = "admin-msp"
	chaincodeAdminTLSVolume    = "admin-tls"

	installChaincodeContainer        = "install-chaincode-package"
	publishChaincodeInstallContainer = "publish-chaincode-package-id"
	approveChaincodeContainer        = "approve-chaincode-definition"
	commitChaincodeContainer         = "commit-chaincode-definition"
	chaincodeServerContainer         = "chaincode"

	envChaincodePackageIDConfigMap = "FABRICOPS_CHAINCODE_PACKAGE_ID_CONFIGMAP"
	envChaincodeChannel            = "FABRICOPS_CHAINCODE_CHANNEL"
	envChaincodeName               = "FABRICOPS_CHAINCODE_NAME"
	envChaincodePeer               = "FABRICOPS_CHAINCODE_PEER"
	envCCAASChaincodeID            = "CHAINCODE_ID"
	envCCAASCoreChaincodeIDName    = "CORE_CHAINCODE_ID_NAME"
	envCCAASChaincodeServerAddress = "CHAINCODE_SERVER_ADDRESS"
	envCCAASCoreChaincodeAddress   = "CORE_CHAINCODE_ADDRESS"

	chaincodePeerHostnameTemplate    = "{{.peer_hostname}}"
	chaincodePeerHostnamePlaceholder = "fabricops-peer-hostname"
)

type chaincodePackageMetadata struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

type chaincodeConnection struct {
	Address     string `json:"address"`
	DialTimeout string `json:"dial_timeout"`
	TLSRequired bool   `json:"tls_required"`
}

type chaincodeLifecyclePeer struct {
	org       fabricopsv1alpha1.Org
	peerName  string
	namespace string
}

func (r *FabricNetworkReconciler) reconcileChaincodes(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	channelStatuses []fabricopsv1alpha1.ChannelStatus,
) ([]fabricopsv1alpha1.ChaincodeStatus, error) {
	statuses := make([]fabricopsv1alpha1.ChaincodeStatus, 0, len(net.Spec.Chaincodes))
	channels := channelsByName(net)
	orgs := orgsByName(net)
	seen := map[string]struct{}{}

	for _, chaincode := range net.Spec.Chaincodes {
		status := fabricopsv1alpha1.ChaincodeStatus{
			Name:         chaincode.Name,
			Channel:      chaincode.Channel,
			Version:      chaincode.Version,
			PackageLabel: chaincodePackageLabel(chaincode),
			Sequence:     chaincodeSequence(chaincode),
		}
		messages := []string{}
		lifecycleMessage := ""
		approvalPeers := map[string]string{}
		channelReady := false

		key := chaincode.Channel + "/" + chaincode.Name
		if _, ok := seen[key]; ok {
			messages = append(messages, "Chaincode name must be unique within a channel")
		}
		seen[key] = struct{}{}

		if strings.TrimSpace(chaincode.Name) == "" {
			messages = append(messages, "Chaincode name is required")
		}
		if strings.TrimSpace(chaincode.Version) == "" {
			messages = append(messages, "Chaincode version is required")
		}
		if strings.TrimSpace(chaincode.Image) == "" {
			messages = append(messages, "Chaincode image is required for CCaaS")
		}

		channel, ok := channels[chaincode.Channel]
		if strings.TrimSpace(chaincode.Channel) == "" {
			messages = append(messages, "Chaincode channel is required")
		} else if !ok {
			messages = append(messages, "Unknown channel")
		} else {
			channelReady = channelReadyForChaincode(channelStatuses, chaincode.Channel)
			approvalPeers = chaincodeApprovalPeers(channel)
		}

		if ok {
			for _, channelOrg := range channel.Orgs {
				org, orgKnown := orgs[channelOrg.Name]
				for _, peerName := range channelOrg.Peers {
					target := fabricopsv1alpha1.ChaincodeTargetStatus{
						OrgName:  channelOrg.Name,
						PeerName: peerName,
					}
					status.PackageMetadata.Desired++

					if !orgKnown {
						target.Message = "Unknown org"
						status.Targets = append(status.Targets, target)
						continue
					}

					target.Namespace = orgNamespaceName(net, org)
					target.WorkloadName = chaincodeServiceName(chaincode, org, peerName)
					target.Workload.Desired = chaincodeReplicas(chaincode)
					target.ServiceName = chaincodeServiceName(chaincode, org, peerName)
					target.Address = chaincodeConnectionAddress(target.ServiceName, target.Namespace, chaincode)
					target.PackageConfigMapName = chaincodePackageConfigMapName(chaincode, org)
					target.PackageIDConfigMapName = chaincodePackageIDConfigMapName(chaincode, org, peerName)
					target.InstallJobName = chaincodeInstallJobName(chaincode, org, peerName)

					if !peerDeclared(org, peerName) {
						target.Message = "Unknown peer"
						status.Targets = append(status.Targets, target)
						continue
					}
					if len(messages) > 0 {
						target.Message = "Waiting for valid chaincode configuration"
						status.Targets = append(status.Targets, target)
						continue
					}
					status.Workloads.Desired += target.Workload.Desired

					configMap, err := buildChaincodePackageConfigMap(net, chaincode, org)
					if err != nil {
						return statuses, err
					}
					if err := r.ensureConfigMap(ctx, configMap); err != nil {
						return statuses, err
					}

					target.PackageMetadataReady = true
					target.Message = "Package metadata generated"
					status.PackageMetadata.Ready++

					status.Installed.Desired++
					if channelReady {
						if err := r.ensureChaincodeInstallRBAC(ctx, net, org, chaincode, peerName); err != nil {
							return statuses, err
						}

						installed, packageID, chaincodeID, message, err := r.chaincodeInstallReadiness(
							ctx,
							target.Namespace,
							target.PackageIDConfigMapName,
							target.InstallJobName,
						)
						if err != nil {
							return statuses, err
						}

						target.PackageID = packageID
						target.ChaincodeID = chaincodeID
						target.Installed = installed
						target.Message = message

						if installed {
							status.Installed.Ready++
							if err := r.ensureChaincodeWorkload(ctx, net, chaincode, org, peerName, chaincodeID); err != nil {
								return statuses, err
							}
							workload, workloadReady, workloadMessage, err := r.chaincodeWorkloadReadiness(ctx, target.Namespace, target.WorkloadName)
							if err != nil {
								return statuses, err
							}
							target.Workload = workload
							target.WorkloadReady = workloadReady
							status.Workloads.Ready += workload.Ready
							if !workloadReady && workloadMessage != "" {
								target.Message = workloadMessage
							}
						} else {
							if err := r.ensureJob(ctx, buildChaincodeInstallJob(net, chaincode, org, peerName)); err != nil {
								return statuses, err
							}
						}

						if approvalPeers[channelOrg.Name] == peerName {
							status.Approved.Desired++
							if installed {
								target.ApproveJobName = chaincodeApproveJobName(chaincode, org, packageID)

								orderer, ok := chaincodeLifecycleOrderer(net)
								if !ok {
									lifecycleMessage = "Waiting for an orderer before lifecycle approval"
								} else {
									if err := r.ensureChaincodeApproveInputs(ctx, net, channel, org, target.Namespace, orderer); err != nil {
										return statuses, err
									}

									approved, message, err := r.chaincodeApproveReadiness(ctx, target.Namespace, target.ApproveJobName, org)
									if err != nil {
										return statuses, err
									}
									if approved {
										target.Approved = true
										target.Message = "Chaincode definition approved"
										status.Approved.Ready++
									} else {
										if message != "" {
											target.Message = message
										}
										if err := r.ensureJob(ctx, buildChaincodeApproveJob(net, channel, chaincode, org, peerName, packageID, orderer)); err != nil {
											return statuses, err
										}
									}
								}
							}
						}
					}
					status.Targets = append(status.Targets, target)
				}
			}
		}

		status.PackageMetadataReady = status.PackageMetadata.Desired > 0 &&
			status.PackageMetadata.Ready >= status.PackageMetadata.Desired
		status.InstalledReady = status.Installed.Desired > 0 &&
			status.Installed.Ready >= status.Installed.Desired
		status.WorkloadsReady = status.Workloads.Desired > 0 &&
			status.Workloads.Ready >= status.Workloads.Desired
		status.ApprovedReady = status.Approved.Desired > 0 &&
			status.Approved.Ready >= status.Approved.Desired
		if ok && len(messages) == 0 && status.ApprovedReady {
			packageID := firstChaincodePackageID(status.Targets)
			if packageID == "" {
				lifecycleMessage = "Waiting for chaincode package ID before lifecycle commit"
			} else {
				status.CommitJobName = chaincodeCommitJobName(chaincode, packageID)
				committed, message, err := r.reconcileChaincodeCommit(ctx, net, channel, chaincode, packageID)
				if err != nil {
					return statuses, err
				}
				status.Committed = committed
				if message != "" {
					lifecycleMessage = message
				}
			}
		}
		status.Ready = status.Committed && status.WorkloadsReady
		status.Message = chaincodeStatusMessage(status, messages, channelReady, lifecycleMessage)
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func buildChaincodePackageConfigMap(
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
) (*corev1.ConfigMap, error) {
	label := chaincodePackageLabel(chaincode)
	address, err := chaincodeConnectionAddressTemplate(chaincode, org, orgNamespaceName(net, org))
	if err != nil {
		return nil, err
	}

	metadataJSON, err := marshalChaincodeJSON(chaincodePackageMetadata{
		Type:  "ccaas",
		Label: label,
	})
	if err != nil {
		return nil, err
	}

	connectionJSON, err := marshalChaincodeJSON(chaincodeConnection{
		Address:     address,
		DialTimeout: chaincodeDialTimeout(chaincode),
		TLSRequired: false,
	})
	if err != nil {
		return nil, err
	}
	packageFile := label + ".tar.gz"
	packageArchive, err := buildChaincodePackageArchive(metadataJSON, connectionJSON)
	if err != nil {
		return nil, err
	}

	labels := chaincodeLabels(net, org, chaincode.Channel, chaincode.Name)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        chaincodePackageConfigMapName(chaincode, org),
			Namespace:   orgNamespaceName(net, org),
			Labels:      labels,
			Annotations: resourceAnnotations(net, org),
		},
		Data: map[string]string{
			chaincodeMetadataKey:       metadataJSON,
			chaincodeConnectionKey:     connectionJSON,
			chaincodePackageLabelKey:   label,
			chaincodePackageFileKey:    packageFile,
			chaincodeConnectionAddrKey: address,
		},
		BinaryData: map[string][]byte{
			packageFile: packageArchive,
		},
	}, nil
}

func buildChaincodePackageArchive(metadataJSON, connectionJSON string) ([]byte, error) {
	codeArchive, err := gzipTar(map[string][]byte{
		chaincodeConnectionKey: []byte(connectionJSON),
	})
	if err != nil {
		return nil, err
	}

	return gzipTar(map[string][]byte{
		chaincodeMetadataKey: []byte(metadataJSON),
		"code.tar.gz":        codeArchive,
	})
}

func gzipTar(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	gzipWriter.ModTime = time.Unix(0, 0).UTC()

	tarWriter := tar.NewWriter(gzipWriter)
	for _, name := range sortedKeys(files) {
		contents := files[name]
		header := &tar.Header{
			Name:    name,
			Mode:    chaincodePackageArchiveMode,
			Size:    int64(len(contents)),
			ModTime: time.Unix(0, 0).UTC(),
			Format:  tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(contents); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (r *FabricNetworkReconciler) ensureChaincodeWorkload(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
	chaincodeID string,
) error {
	if err := r.ensureService(ctx, buildChaincodeService(net, chaincode, org, peerName)); err != nil {
		return err
	}
	return r.ensureDeployment(ctx, buildChaincodeDeployment(net, chaincode, org, peerName, chaincodeID))
}

func buildChaincodeDeployment(
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
	chaincodeID string,
) *appsv1.Deployment {
	namespace := orgNamespaceName(net, org)
	name := chaincodeServiceName(chaincode, org, peerName)
	replicas := chaincodeReplicas(chaincode)
	labels := chaincodePeerLabels(net, org, chaincode, peerName)
	selector := chaincodeWorkloadSelector(net, org, chaincode, peerName)
	containerPort := chaincodeContainerPort(chaincode)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: resourceAnnotations(net, org),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: resourceAnnotations(net, org),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            chaincodeServerContainer,
							Image:           chaincode.Image,
							ImagePullPolicy: chaincodeImagePullPolicy(chaincode),
							Env:             chaincodeContainerEnv(chaincode, chaincodeID),
							Ports: []corev1.ContainerPort{
								{
									Name:          "chaincode",
									ContainerPort: containerPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							ReadinessProbe: tcpReadinessProbe(containerPort),
							LivenessProbe:  tcpLivenessProbe(containerPort),
							Resources:      chaincodeResourceRequirements(chaincode),
						},
					},
				},
			},
		},
	}
}

func buildChaincodeService(
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) *corev1.Service {
	name := chaincodeServiceName(chaincode, org, peerName)
	servicePort := chaincodeServicePort(chaincode)
	containerPort := chaincodeContainerPort(chaincode)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   orgNamespaceName(net, org),
			Labels:      chaincodePeerLabels(net, org, chaincode, peerName),
			Annotations: resourceAnnotations(net, org),
		},
		Spec: corev1.ServiceSpec{
			Selector: chaincodeWorkloadSelector(net, org, chaincode, peerName),
			Ports: []corev1.ServicePort{
				{
					Name:       "chaincode",
					Port:       servicePort,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt32(containerPort),
				},
			},
		},
	}
}

func (r *FabricNetworkReconciler) chaincodeWorkloadReadiness(
	ctx context.Context,
	namespace string,
	deploymentName string,
) (fabricopsv1alpha1.WorkloadStatus, bool, string, error) {
	workload, err := r.deploymentWorkloadStatus(ctx, namespace, deploymentName)
	if apierrors.IsNotFound(err) {
		return fabricopsv1alpha1.WorkloadStatus{}, false, "Waiting for chaincode workload Deployment", nil
	}
	if err != nil {
		return fabricopsv1alpha1.WorkloadStatus{}, false, "", err
	}
	if workload.Desired > 0 && workloadReady(workload) {
		return workload, true, "", nil
	}

	return workload, false, "Waiting for chaincode workload Deployment", nil
}

func (r *FabricNetworkReconciler) ensureChaincodeInstallRBAC(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	chaincode fabricopsv1alpha1.Chaincode,
	peerName string,
) error {
	namespace := orgNamespaceName(net, org)
	if err := r.ensureServiceAccount(ctx, buildChaincodeInstallServiceAccount(net, org, chaincode, peerName)); err != nil {
		return err
	}
	if err := r.ensureRole(ctx, buildChaincodeInstallRole(net, org, chaincode, peerName, namespace)); err != nil {
		return err
	}
	return r.ensureRoleBinding(ctx, buildChaincodeInstallRoleBinding(net, org, chaincode, peerName, namespace))
}

func buildChaincodeInstallServiceAccount(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	chaincode fabricopsv1alpha1.Chaincode,
	peerName string,
) *corev1.ServiceAccount {
	namespace := orgNamespaceName(net, org)
	name := chaincodeInstallerName(chaincode, org, peerName)

	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      chaincodePeerLabels(net, org, chaincode, peerName),
			Annotations: resourceAnnotations(net, org),
		},
	}
}

func buildChaincodeInstallRole(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	chaincode fabricopsv1alpha1.Chaincode,
	peerName string,
	namespace string,
) *rbacv1.Role {
	name := chaincodeInstallerName(chaincode, org, peerName)

	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      chaincodePeerLabels(net, org, chaincode, peerName),
			Annotations: resourceAnnotations(net, org),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "create", "update", "patch"},
			},
		},
	}
}

func buildChaincodeInstallRoleBinding(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	chaincode fabricopsv1alpha1.Chaincode,
	peerName string,
	namespace string,
) *rbacv1.RoleBinding {
	name := chaincodeInstallerName(chaincode, org, peerName)

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      chaincodePeerLabels(net, org, chaincode, peerName),
			Annotations: resourceAnnotations(net, org),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      name,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     name,
		},
	}
}

func buildChaincodeInstallJob(
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) *batchv1.Job {
	namespace := orgNamespaceName(net, org)
	labels := chaincodePeerLabels(net, org, chaincode, peerName)
	packageFile := chaincodePackageLabel(chaincode) + ".tar.gz"
	backoffLimit := int32(4)
	volumeMounts := []corev1.VolumeMount{
		{Name: chaincodePackageVolumeName, MountPath: chaincodePackageInputDir, ReadOnly: true},
		{Name: chaincodeOutputVolumeName, MountPath: chaincodeOutputDir},
		{Name: chaincodeAdminMSPVolume, MountPath: chaincodeAdminMSPPath, ReadOnly: true},
	}
	volumes := []corev1.Volume{
		{
			Name: chaincodePackageVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: chaincodePackageConfigMapName(chaincode, org),
					},
					Items: []corev1.KeyToPath{
						{Key: chaincodeMetadataKey, Path: chaincodeMetadataKey},
						{Key: chaincodeConnectionKey, Path: chaincodeConnectionKey},
						{Key: chaincodePackageLabelKey, Path: chaincodePackageLabelKey},
						{Key: chaincodePackageFileKey, Path: chaincodePackageFileKey},
						{Key: packageFile, Path: packageFile},
					},
				},
			},
		},
		{
			Name: chaincodeOutputVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: chaincodeAdminMSPVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  identitySecretName(adminIdentityName(org), secretKindMSP),
					Items:       mspSecretItems(net.Spec.Global.TLS),
					DefaultMode: secretVolumeDefaultMode(),
				},
			},
		},
	}

	if net.Spec.Global.TLS {
		volumes = append(volumes, corev1.Volume{
			Name: chaincodeAdminTLSVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  identitySecretName(adminIdentityName(org), secretKindTLS),
					Items:       adminTLSSecretItems(),
					DefaultMode: secretVolumeDefaultMode(),
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      chaincodeAdminTLSVolume,
			MountPath: chaincodeAdminTLSPath,
			ReadOnly:  true,
		})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        chaincodeInstallJobName(chaincode, org, peerName),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: resourceAnnotations(net, org),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: resourceAnnotations(net, org),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: chaincodeInstallerName(chaincode, org, peerName),
					RestartPolicy:      corev1.RestartPolicyNever,
					Volumes:            volumes,
					InitContainers: []corev1.Container{
						{
							Name:         installChaincodeContainer,
							Image:        fabricToolsImage(net.Spec.Global.FabricVersion),
							Command:      []string{"sh", "-ec", installChaincodePackageScript(org, peerName, namespace, net.Spec.Global.TLS)},
							Resources:    componentResourceRequirements(componentPeer),
							VolumeMounts: volumeMounts,
						},
					},
					Containers: []corev1.Container{
						{
							Name:      publishChaincodeInstallContainer,
							Image:     kubectlImage(),
							Command:   []string{"sh", "-ec", publishChaincodeInstallScript()},
							Env:       publishChaincodeInstallEnv(chaincode, org, peerName),
							Resources: componentResourceRequirements(componentKubectl),
							VolumeMounts: []corev1.VolumeMount{
								{Name: chaincodeOutputVolumeName, MountPath: chaincodeOutputDir},
							},
						},
					},
				},
			},
		},
	}
}

func (r *FabricNetworkReconciler) ensureChaincodeApproveInputs(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	org fabricopsv1alpha1.Org,
	namespace string,
	orderer ordererInstance,
) error {
	if !net.Spec.Global.TLS {
		return nil
	}

	source := client.ObjectKey{
		Namespace: orderer.namespace,
		Name:      identitySecretName(orderer.name, secretKindTLS),
	}
	return r.ensureCopiedSecret(
		ctx,
		source,
		namespace,
		channelOrdererTLSSecretName(channel.Name, orderer.name),
		channelLabels(net, org, channel.Name),
		resourceAnnotations(net, org),
	)
}

func buildChaincodeApproveJob(
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
	packageID string,
	orderer ordererInstance,
) *batchv1.Job {
	namespace := orgNamespaceName(net, org)
	labels := chaincodePeerLabels(net, org, chaincode, peerName)
	labels[labelWorkload] = sanitizeName(adminIdentityName(org))
	backoffLimit := int32(4)
	volumeMounts := []corev1.VolumeMount{
		{Name: chaincodeAdminMSPVolume, MountPath: chaincodeAdminMSPPath, ReadOnly: true},
	}
	volumes := []corev1.Volume{
		{
			Name: chaincodeAdminMSPVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  identitySecretName(adminIdentityName(org), secretKindMSP),
					Items:       mspSecretItems(net.Spec.Global.TLS),
					DefaultMode: secretVolumeDefaultMode(),
				},
			},
		},
	}

	if net.Spec.Global.TLS {
		volumes = append(volumes,
			corev1.Volume{
				Name: chaincodeAdminTLSVolume,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  identitySecretName(adminIdentityName(org), secretKindTLS),
						Items:       adminTLSSecretItems(),
						DefaultMode: secretVolumeDefaultMode(),
					},
				},
			},
			corev1.Volume{
				Name: channelOrdererTLSVolumeName(orderer.name),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  channelOrdererTLSSecretName(channel.Name, orderer.name),
						Items:       tlsSecretItems(),
						DefaultMode: secretVolumeDefaultMode(),
					},
				},
			},
		)
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: chaincodeAdminTLSVolume, MountPath: chaincodeAdminTLSPath, ReadOnly: true},
			corev1.VolumeMount{Name: channelOrdererTLSVolumeName(orderer.name), MountPath: chaincodeOrdererTLSPath(orderer.name), ReadOnly: true},
		)
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        chaincodeApproveJobName(chaincode, org, packageID),
			Namespace:   namespace,
			Labels:      labels,
			Annotations: resourceAnnotations(net, org),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: resourceAnnotations(net, org),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: chaincodeInstallerName(chaincode, org, peerName),
					RestartPolicy:      corev1.RestartPolicyNever,
					Volumes:            volumes,
					Containers: []corev1.Container{
						{
							Name:  approveChaincodeContainer,
							Image: fabricToolsImage(net.Spec.Global.FabricVersion),
							Command: []string{"sh", "-ec", approveChaincodeDefinitionScript(
								net,
								channel,
								chaincode,
								org,
								peerName,
								namespace,
								packageID,
								orderer,
							)},
							Resources:    componentResourceRequirements(componentPeer),
							VolumeMounts: volumeMounts,
						},
					},
				},
			},
		},
	}
}

func (r *FabricNetworkReconciler) chaincodeApproveReadiness(
	ctx context.Context,
	namespace string,
	approveJobName string,
	org fabricopsv1alpha1.Org,
) (bool, string, error) {
	var job batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: approveJobName}, &job)
	if apierrors.IsNotFound(err) {
		return false, "Waiting for chaincode approve Job", nil
	}
	if err != nil {
		return false, "", err
	}
	if jobFailed(job) {
		return false, fmt.Sprintf("%s: chaincode approve Job failed", org.Organization.Name), nil
	}
	if jobSucceeded(job) {
		return true, "", nil
	}

	return false, "Waiting for chaincode approve Job", nil
}

func (r *FabricNetworkReconciler) reconcileChaincodeCommit(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
	packageID string,
) (bool, string, error) {
	orderer, ok := chaincodeLifecycleOrderer(net)
	if !ok {
		return false, "Waiting for an orderer before lifecycle commit", nil
	}

	peers := chaincodeLifecyclePeers(net, channel)
	if len(peers) == 0 {
		return false, "Waiting for target peers before lifecycle commit", nil
	}

	submitter := peers[0]
	namespace := submitter.namespace
	jobName := chaincodeCommitJobName(chaincode, packageID)

	committed, message, err := r.chaincodeCommitReadiness(ctx, namespace, jobName)
	if err != nil {
		return false, "", err
	}
	if committed {
		return true, "", nil
	}
	if err := r.ensureChaincodeCommitInputs(ctx, net, channel, chaincode, submitter.org, namespace, orderer, peers); err != nil {
		return false, "", err
	}
	if err := r.ensureServiceAccount(ctx, buildChaincodeCommitServiceAccount(net, chaincode, submitter.org, namespace)); err != nil {
		return false, "", err
	}
	if err := r.ensureJob(ctx, buildChaincodeCommitJob(net, channel, chaincode, packageID, submitter, orderer, peers)); err != nil {
		return false, "", err
	}
	if message != "" {
		return false, message, nil
	}

	return false, "Waiting for chaincode commit Job", nil
}

func (r *FabricNetworkReconciler) ensureChaincodeCommitInputs(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
	hostOrg fabricopsv1alpha1.Org,
	namespace string,
	orderer ordererInstance,
	peers []chaincodeLifecyclePeer,
) error {
	if !net.Spec.Global.TLS {
		return nil
	}

	labels := chaincodeLabels(net, hostOrg, chaincode.Channel, chaincode.Name)
	annotations := resourceAnnotations(net, hostOrg)

	if err := r.ensureCopiedSecret(
		ctx,
		client.ObjectKey{
			Namespace: orderer.namespace,
			Name:      identitySecretName(orderer.name, secretKindTLS),
		},
		namespace,
		channelOrdererTLSSecretName(channel.Name, orderer.name),
		labels,
		annotations,
	); err != nil {
		return err
	}

	for _, peer := range peers {
		if err := r.ensureCopiedSecret(
			ctx,
			client.ObjectKey{
				Namespace: peer.namespace,
				Name:      identitySecretName(peer.peerName, secretKindTLS),
			},
			namespace,
			chaincodePeerTLSSecretName(chaincode, peer.org, peer.peerName),
			labels,
			annotations,
		); err != nil {
			return err
		}
	}

	return nil
}

func buildChaincodeCommitServiceAccount(
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	hostOrg fabricopsv1alpha1.Org,
	namespace string,
) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        chaincodeCommitterName(chaincode),
			Namespace:   namespace,
			Labels:      chaincodeLabels(net, hostOrg, chaincode.Channel, chaincode.Name),
			Annotations: resourceAnnotations(net, hostOrg),
		},
	}
}

func buildChaincodeCommitJob(
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
	packageID string,
	submitter chaincodeLifecyclePeer,
	orderer ordererInstance,
	peers []chaincodeLifecyclePeer,
) *batchv1.Job {
	labels := chaincodeLabels(net, submitter.org, chaincode.Channel, chaincode.Name)
	labels[labelWorkload] = sanitizeName(adminIdentityName(submitter.org))
	backoffLimit := int32(4)
	volumeMounts := []corev1.VolumeMount{
		{Name: chaincodeAdminMSPVolume, MountPath: chaincodeAdminMSPPath, ReadOnly: true},
	}
	volumes := []corev1.Volume{
		{
			Name: chaincodeAdminMSPVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  identitySecretName(adminIdentityName(submitter.org), secretKindMSP),
					Items:       mspSecretItems(net.Spec.Global.TLS),
					DefaultMode: secretVolumeDefaultMode(),
				},
			},
		},
	}

	if net.Spec.Global.TLS {
		volumes = append(volumes,
			corev1.Volume{
				Name: chaincodeAdminTLSVolume,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  identitySecretName(adminIdentityName(submitter.org), secretKindTLS),
						Items:       adminTLSSecretItems(),
						DefaultMode: secretVolumeDefaultMode(),
					},
				},
			},
			corev1.Volume{
				Name: channelOrdererTLSVolumeName(orderer.name),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  channelOrdererTLSSecretName(channel.Name, orderer.name),
						Items:       tlsSecretItems(),
						DefaultMode: secretVolumeDefaultMode(),
					},
				},
			},
		)
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{Name: chaincodeAdminTLSVolume, MountPath: chaincodeAdminTLSPath, ReadOnly: true},
			corev1.VolumeMount{Name: channelOrdererTLSVolumeName(orderer.name), MountPath: chaincodeOrdererTLSPath(orderer.name), ReadOnly: true},
		)

		for _, peer := range peers {
			volumeName := chaincodePeerTLSVolumeName(peer.org, peer.peerName)
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  chaincodePeerTLSSecretName(chaincode, peer.org, peer.peerName),
						Items:       tlsSecretItems(),
						DefaultMode: secretVolumeDefaultMode(),
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: chaincodePeerTLSPath(peer.org, peer.peerName),
				ReadOnly:  true,
			})
		}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        chaincodeCommitJobName(chaincode, packageID),
			Namespace:   submitter.namespace,
			Labels:      labels,
			Annotations: resourceAnnotations(net, submitter.org),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: resourceAnnotations(net, submitter.org),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: chaincodeCommitterName(chaincode),
					RestartPolicy:      corev1.RestartPolicyNever,
					Volumes:            volumes,
					Containers: []corev1.Container{
						{
							Name:  commitChaincodeContainer,
							Image: fabricToolsImage(net.Spec.Global.FabricVersion),
							Command: []string{"sh", "-ec", commitChaincodeDefinitionScript(
								net,
								channel,
								chaincode,
								submitter,
								orderer,
								peers,
							)},
							Resources:    componentResourceRequirements(componentPeer),
							VolumeMounts: volumeMounts,
						},
					},
				},
			},
		},
	}
}

func (r *FabricNetworkReconciler) chaincodeCommitReadiness(
	ctx context.Context,
	namespace string,
	commitJobName string,
) (bool, string, error) {
	var job batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: commitJobName}, &job)
	if apierrors.IsNotFound(err) {
		return false, "Waiting for chaincode commit Job", nil
	}
	if err != nil {
		return false, "", err
	}
	if jobFailed(job) {
		return false, "Chaincode commit Job failed", nil
	}
	if jobSucceeded(job) {
		return true, "", nil
	}

	return false, "Waiting for chaincode commit Job", nil
}

func (r *FabricNetworkReconciler) chaincodeInstallReadiness(
	ctx context.Context,
	namespace string,
	packageIDConfigMapName string,
	installJobName string,
) (bool, string, string, string, error) {
	var result corev1.ConfigMap
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: packageIDConfigMapName}, &result)
	if err == nil {
		packageID := strings.TrimSpace(result.Data[chaincodePackageIDKey])
		chaincodeID := strings.TrimSpace(result.Data[chaincodeChaincodeIDKey])
		if chaincodeID == "" {
			chaincodeID = packageID
		}
		if packageID != "" {
			return true, packageID, chaincodeID, "Chaincode package installed", nil
		}
		return false, packageID, chaincodeID, "Waiting for package ID ConfigMap", nil
	}
	if !apierrors.IsNotFound(err) {
		return false, "", "", "", err
	}

	var job batchv1.Job
	err = r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: installJobName}, &job)
	if apierrors.IsNotFound(err) {
		return false, "", "", "Waiting for chaincode install Job", nil
	}
	if err != nil {
		return false, "", "", "", err
	}
	if jobFailed(job) {
		return false, "", "", "Chaincode install Job failed", nil
	}
	if jobSucceeded(job) {
		return false, "", "", "Waiting for package ID ConfigMap", nil
	}

	return false, "", "", "Waiting for chaincode install Job", nil
}

func marshalChaincodeJSON(value any) (string, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

func installChaincodePackageScript(
	org fabricopsv1alpha1.Org,
	peerName string,
	namespace string,
	tlsEnabled bool,
) string {
	tlsEnv := "export CORE_PEER_TLS_ENABLED=false"
	if tlsEnabled {
		tlsEnv = fmt.Sprintf(`ADMIN_TLS_DIR=%q
export CORE_PEER_TLS_ENABLED=true
export CORE_PEER_TLS_CERT_FILE="$ADMIN_TLS_DIR/client.crt"
export CORE_PEER_TLS_KEY_FILE="$ADMIN_TLS_DIR/client.key"
export CORE_PEER_TLS_ROOTCERT_FILE="$ADMIN_TLS_DIR/ca.crt"`, chaincodeAdminTLSPath)
	}

	return fmt.Sprintf(`set -eu

PACKAGE_INPUT_DIR=%q
OUTPUT_DIR=%q
PACKAGE_LABEL="$(cat "$PACKAGE_INPUT_DIR/%s")"
PACKAGE_ARCHIVE="$(cat "$PACKAGE_INPUT_DIR/%s")"
PACKAGE_FILE="$PACKAGE_INPUT_DIR/$PACKAGE_ARCHIVE"
QUERY_FILE="$OUTPUT_DIR/%s"
PACKAGE_ID_FILE="$OUTPUT_DIR/%s"
CHAINCODE_ID_FILE="$OUTPUT_DIR/%s"
PACKAGE_HASH_FILE="$OUTPUT_DIR/%s"

export CORE_PEER_LOCALMSPID=%q
export CORE_PEER_ADDRESS=%q
export CORE_PEER_MSPCONFIGPATH=%q
%s

mkdir -p "$OUTPUT_DIR"
test -f "$PACKAGE_FILE"

query_installed() {
  peer lifecycle chaincode queryinstalled --output json > "$QUERY_FILE"
}

extract_package_id() {
  PACKAGE_ID="$(jq -r --arg package_label "$PACKAGE_LABEL" '.installed_chaincodes[]? | select(.label == $package_label) | .package_id' "$QUERY_FILE" | tail -n 1)"
  if [ -z "$PACKAGE_ID" ] || [ "$PACKAGE_ID" = "null" ]; then
    echo "Package label $PACKAGE_LABEL was not found in queryinstalled output" >&2
    return 1
  fi

  PACKAGE_HASH="${PACKAGE_ID#*:}"
  printf '%%s\n' "$PACKAGE_ID" > "$PACKAGE_ID_FILE"
  printf '%%s\n' "$PACKAGE_ID" > "$CHAINCODE_ID_FILE"
  printf '%%s\n' "$PACKAGE_HASH" > "$PACKAGE_HASH_FILE"
}

if query_installed 2>/tmp/fabricops-chaincode-queryinstalled.err && extract_package_id; then
  echo "Chaincode package $PACKAGE_LABEL is already installed"
  exit 0
fi

peer lifecycle chaincode install "$PACKAGE_FILE"
query_installed
extract_package_id
`, chaincodePackageInputDir,
		chaincodeOutputDir,
		chaincodePackageLabelKey,
		chaincodePackageFileKey,
		chaincodeQueryInstalledFile,
		chaincodePackageIDFile,
		chaincodeChaincodeIDFile,
		chaincodePackageHashFile,
		org.Organization.MSPName,
		serviceDNS(peerName, namespace, peerPort),
		chaincodeAdminMSPPath,
		tlsEnv,
	)
}

func approveChaincodeDefinitionScript(
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
	namespace string,
	packageID string,
	orderer ordererInstance,
) string {
	tlsEnv := "export CORE_PEER_TLS_ENABLED=false"
	tlsArgs := ""
	if net.Spec.Global.TLS {
		tlsEnv = fmt.Sprintf(`ADMIN_TLS_DIR=%q
export CORE_PEER_TLS_ENABLED=true
export CORE_PEER_TLS_CERT_FILE="$ADMIN_TLS_DIR/client.crt"
export CORE_PEER_TLS_KEY_FILE="$ADMIN_TLS_DIR/client.key"
export CORE_PEER_TLS_ROOTCERT_FILE="$ADMIN_TLS_DIR/ca.crt"`, chaincodeAdminTLSPath)
		tlsArgs = fmt.Sprintf(`set -- "$@" --tls --cafile %q`, chaincodeOrdererTLSPath(orderer.name)+"/ca.crt")
	}

	return fmt.Sprintf(`set -eu

CHANNEL_ID=%q
CHAINCODE_NAME=%q
CHAINCODE_VERSION=%q
PACKAGE_ID=%q
SEQUENCE=%d
ORDERER_ADDRESS=%q
ENDORSEMENT_POLICY=%q
INIT_REQUIRED=%q
QUERY_APPROVED_FILE=/tmp/fabricops-chaincode-approved.json

export CORE_PEER_LOCALMSPID=%q
export CORE_PEER_ADDRESS=%q
export CORE_PEER_MSPCONFIGPATH=%q
%s

if peer lifecycle chaincode queryapproved \
  --channelID "$CHANNEL_ID" \
  --name "$CHAINCODE_NAME" \
  --output json > "$QUERY_APPROVED_FILE" 2>/tmp/fabricops-queryapproved.err; then
  if jq -e --argjson sequence "$SEQUENCE" '(.sequence | tonumber) == $sequence' "$QUERY_APPROVED_FILE" >/dev/null; then
    echo "Chaincode $CHAINCODE_NAME sequence $SEQUENCE is already approved for $CORE_PEER_LOCALMSPID"
    exit 0
  fi
fi

set -- peer lifecycle chaincode approveformyorg \
  -o "$ORDERER_ADDRESS" \
  --channelID "$CHANNEL_ID" \
  --name "$CHAINCODE_NAME" \
  --version "$CHAINCODE_VERSION" \
  --package-id "$PACKAGE_ID" \
  --sequence "$SEQUENCE"

%s

if [ -n "$ENDORSEMENT_POLICY" ]; then
  set -- "$@" --signature-policy "$ENDORSEMENT_POLICY"
fi
if [ "$INIT_REQUIRED" = "true" ]; then
  set -- "$@" --init-required
fi

"$@"
`, channel.Name,
		chaincode.Name,
		chaincode.Version,
		packageID,
		chaincodeSequence(chaincode),
		ordererClientAddress(orderer),
		chaincodeEndorsementPolicy(net, channel, chaincode),
		boolString(chaincode.InitRequired),
		org.Organization.MSPName,
		serviceDNS(peerName, namespace, peerPort),
		chaincodeAdminMSPPath,
		tlsEnv,
		tlsArgs,
	)
}

func commitChaincodeDefinitionScript(
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
	submitter chaincodeLifecyclePeer,
	orderer ordererInstance,
	peers []chaincodeLifecyclePeer,
) string {
	tlsEnv := "export CORE_PEER_TLS_ENABLED=false"
	tlsArgs := ""
	peerArgs := ""
	if net.Spec.Global.TLS {
		tlsEnv = fmt.Sprintf(`ADMIN_TLS_DIR=%q
export CORE_PEER_TLS_ENABLED=true
export CORE_PEER_TLS_CERT_FILE="$ADMIN_TLS_DIR/client.crt"
export CORE_PEER_TLS_KEY_FILE="$ADMIN_TLS_DIR/client.key"
export CORE_PEER_TLS_ROOTCERT_FILE="$ADMIN_TLS_DIR/ca.crt"`, chaincodeAdminTLSPath)
		tlsArgs = fmt.Sprintf(`set -- "$@" --tls --cafile %q`, chaincodeOrdererTLSPath(orderer.name)+"/ca.crt")

		var peerBuilder strings.Builder
		for _, peer := range peers {
			fmt.Fprintf(&peerBuilder,
				"set -- \"$@\" --peerAddresses %q --tlsRootCertFiles %q\n",
				serviceDNS(peer.peerName, peer.namespace, peerPort),
				chaincodePeerTLSPath(peer.org, peer.peerName)+"/ca.crt",
			)
		}
		peerArgs = peerBuilder.String()
	}

	return fmt.Sprintf(`set -eu

CHANNEL_ID=%q
CHAINCODE_NAME=%q
CHAINCODE_VERSION=%q
SEQUENCE=%d
ORDERER_ADDRESS=%q
ENDORSEMENT_POLICY=%q
INIT_REQUIRED=%q
QUERY_COMMITTED_FILE=/tmp/fabricops-chaincode-committed.json

export CORE_PEER_LOCALMSPID=%q
export CORE_PEER_ADDRESS=%q
export CORE_PEER_MSPCONFIGPATH=%q
%s

if peer lifecycle chaincode querycommitted \
  --channelID "$CHANNEL_ID" \
  --name "$CHAINCODE_NAME" \
  --output json > "$QUERY_COMMITTED_FILE" 2>/tmp/fabricops-querycommitted.err; then
  if jq -e --argjson sequence "$SEQUENCE" '(.sequence | tonumber) == $sequence' "$QUERY_COMMITTED_FILE" >/dev/null; then
    echo "Chaincode $CHAINCODE_NAME sequence $SEQUENCE is already committed on $CHANNEL_ID"
    exit 0
  fi
fi

set -- peer lifecycle chaincode commit \
  -o "$ORDERER_ADDRESS" \
  --channelID "$CHANNEL_ID" \
  --name "$CHAINCODE_NAME" \
  --version "$CHAINCODE_VERSION" \
  --sequence "$SEQUENCE"

%s
%s

if [ -n "$ENDORSEMENT_POLICY" ]; then
  set -- "$@" --signature-policy "$ENDORSEMENT_POLICY"
fi
if [ "$INIT_REQUIRED" = "true" ]; then
  set -- "$@" --init-required
fi

"$@"

peer lifecycle chaincode querycommitted \
  --channelID "$CHANNEL_ID" \
  --name "$CHAINCODE_NAME" \
  --output json > "$QUERY_COMMITTED_FILE"
`, channel.Name,
		chaincode.Name,
		chaincode.Version,
		chaincodeSequence(chaincode),
		ordererClientAddress(orderer),
		chaincodeEndorsementPolicy(net, channel, chaincode),
		boolString(chaincode.InitRequired),
		submitter.org.Organization.MSPName,
		serviceDNS(submitter.peerName, submitter.namespace, peerPort),
		chaincodeAdminMSPPath,
		tlsEnv,
		tlsArgs,
		peerArgs,
	)
}

func publishChaincodeInstallScript() string {
	return `set -eu

kubectl -n "$POD_NAMESPACE" create configmap "$FABRICOPS_CHAINCODE_PACKAGE_ID_CONFIGMAP" \
  --from-file="` + chaincodePackageIDKey + `=` + chaincodeOutputDir + `/` + chaincodePackageIDFile + `" \
  --from-file="` + chaincodeChaincodeIDKey + `=` + chaincodeOutputDir + `/` + chaincodeChaincodeIDFile + `" \
  --from-file="` + chaincodePackageHashKey + `=` + chaincodeOutputDir + `/` + chaincodePackageHashFile + `" \
  --from-file="` + chaincodeQueryInstalledKey + `=` + chaincodeOutputDir + `/` + chaincodeQueryInstalledFile + `" \
  --dry-run=client -o yaml | kubectl -n "$POD_NAMESPACE" apply -f -

kubectl -n "$POD_NAMESPACE" label configmap "$FABRICOPS_CHAINCODE_PACKAGE_ID_CONFIGMAP" \
  fabricops.io/component=chaincode \
  fabricops.io/channel="$FABRICOPS_CHAINCODE_CHANNEL" \
  fabricops.io/chaincode="$FABRICOPS_CHAINCODE_NAME" \
  fabricops.io/workload="$FABRICOPS_CHAINCODE_PEER" \
  app.kubernetes.io/component=chaincode \
  --overwrite
`
}

func publishChaincodeInstallEnv(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: envChaincodePackageIDConfigMap, Value: chaincodePackageIDConfigMapName(chaincode, org, peerName)},
		{Name: envChaincodeChannel, Value: sanitizeName(chaincode.Channel)},
		{Name: envChaincodeName, Value: sanitizeName(chaincode.Name)},
		{Name: envChaincodePeer, Value: sanitizeName(peerName)},
		{
			Name: envPodNamespace,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}
}

func chaincodeStatusMessage(
	status fabricopsv1alpha1.ChaincodeStatus,
	messages []string,
	channelReady bool,
	lifecycleMessage string,
) string {
	if len(messages) > 0 {
		return strings.Join(messages, "; ")
	}
	if lifecycleMessage != "" {
		return lifecycleMessage
	}
	if status.PackageMetadata.Desired == 0 {
		return "No target peers found"
	}
	if status.PackageMetadataReady && !channelReady {
		return "Package metadata generated; waiting for channel bootstrap before lifecycle install"
	}
	if status.Ready {
		return "Chaincode committed and workload ready"
	}
	if status.Committed && !status.WorkloadsReady {
		return "Chaincode committed; waiting for chaincode workload Deployment"
	}
	if status.ApprovedReady {
		return "Chaincode approved; waiting for lifecycle commit Job"
	}
	if status.InstalledReady && !status.WorkloadsReady {
		return "Chaincode package installed; waiting for chaincode workloads and lifecycle approval Jobs"
	}
	if status.InstalledReady {
		return "Chaincode package installed; waiting for lifecycle approval Jobs"
	}
	if status.PackageMetadataReady && channelReady {
		return "Package metadata generated; waiting for chaincode install Jobs"
	}
	return "Waiting for package metadata generation"
}

func chaincodeStatusesEqual(a, b []fabricopsv1alpha1.ChaincodeStatus) bool {
	return reflect.DeepEqual(a, b)
}

func allChaincodesReady(statuses []fabricopsv1alpha1.ChaincodeStatus) bool {
	for _, status := range statuses {
		if !status.Ready {
			return false
		}
	}

	return true
}

func channelsByName(net *fabricopsv1alpha1.FabricNetwork) map[string]fabricopsv1alpha1.Channel {
	channels := map[string]fabricopsv1alpha1.Channel{}
	for _, channel := range net.Spec.Channels {
		channels[channel.Name] = channel
	}
	return channels
}

func channelReadyForChaincode(statuses []fabricopsv1alpha1.ChannelStatus, channelName string) bool {
	for _, status := range statuses {
		if status.Name == channelName {
			return status.Ready
		}
	}
	return false
}

func chaincodeApprovalPeers(channel fabricopsv1alpha1.Channel) map[string]string {
	peers := map[string]string{}
	for _, org := range channel.Orgs {
		if len(org.Peers) == 0 {
			continue
		}
		peers[org.Name] = org.Peers[0]
	}

	return peers
}

func chaincodeLifecycleOrderer(net *fabricopsv1alpha1.FabricNetwork) (ordererInstance, bool) {
	orderers := desiredOrdererInstances(net)
	if len(orderers) == 0 {
		return ordererInstance{}, false
	}

	return orderers[0], true
}

func chaincodeLifecyclePeers(
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
) []chaincodeLifecyclePeer {
	orgs := orgsByName(net)
	peers := []chaincodeLifecyclePeer{}

	for _, channelOrg := range channel.Orgs {
		org, ok := orgs[channelOrg.Name]
		if !ok {
			continue
		}

		namespace := orgNamespaceName(net, org)
		for _, peerName := range channelOrg.Peers {
			if !peerDeclared(org, peerName) {
				continue
			}
			peers = append(peers, chaincodeLifecyclePeer{
				org:       org,
				peerName:  peerName,
				namespace: namespace,
			})
		}
	}

	return peers
}

func firstChaincodePackageID(targets []fabricopsv1alpha1.ChaincodeTargetStatus) string {
	for _, target := range targets {
		if strings.TrimSpace(target.PackageID) != "" {
			return target.PackageID
		}
	}

	return ""
}

func chaincodeEndorsementPolicy(
	net *fabricopsv1alpha1.FabricNetwork,
	channel fabricopsv1alpha1.Channel,
	chaincode fabricopsv1alpha1.Chaincode,
) string {
	if strings.TrimSpace(chaincode.EndorsementPolicy) != "" {
		return chaincode.EndorsementPolicy
	}

	orgs := channelPeerOrganizations(net, channel)
	if len(orgs) == 0 {
		return ""
	}

	identities := make([]string, 0, len(orgs))
	for _, org := range orgs {
		identities = append(identities, fmt.Sprintf("'%s.member'", org.Organization.MSPName))
	}

	return fmt.Sprintf("OR(%s)", strings.Join(identities, ","))
}

func peerDeclared(org fabricopsv1alpha1.Org, peerName string) bool {
	if org.Peer == nil {
		return false
	}

	for i := 0; i < org.Peer.Instances; i++ {
		if sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, i)) == peerName {
			return true
		}
	}
	return false
}

func chaincodePackageLabel(chaincode fabricopsv1alpha1.Chaincode) string {
	if strings.TrimSpace(chaincode.PackageLabel) != "" {
		return chaincode.PackageLabel
	}
	return fmt.Sprintf("%s_%s_%s", chaincode.Channel, chaincode.Name, chaincode.Version)
}

func chaincodeSequence(chaincode fabricopsv1alpha1.Chaincode) int32 {
	if chaincode.Sequence > 0 {
		return chaincode.Sequence
	}
	return 1
}

func chaincodeDialTimeout(chaincode fabricopsv1alpha1.Chaincode) string {
	if chaincode.CCAAS != nil && strings.TrimSpace(chaincode.CCAAS.DialTimeout) != "" {
		return chaincode.CCAAS.DialTimeout
	}
	return "10s"
}

func chaincodeReplicas(chaincode fabricopsv1alpha1.Chaincode) int32 {
	if chaincode.CCAAS != nil && chaincode.CCAAS.Replicas > 0 {
		return chaincode.CCAAS.Replicas
	}
	return 1
}

func chaincodeContainerPort(chaincode fabricopsv1alpha1.Chaincode) int32 {
	if chaincode.CCAAS != nil && chaincode.CCAAS.ContainerPort > 0 {
		return chaincode.CCAAS.ContainerPort
	}
	return peerChaincodePort
}

func chaincodeServicePort(chaincode fabricopsv1alpha1.Chaincode) int32 {
	if chaincode.CCAAS != nil && chaincode.CCAAS.ServicePort > 0 {
		return chaincode.CCAAS.ServicePort
	}
	return peerChaincodePort
}

func chaincodeImagePullPolicy(chaincode fabricopsv1alpha1.Chaincode) corev1.PullPolicy {
	if chaincode.CCAAS != nil && chaincode.CCAAS.ImagePullPolicy != "" {
		return chaincode.CCAAS.ImagePullPolicy
	}
	return corev1.PullIfNotPresent
}

func chaincodeContainerEnv(chaincode fabricopsv1alpha1.Chaincode, chaincodeID string) []corev1.EnvVar {
	required := map[string]string{
		envCCAASChaincodeID:            chaincodeID,
		envCCAASCoreChaincodeIDName:    chaincodeID,
		envCCAASChaincodeServerAddress: fmt.Sprintf("0.0.0.0:%d", chaincodeContainerPort(chaincode)),
		envCCAASCoreChaincodeAddress:   fmt.Sprintf("0.0.0.0:%d", chaincodeContainerPort(chaincode)),
	}
	protected := map[string]struct{}{}
	env := []corev1.EnvVar{}

	for name := range required {
		protected[name] = struct{}{}
	}
	if chaincode.CCAAS != nil {
		for _, item := range chaincode.CCAAS.Env {
			if _, ok := protected[item.Name]; ok {
				continue
			}
			env = append(env, item)
		}
	}
	for _, name := range []string{
		envCCAASChaincodeID,
		envCCAASCoreChaincodeIDName,
		envCCAASChaincodeServerAddress,
		envCCAASCoreChaincodeAddress,
	} {
		env = append(env, corev1.EnvVar{Name: name, Value: required[name]})
	}

	return env
}

func chaincodeResourceRequirements(chaincode fabricopsv1alpha1.Chaincode) corev1.ResourceRequirements {
	if chaincode.CCAAS != nil && chaincode.CCAAS.Resources != nil {
		return *chaincode.CCAAS.Resources.DeepCopy()
	}
	return componentResourceRequirements(componentPeer)
}

func chaincodeServiceName(chaincode fabricopsv1alpha1.Chaincode, org fabricopsv1alpha1.Org, peerName string) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-%s-ccaas", chaincode.Channel, chaincode.Name, org.Organization.Name, peerName))
}

func chaincodeConnectionAddress(serviceName string, namespace string, chaincode fabricopsv1alpha1.Chaincode) string {
	return serviceDNS(serviceName, namespace, chaincodeServicePort(chaincode))
}

func chaincodeConnectionAddressTemplate(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	namespace string,
) (string, error) {
	serviceName := chaincodeServiceName(chaincode, org, chaincodePeerHostnamePlaceholder)
	if !strings.Contains(serviceName, chaincodePeerHostnamePlaceholder) {
		return "", fmt.Errorf(
			"chaincode service name for %q on channel %q in org %q is too long to template peer-specific CCaaS addresses",
			chaincode.Name,
			chaincode.Channel,
			org.Organization.Name,
		)
	}

	serviceName = strings.ReplaceAll(serviceName, chaincodePeerHostnamePlaceholder, chaincodePeerHostnameTemplate)
	return chaincodeConnectionAddress(serviceName, namespace, chaincode), nil
}

func chaincodePackageConfigMapName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-package", chaincode.Channel, chaincode.Name, org.Organization.Name))
}

func chaincodePackageIDConfigMapName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-package-id", chaincodePackageLabel(chaincode), org.Organization.Name, peerName))
}

func chaincodeApproveJobName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	packageID string,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-approve", chaincodePackageLabel(chaincode), chaincodePackageHash(packageID), org.Organization.Name))
}

func chaincodeCommitJobName(chaincode fabricopsv1alpha1.Chaincode, packageID string) string {
	return sanitizeName(fmt.Sprintf("%s-%s-commit", chaincodePackageLabel(chaincode), chaincodePackageHash(packageID)))
}

func chaincodeInstallJobName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-install", chaincodePackageLabel(chaincode), org.Organization.Name, peerName))
}

func chaincodeInstallerName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-installer", chaincodePackageLabel(chaincode), org.Organization.Name, peerName))
}

func chaincodeCommitterName(chaincode fabricopsv1alpha1.Chaincode) string {
	return sanitizeName(fmt.Sprintf("%s-committer", chaincodePackageLabel(chaincode)))
}

func chaincodeOrdererTLSPath(ordererName string) string {
	return chaincodeWorkDir + "/crypto/orderers/" + sanitizeName(ordererName) + "/tls"
}

func chaincodePeerTLSVolumeName(org fabricopsv1alpha1.Org, peerName string) string {
	return sanitizeName("peer-tls-" + org.Organization.Name + "-" + peerName)
}

func chaincodePeerTLSSecretName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-tls", chaincodePackageLabel(chaincode), org.Organization.Name, peerName))
}

func chaincodePeerTLSPath(org fabricopsv1alpha1.Org, peerName string) string {
	return chaincodeWorkDir + "/crypto/peers/" + sanitizeName(org.Organization.Name) + "/" + sanitizeName(peerName) + "/tls"
}

func chaincodePackageHash(packageID string) string {
	parts := strings.SplitN(packageID, ":", 2)
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		return parts[1]
	}
	return "pending"
}

func chaincodeLabels(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	channelName string,
	chaincodeName string,
) map[string]string {
	return mergeMap(orgLabels(net, org, componentChaincode), map[string]string{
		labelChannel:   sanitizeName(channelName),
		labelChaincode: sanitizeName(chaincodeName),
	})
}

func chaincodePeerLabels(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	chaincode fabricopsv1alpha1.Chaincode,
	peerName string,
) map[string]string {
	return mergeMap(chaincodeLabels(net, org, chaincode.Channel, chaincode.Name), map[string]string{
		labelWorkload: sanitizeName(peerName),
	})
}

func chaincodeWorkloadSelector(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	chaincode fabricopsv1alpha1.Chaincode,
	peerName string,
) map[string]string {
	return map[string]string{
		labelFabricNetwork:          sanitizeName(net.Name),
		labelFabricNetworkNamespace: sanitizeName(net.Namespace),
		labelOrg:                    sanitizeName(org.Organization.Name),
		labelComponent:              componentChaincode,
		labelChannel:                sanitizeName(chaincode.Channel),
		labelChaincode:              sanitizeName(chaincode.Name),
		labelWorkload:               sanitizeName(peerName),
	}
}
