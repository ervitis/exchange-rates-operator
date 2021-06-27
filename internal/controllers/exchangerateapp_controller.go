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
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ExchangeRateAppReconciler reconciles a ExchangeRateApp object
type ExchangeRateAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	log logr.Logger
}

const (
	exchangerateAppFinalizer = "finalizer.exchangerate.nazobenkyou.dev"

	AppName = "exchange-rate"
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
	r.log = log.FromContext(ctx).WithName("exchangeRateApp")

	instance := &v1alpha1.ExchangeRateApp{}

	if err := r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, instance); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("instance not found")
			return ctrl.Result{}, err
		}
		r.log.Error(err, "error getting instance")
		return ctrl.Result{}, returnWrappedError("error getting instance", err)
	}

	if r.isMarkedToBeDeleted(instance) {
		// TODO finalize client

		instance.SetFinalizers([]string{exchangerateAppFinalizer})
		if err := r.Update(ctx, instance); err != nil {
			r.log.Error(err, "error updating instance finalizers")
			return ctrl.Result{}, returnWrappedError("error updating instance finalizers", err)
		}
		return ctrl.Result{}, nil
	}

	if !contains(instance.GetFinalizers(), exchangerateAppFinalizer) {
		// TODO add finalizer
	}

	// TODO set the deployment and update it to reconcile

	return ctrl.Result{}, nil
}

func (r *ExchangeRateAppReconciler) createDeployment(instance *v1alpha1.ExchangeRateApp) *appsv1.Deployment {
	r.log.Info("Creating deployment in namespace", "namespace", instance.GetNamespace())

	reqCpu, err := resource.ParseQuantity(instance.Spec.CPU)
	if err != nil {
		r.log.Error(err, "error assign cpu from spec, check format")
		return nil
	}
	reqMem, err := resource.ParseQuantity(instance.Spec.Memory)
	if err != nil {
		r.log.Error(err, "error assign memory form spec, check format")
		return nil
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.GetName(),
			Namespace: instance.GetNamespace(),
			Labels:    labels(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &instance.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels(),
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels(),
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  AppName,
							Image: instance.Spec.Image,
							Ports: []v1.ContainerPort{
								{
									Name:          fmt.Sprintf("%s-port", AppName),
									ContainerPort: 8080,
								},
							},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceRequestsCPU: reqCpu,
									v1.ResourceRequestsMemory: reqMem,
								},
							},
							LivenessProbe:   &v1.Probe{
								Handler:             v1.Handler{
									HTTPGet: &v1.HTTPGetAction{
										Path:        "/health",
										Port:        intstr.FromInt(8080),
										Scheme:      "http",
									},
								},
								InitialDelaySeconds: 10,
								TimeoutSeconds:      10,
								PeriodSeconds:       3,
							},
							ImagePullPolicy: "Always",
						},
					},
				},
			},
		},
	}

	r.log.Info("Deployment created for application")

	if err := ctrl.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		r.log.Error(err, "when setting the controller reference for instance")
		return nil
	}
	return deployment
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

func labels() map[string]string {
	return map[string]string{
		"app": AppName,
	}
}

func returnWrappedError(msg string, err error) error {
	return fmt.Errorf(msg+": %w", err)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExchangeRateAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ExchangeRateApp{}).
		Complete(r)
}
