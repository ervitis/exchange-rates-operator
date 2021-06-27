/*
Copyright 2021.

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

package controllers

import (
	"context"
	"fmt"
	"github.com/ervitis/exchange-rates-operator/internal/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ExchangeRateAppReconciler reconciles a ExchangeRateApp object
type ExchangeRateAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	exchangerateAppFinalizer = "finalizer.exchangerate.nazobenkyou.dev"
)

//+kubebuilder:rbac:groups=app.ervitis.nazobenkyo.dev,resources=exchangerateapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.ervitis.nazobenkyo.dev,resources=exchangerateapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=app.ervitis.nazobenkyo.dev,resources=exchangerateapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the ExchangeRateApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *ExchangeRateAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lg := log.FromContext(ctx).WithName("exchangeRateApp")

	instance := &v1alpha1.ExchangeRateApp{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, instance); err != nil {
		if errors.IsNotFound(err) {
			lg.Info("instance not found")
			return ctrl.Result{}, err
		}
		lg.Error(err, "error getting instance")
		return ctrl.Result{}, returnWrappedError("error getting instance", err)
	}

	if r.isMarkedToBeDeleted(instance) {
		// TODO finalize client

		instance.SetFinalizers([]string{exchangerateAppFinalizer})
		if err := r.Update(ctx, instance); err != nil {
			lg.Error(err, "error updating instance finalizers")
			return ctrl.Result{}, returnWrappedError("error updating instance finalizers", err)
		}
		return ctrl.Result{}, nil
	}

	if !contains(instance.GetFinalizers(), exchangerateAppFinalizer) {
		// TODO add finalizer
	}


	return ctrl.Result{}, nil
}

func (r *ExchangeRateAppReconciler) isMarkedToBeDeleted(instance *v1alpha1.ExchangeRateApp) bool {
	if instance.GetDeletionTimestamp() == nil {
		return false
	}

	return contains(instance.GetFinalizers(), exchangerateAppFinalizer)
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func returnWrappedError(msg string, err error) error {
	return fmt.Errorf(msg + ": %w", err)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExchangeRateAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ExchangeRateApp{}).
		Complete(r)
}
