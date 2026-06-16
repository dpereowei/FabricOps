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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FabricNetworkReconciler reconciles a FabricNetwork object
type FabricNetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *FabricNetworkReconciler) ensureNamespace(ctx context.Context, name string) error {
	var ns corev1.Namespace

	err := r.Get(ctx, client.ObjectKey{Name: name}, &ns)
	if err == nil {
		return nil
	}

	ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	return r.Create(ctx, &ns)
}

func (r *FabricNetworkReconciler) reconcileOrgs(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	namespace string,
) error {

	log := logf.FromContext(ctx)

	for _, org := range net.Spec.Orgs {
		log.Info(
			"Reconciling org",
			"name", org.Organization.Name,
			"domain", org.Organization.Domain,
		)

		// create CA + peer
	}

	return nil
}

func (r *FabricNetworkReconciler) updateStatusIfChanged(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	newPhase fabricopsv1alpha1.Phase,
	newMessage string,
) error {
	if net.Status.Phase == newPhase && net.Status.Message == newMessage {
		return nil
	}

	base := net.DeepCopy()
	base.Status.Phase = newPhase
	base.Status.Message = newMessage

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

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FabricNetwork object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
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

	namespaceName := "fabric-network-" + network.Name

	// Check if the namespace already exists
	log.Info(
		"Checking namespace",
		"namespaceName", namespaceName,
	)

	if err := r.ensureNamespace(ctx, namespaceName); err != nil {
		log.Error(err, "failed to ensure namespace exists")

		_ = r.updateStatusIfChanged(
			ctx,
			&network,
			fabricopsv1alpha1.PhaseFailed,
			"Failes to ensure namespace: "+err.Error(),
		)

		return ctrl.Result{}, err
	}

	if network.Status.Phase != fabricopsv1alpha1.PhaseReady ||
		network.Status.Message != "Namespace ready" {

		_ = r.updateStatusIfChanged(
			ctx,
			&network,
			fabricopsv1alpha1.PhaseReady,
			"Namespace ready",
		)
	}

	if err := r.reconcileOrgs(ctx, &network, namespaceName); err != nil {
		log.Error(err, "failed to reconcile orgs")

		_ = r.updateStatusIfChanged(
			ctx,
			&network,
			fabricopsv1alpha1.PhaseFailed,
			"failed to reconcile orgs: "+err.Error(),
		)

		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FabricNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fabricopsv1alpha1.FabricNetwork{}).
		Named("fabricnetwork").
		Complete(r)
}
