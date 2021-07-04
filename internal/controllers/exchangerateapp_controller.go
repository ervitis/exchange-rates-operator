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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
//+kubebuilder:rbac:groups=app.ervitis.nazobenkyo.dev,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=app.ervitis.nazobenkyo.dev,resources=pods,verbs=get;list;watch

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
	instanceType := types.NamespacedName{
		Name:      req.Name,
		Namespace: req.Namespace,
	}

	if err := r.Get(ctx, instanceType, instance); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("instance not found")
			return ctrl.Result{}, err
		}
		r.log.Error(err, "error getting instance")
		return ctrl.Result{}, returnWrappedError("error getting instance", err)
	}

	if r.isMarkedToBeDeleted(instance) {
		if err := r.finalizeExchangeRateApp(ctx, instance); err != nil {
			r.log.Error(err, "error finalizing components")
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(instance, exchangerateAppFinalizer)
		if err := r.Update(ctx, instance); err != nil {
			r.log.Error(err, "error updating instance finalizers")
			return ctrl.Result{}, returnWrappedError("error updating instance finalizers", err)
		}
		return ctrl.Result{}, nil
	}

	if !contains(instance.GetFinalizers(), exchangerateAppFinalizer) {
		if err := r.addFinalizer(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	foundDeployment := &appsv1.Deployment{}
	r.log.Info("searching deployment")
	if err := r.Get(ctx, instanceType, foundDeployment); err != nil {
		if errors.IsNotFound(err) {
			newDeployment, err := r.createDeployment(instance)
			if err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Create(ctx, newDeployment); err != nil {
				r.log.Error(err, "error creating deployment, not requeue")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		r.log.Error(err, "failed to get deployment")
		return ctrl.Result{}, err
	}

	for _, f := range []func(context.Context, *v1alpha1.ExchangeRateApp) error{
		r.ensureService,
		r.ensureSecret,
	} {
		if err := f(ctx, instance); err != nil {
			r.log.Error(err, "error reconciling element")
			if statusErr := r.Status().Update(ctx, instance); statusErr != nil {
				r.log.Error(err, "failed updating status")
				return ctrl.Result{Requeue: true}, statusErr
			}
			return ctrl.Result{Requeue: true}, err
		}
	}

	// check the deployment spec if it's found to match the status when reconciling
	newDeployment, err := r.createDeployment(instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	if r.isDeploymentImageDifferent(foundDeployment, newDeployment) {
		foundDeployment = newDeployment
		r.log.Info("the deployments are not equal, so we reconcile it and update the status")
		if err := r.Update(ctx, foundDeployment); err != nil {
			r.log.Error(err, "failed to update the deployment")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// check the size of the replicas
	if *foundDeployment.Spec.Replicas != instance.Spec.Replicas {
		foundDeployment.Spec.Replicas = &instance.Spec.Replicas
		if err := r.Update(ctx, foundDeployment); err != nil {
			r.log.Error(err, "error updating replicas size in the deployment")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// update the operator status
	podList := &v1.PodList{}
	listOpts := []client.ListOption{client.InNamespace(foundDeployment.Namespace), client.MatchingLabels(labels())}
	if err := r.List(ctx, podList, listOpts...); err != nil {
		r.log.Error(err, "failed to list pods")
		return ctrl.Result{}, err
	}
	var podNames []string
	for _, pod := range podList.Items {
		podNames = append(podNames, pod.Name)
	}
	if !reflect.DeepEqual(podNames, instance.Status.Nodes) {
		r.log.Info("updating pod names")
		instance.Status.Nodes = podNames
		if err := r.Status().Update(ctx, instance); err != nil {
			r.log.Error(err, "error updating pod names in list")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ExchangeRateAppReconciler) isDeploymentImageDifferent(found, newDeployment *appsv1.Deployment) bool {
	for _, curr := range found.Spec.Template.Spec.Containers {
		for _, newDeploy := range newDeployment.Spec.Template.Spec.Containers {
			if curr.Name == newDeploy.Name {
				if curr.Image != newDeploy.Image {
					return true
				}
			}
		}
	}
	return false
}

func (r *ExchangeRateAppReconciler) isDeploymentReady(instance *appsv1.Deployment) bool {
	return instance.Status.ReadyReplicas == instance.Status.Replicas
}

func (r *ExchangeRateAppReconciler) addFinalizer(ctx context.Context, instance *v1alpha1.ExchangeRateApp) error {
	r.log.Info("adding finalizers")
	controllerutil.AddFinalizer(instance, exchangerateAppFinalizer)

	if err := r.Update(ctx, instance); err != nil {
		r.log.Error(err, "error adding finalizer and updating data")
		return err
	}
	return nil
}

func (r *ExchangeRateAppReconciler) finalizeExchangeRateApp(ctx context.Context, instance *v1alpha1.ExchangeRateApp) error {
	r.log.Info("finalizing")
	if err := r.DeleteAllOf(ctx, instance); err != nil {
		return fmt.Errorf("error deleting app: %w", err)
	}
	r.log.Info("app finalized!")
	return nil
}

func (r *ExchangeRateAppReconciler) createService(instance *v1alpha1.ExchangeRateApp) (*v1.Service, controllerutil.MutateFn) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AppName,
			Labels:    labels(),
			Namespace: instance.GetNamespace(),
		},
		Spec: v1.ServiceSpec{
			Selector: labels(),
			Type:     v1.ServiceTypeClusterIP,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		if svc.ObjectMeta.Annotations == nil {
			svc.ObjectMeta.Annotations = labels()
		}
		if len(svc.Spec.Ports) == 0 {
			svc.Spec.Ports = make([]v1.ServicePort, 1)
		}
		svcPort := v1.ServicePort{
			Name:       "exchange-app-svc",
			Protocol:   v1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt(8181),
		}
		svc.Spec.Ports[0] = svcPort
		return nil
	}
	return svc, mutateFn
}

func (r *ExchangeRateAppReconciler) ensureService(ctx context.Context, instance *v1alpha1.ExchangeRateApp) error {
	svc, mutateFn := r.createService(instance)

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, mutateFn)
	if err != nil {
		return fmt.Errorf("error creating or updating secret: %w", err)
	}
	if result != controllerutil.OperationResultNone {
		r.log.Info("api key created or updated")
	}
	return nil
}

func (r *ExchangeRateAppReconciler) createDeployment(instance *v1alpha1.ExchangeRateApp) (*appsv1.Deployment, error) {
	r.log.Info("Creating deployment in namespace", "namespace", instance.GetNamespace())

	reqCpu, err := resource.ParseQuantity(instance.Spec.CPU)
	if err != nil {
		r.log.Error(err, "error assign cpu from spec, check format")
		return nil, err
	}
	reqMem, err := resource.ParseQuantity(instance.Spec.Memory)
	if err != nil {
		r.log.Error(err, "error assign memory form spec, check format")
		return nil, err
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
									Name:          "app-port",
									ContainerPort: 8181,
								},
							},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"cpu":    reqCpu,
									"memory": reqMem,
								},
							},
							LivenessProbe: &v1.Probe{
								Handler: v1.Handler{
									HTTPGet: &v1.HTTPGetAction{
										Path:   "/health",
										Port:   intstr.FromInt(8181),
										Scheme: "HTTP",
									},
								},
								InitialDelaySeconds: 10,
								TimeoutSeconds:      10,
								PeriodSeconds:       3,
							},
							ImagePullPolicy: "Always",
							Env: []v1.EnvVar{
								{
									Name: "API_KEY",
									ValueFrom: &v1.EnvVarSource{
										SecretKeyRef: &v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{
												Name: "exchange-app-secret",
											},
											Key: "API_KEY",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r.log.Info("Deployment created for application")

	if err := ctrl.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		r.log.Error(err, "when setting the controller reference for instance")
		return nil, err
	}
	return deployment, nil
}

func (r *ExchangeRateAppReconciler) ensureSecret(ctx context.Context, instance *v1alpha1.ExchangeRateApp) error {
	secret, mutateFn, err := r.createSecret(instance)

	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, mutateFn)
	if err != nil {
		return fmt.Errorf("error creating or updating secret: %w", err)
	}
	if result != controllerutil.OperationResultNone {
		r.log.Info("api key created or updated")
	}
	return nil
}

func (r *ExchangeRateAppReconciler) createSecret(instance *v1alpha1.ExchangeRateApp) (*v1.Secret, controllerutil.MutateFn, error) {
	secret := &v1.Secret{
		Type: v1.SecretTypeOpaque,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "exchange-app-secret",
			Namespace: instance.GetNamespace(),
			Labels:    labels(),
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(instance, secret, r.Scheme); err != nil {
			return err
		}
		_, exists := secret.Data["API_KEY"]
		if !exists {
			key := os.Getenv("API_KEY")
			if key == "" {
				return fmt.Errorf("API_KEY env var missing")
			}
			if secret.Data == nil {
				secret.Data = map[string][]byte{
					"API_KEY": []byte(key),
				}
			}
		}
		return nil
	}
	return secret, mutateFn, nil
}

func (r *ExchangeRateAppReconciler) isMarkedToBeDeleted(instance *v1alpha1.ExchangeRateApp) bool {
	if instance.GetDeletionTimestamp().IsZero() {
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
		Owns(&appsv1.Deployment{}).
		Owns(&v1.Service{}).
		Owns(&v1.Secret{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
