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
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

// FabricNetworkReconciler reconciles a FabricNetwork object
type FabricNetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func orgNamespaceName(net *fabricopsv1alpha1.FabricNetwork, org fabricopsv1alpha1.Org) string {
	return sanitizeName("fo-" + networkNamespaceSlug(net) + "-" + org.Organization.Name)
}

func buildOrgNamespace(net *fabricopsv1alpha1.FabricNetwork, org fabricopsv1alpha1.Org) *corev1.Namespace {
	name := orgNamespaceName(net, org)

	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labelFabricNetwork:             sanitizeName(net.Name),
				labelFabricNetworkNamespace:    sanitizeName(net.Namespace),
				labelOrg:                       sanitizeName(org.Organization.Name),
				"app.kubernetes.io/managed-by": "fabricops",
			},
		},
	}
}

func (r *FabricNetworkReconciler) ensureNamespace(ctx context.Context, desired *corev1.Namespace) error {
	var existing corev1.Namespace

	err := r.Get(ctx, client.ObjectKey{Name: desired.Name}, &existing)
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

		if changed {
			log := logf.FromContext(ctx)
			log.Info("Updating Namespace labels", "namespace", desired.Name)
			return r.Update(ctx, &existing)
		}

		return nil
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	log := logf.FromContext(ctx)
	log.Info("Creating Namespace", "namespace", desired.Name)
	return r.Create(ctx, desired)
}

func (r *FabricNetworkReconciler) reconcileOrg(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
) (fabricopsv1alpha1.OrgStatus, error) {
	log := logf.FromContext(ctx)
	namespace := orgNamespaceName(net, org)

	log.Info("Reconciling org",
		"name", org.Organization.Name,
		"domain", org.Organization.Domain,
		"namespace", namespace,
	)

	status := fabricopsv1alpha1.OrgStatus{
		Name:      org.Organization.Name,
		Namespace: namespace,
	}

	if err := r.ensureNamespace(ctx, buildOrgNamespace(net, org)); err != nil {
		return status, err
	}

	caReady, err := r.reconcileCA(ctx, net, org, namespace)
	if err != nil {
		return status, err
	}
	status.CAReady = caReady

	if err := r.reconcileOrderers(ctx, net, org, namespace); err != nil {
		return status, err
	}

	if err := r.reconcilePeers(ctx, net, org, namespace); err != nil {
		return status, err
	}

	return status, nil
}

func (r *FabricNetworkReconciler) reconcileOrgs(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
) ([]fabricopsv1alpha1.OrgStatus, error) {
	orgStatuses := make([]fabricopsv1alpha1.OrgStatus, 0, len(net.Spec.Orgs))

	for _, org := range net.Spec.Orgs {
		status, err := r.reconcileOrg(ctx, net, org)
		if err != nil {
			return orgStatuses, err
		}
		orgStatuses = append(orgStatuses, status)
	}

	return orgStatuses, nil
}

func orgStatusesEqual(a, b []fabricopsv1alpha1.OrgStatus) bool {
	return reflect.DeepEqual(a, b)
}

func allCAsReady(statuses []fabricopsv1alpha1.OrgStatus) bool {
	if len(statuses) == 0 {
		return false
	}

	for _, status := range statuses {
		if !status.CAReady {
			return false
		}
	}

	return true
}

func (r *FabricNetworkReconciler) updateStatus(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	newPhase fabricopsv1alpha1.Phase,
	newMessage string,
	orgStatus []fabricopsv1alpha1.OrgStatus,
) error {
	if net.Status.Phase == newPhase &&
		net.Status.Message == newMessage &&
		orgStatusesEqual(net.Status.OrgStatus, orgStatus) {
		return nil
	}

	base := net.DeepCopy()
	base.Status.Phase = newPhase
	base.Status.Message = newMessage
	base.Status.OrgStatus = orgStatus

	log := logf.FromContext(ctx)

	log.Info(
		"Updating status",
		"oldPhase", net.Status.Phase,
		"newPhase", newPhase,
		"oldMessage", net.Status.Message,
		"newMessage", newMessage,
	)

	return r.Status().Patch(ctx, base, client.MergeFrom(net))
}

// +kubebuilder:rbac:groups=fabricops.my.domain,resources=fabricnetworks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fabricops.my.domain,resources=fabricnetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fabricops.my.domain,resources=fabricnetworks/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *FabricNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	log.Info(
		"Reconciling FabricNetwork",
		"name", req.Name,
		"namespace", req.Namespace,
	)

	var network fabricopsv1alpha1.FabricNetwork

	if err := r.Get(ctx, req.NamespacedName, &network); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	orgStatuses, err := r.reconcileOrgs(ctx, &network)
	if err != nil {
		log.Error(err, "Failed to reconcile orgs")

		_ = r.updateStatus(
			ctx,
			&network,
			fabricopsv1alpha1.PhaseFailed,
			"Failed to reconcile orgs: "+err.Error(),
			orgStatuses,
		)

		return ctrl.Result{}, err
	}

	if allCAsReady(orgStatuses) {
		if err := r.updateStatus(
			ctx,
			&network,
			fabricopsv1alpha1.PhaseReady,
			"All orgs reconciled",
			orgStatuses,
		); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if err := r.updateStatus(
		ctx,
		&network,
		fabricopsv1alpha1.PhaseCreating,
		"Waiting for org CAs to become ready",
		orgStatuses,
	); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FabricNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fabricopsv1alpha1.FabricNetwork{}).
		Named("fabricnetwork").
		Complete(r)
}
