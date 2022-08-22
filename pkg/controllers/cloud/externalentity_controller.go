// Copyright 2022 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloud

import (
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	"context"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ExternalEntityReconciler reconciles a ExternalEntity object.
type ExternalEntityReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// nolint:lll
// +kubebuilder:rbac:groups=crd.antrea.io,resources=externalentities,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.antrea.io,resources=externalentities/status,verbs=get;update;patch

func (r *ExternalEntityReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("externalentity", req.NamespacedName)

	return ctrl.Result{}, nil
}

// SetupWithManager registers ExternalEntityReconciler itself with manager.
func (r *ExternalEntityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&antreatypes.ExternalEntity{}).
		Complete(r)
}
