/*

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
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	oathkeeperv1alpha1 "github.com/ory/oathkeeper-maester/api/v1alpha1"
	"github.com/ory/oathkeeper-maester/internal/validation"

	"github.com/avast/retry-go"
	apiv1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	retryAttempts = 5
	retryDelay    = time.Second * 2
	FinalizerName = "finalizer.ory.oathkeeper.sh"
)

// RuleReconciler reconciles a Rule object
type RuleReconciler struct {
	client.Client
	Log              logr.Logger
	RuleConfigmap    types.NamespacedName
	ValidationConfig validation.Config
	RulesFileName    string
}

// +kubebuilder:rbac:groups=oathkeeper.ory.sh,resources=rules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=oathkeeper.ory.sh,resources=rules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

//Reconcile ??
func (r *RuleReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {

	ctx := context.Background()
	_ = r.Log.WithValues("rule", req.NamespacedName)

	var rule oathkeeperv1alpha1.Rule
	skipValidation := false

	if err := r.Get(ctx, req.NamespacedName, &rule); err != nil {
		if apierrs.IsNotFound(err) {
			// just return here, the finalizers have already run
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !skipValidation {
		if err := rule.ValidateWith(r.ValidationConfig); err != nil {
			rule.Status.Validation = &oathkeeperv1alpha1.Validation{}
			rule.Status.Validation.Valid = boolPtr(false)
			rule.Status.Validation.Error = stringPtr(err.Error())
			r.Log.Info(fmt.Sprintf("validation error in Rule %s/%s: \"%s\"", rule.Namespace, rule.Name, err.Error()))
			if err := r.Update(ctx, &rule); err != nil {
				r.Log.Error(err, "unable to update Rule status")
				//Invoke requeue directly without logging error with whole stacktrace
				return ctrl.Result{Requeue: true}, nil
			}
			// continue, as validation can't be fixed by requeuing request and we still have to update the configmap
		} else {
			// rule valid - set the status
			rule.Status.Validation = &oathkeeperv1alpha1.Validation{}
			rule.Status.Validation.Valid = boolPtr(true)
			if err := r.Update(ctx, &rule); err != nil {
				r.Log.Error(err, "unable to update Rule status")
				//Invoke requeue directly without logging error with whole stacktrace
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	var rulesList oathkeeperv1alpha1.RuleList

	if err := r.List(ctx, &rulesList, client.InNamespace(req.NamespacedName.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if rule.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(rule.ObjectMeta.Finalizers, FinalizerName) {
			rule.ObjectMeta.Finalizers = append(rule.ObjectMeta.Finalizers, FinalizerName)
			if err := r.Update(ctx, &rule); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(rule.ObjectMeta.Finalizers, FinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			rulesList = rulesList.FilterOutRule(rule)

			// remove our finalizer from the list and update it.
			rule.ObjectMeta.Finalizers = removeString(rule.ObjectMeta.Finalizers, FinalizerName)
			if err := r.Update(ctx, &rule); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	oathkeeperRulesJSON, err := rulesList.FilterNotValid().
		FilterConfigMapName(rule.Spec.ConfigMapName).
		ToOathkeeperRules()
	if err != nil {
		return ctrl.Result{}, err
	}

	configMap := r.RuleConfigmap
	if rule.Spec.ConfigMapName != nil {
		configMap = types.NamespacedName{
			Name:      *rule.Spec.ConfigMapName,
			Namespace: req.NamespacedName.Namespace,
		}
	}
	if err := r.updateOrCreateRulesConfigmap(ctx, configMap, string(oathkeeperRulesJSON)); err != nil {
		r.Log.Error(err, "unable to process rules Configmap")
		os.Exit(1)
	}

	return ctrl.Result{}, nil
}

//SetupWithManager ??
func (r *RuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&oathkeeperv1alpha1.Rule{}).
		Owns(&apiv1.ConfigMap{}).
		Complete(r)
}

func (r *RuleReconciler) updateOrCreateRulesConfigmap(ctx context.Context, configMap types.NamespacedName, data string) error {

	var oathkeeperRulesConfigmap apiv1.ConfigMap
	var exists = false

	fetchMapFunc := func() error {

		if err := r.Get(ctx, configMap, &oathkeeperRulesConfigmap); err != nil {

			if apierrs.IsForbidden(err) {
				return retry.Unrecoverable(err)
			}

			if apierrs.IsNotFound(err) {
				return nil
			}

			return err
		}

		exists = true
		return nil
	}

	createMapFunc := func() error {
		r.Log.Info("creating ConfigMap")
		oathkeeperRulesConfigmap = apiv1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMap.Name,
				Namespace: configMap.Namespace,
			},
			Data: map[string]string{r.RulesFileName: data},
		}
		return r.Create(ctx, &oathkeeperRulesConfigmap)
	}

	updateMapFunc := func() error {
		r.Log.Info("updating ConfigMap")
		oathkeeperRulesConfigmap.Data = map[string]string{r.RulesFileName: data}
		err := r.Update(ctx, &oathkeeperRulesConfigmap)
		return err
	}

	return retryOnError(func() error {
		exists = false

		if err := fetchMapFunc(); err != nil {
			return err
		}

		if exists {
			err := updateMapFunc()
			if err != nil {
				if isObjectHasBeenModified(err) {
					r.Log.Error(err, "incorrect object version during ConfigMap update")
				}
			}
			return err
		}

		return createMapFunc()
	})
}

func isObjectHasBeenModified(err error) bool {
	return apierrs.IsConflict(err) && strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version")
}

func retryOnError(retryable func() error) error {
	return retryOnErrorWith(retryable, retryAttempts, retryDelay)
}

func retryOnErrorWith(retryable func() error, attempts int, delay time.Duration) error {
	return retry.Do(retryable,
		retry.Attempts(uint(attempts)),
		retry.Delay(delay),
		retry.DelayType(retry.FixedDelay))
}

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
