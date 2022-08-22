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
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	converter "antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/logging"
	"context"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VirtualMachineReconciler reconciles a VirtualMachine object.
type VirtualMachineReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	converter converter.VMConverter
}

// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=virtualmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=virtualmachines/status,verbs=get;update;patch
// nolint:lll

func (r *VirtualMachineReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("Virtualmachine", req.NamespacedName)
	virtualMachine := cloudv1alpha1.VirtualMachine{}
	if err := r.Get(context.TODO(), req.NamespacedName, &virtualMachine); err != nil {
		if !errors.IsNotFound(err) {
			r.Log.V(0).Info("Error getting VM crd", "vm", req.NamespacedName)
			return ctrl.Result{}, err
		}
		// Fall through if owner is deleted.
		r.Log.V(1).Info("Is delete")
		accessor, _ := meta.Accessor(&virtualMachine)
		accessor.SetName(req.Name)
		accessor.SetNamespace(req.Namespace)
	}
	r.converter.Ch <- virtualMachine
	return ctrl.Result{}, nil
}

// SetupWithManager registers VirtualMachineReconciler with manager.
// It also sets up converter channels to forward VM events.
func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.converter = converter.VMConverter{
		Client: r.Client,
		Log:    logging.GetLogger("converter").WithName("VMConverter"),
		Ch:     make(chan cloudv1alpha1.VirtualMachine),
		Scheme: r.Scheme,
	}
	go r.converter.Start()
	return ctrl.NewControllerManagedBy(mgr).
		For(&cloudv1alpha1.VirtualMachine{}).
		Complete(r)
}
