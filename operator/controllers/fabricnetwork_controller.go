package controllers

import (
	"context"

	fabricv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type FabricNetworkReconciler struct {
	client.Client
}

func (r *FabricNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var network fabricv1alpha1.FabricNetwork
	if err := r.Get(ctx, req.NamespacedName, &network); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	namespaceName := network.Name + "-fabric"

	var ns corev1.Namespace
	err := r.Get(ctx, types.NamespacedName{Name: namespaceName}, &ns)
	if err == nil {
		logger.Info("namespace already exists", "namespace", namespaceName)
		return ctrl.Result{}, nil
	}

	ns = corev1.Namespace{}
	ns.Name = namespaceName

	if err := r.Create(ctx, &ns); err != nil {
		logger.Error(err, "failed to create namespace")
		return ctrl.Result{}, err
	}

	logger.Info("created namespace", "namespace", namespaceName)
	return ctrl.Result{}, nil
}

func (r *FabricNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fabricv1alpha1.FabricNetwork{}).
		Complete(r)
}