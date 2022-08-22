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

package virtualmachinepolicy

import (
	"context"
	logger "github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metatable "k8s.io/apimachinery/pkg/api/meta/table"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/tools/cache"
	"strings"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/cloud"
)

// REST implements rest.Storage for VirtualMachinePolicy.
type REST struct {
	vmpIndexer cache.Indexer
	logger     logger.Logger
}

const (
	NoneString = "<none>"
)

var (
	_ rest.Scoper = &REST{}
	_ rest.Getter = &REST{}
	_ rest.Lister = &REST{}
)

// NewREST returns a REST object that will work against API services.
func NewREST(indexer cache.Indexer, l logger.Logger) *REST {
	return &REST{
		vmpIndexer: indexer,
		logger:     l,
	}
}

func (r *REST) New() runtime.Object {
	return &runtimev1alpha1.VirtualMachinePolicy{}
}

func (r *REST) NewList() runtime.Object {
	return &runtimev1alpha1.VirtualMachinePolicyList{}
}

func (r *REST) ShortNames() []string {
	return []string{"vmp"}
}

func (r *REST) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, ok := request.NamespaceFrom(ctx)
	if !ok || len(ns) == 0 {
		return nil, errors.NewBadRequest("Namespace parameter required.")
	}
	fetchKey := types.NamespacedName{Namespace: ns, Name: name}
	obj, found, _ := r.vmpIndexer.GetByKey(fetchKey.String())
	if !found {
		return nil, errors.NewNotFound(runtimev1alpha1.Resource("virtualmachinepolicy"), name)
	}
	return r.convertToVMP(obj.(*cloud.NetworkPolicyStatus)), nil
}

func (r *REST) List(ctx context.Context, _ *internalversion.ListOptions) (runtime.Object, error) {
	ns, _ := request.NamespaceFrom(ctx)
	var objs []interface{}
	if ns == "" {
		objs = r.vmpIndexer.List()
	} else {
		objs, _ = r.vmpIndexer.ByIndex(cloud.NetworkPolicyStatusIndexerByNamespace, ns)
	}
	vmpList := &runtimev1alpha1.VirtualMachinePolicyList{}
	for _, obj := range objs {
		vmp := r.convertToVMP(obj.(*cloud.NetworkPolicyStatus))
		vmpList.Items = append(vmpList.Items, *vmp)
	}
	return vmpList, nil
}

func (r *REST) NamespaceScoped() bool {
	return true
}

func (r *REST) ConvertToTable(_ context.Context, obj runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "VM Name", Type: "string", Description: "Virtual machine name."},
			{Name: "Realization", Type: "string", Description: "Network policy realization status."},
			{Name: "Count", Type: "string", Description: "Number of network policies applied to this virtual machine."},
		},
	}
	if m, err := meta.ListAccessor(obj); err == nil {
		table.ResourceVersion = m.GetResourceVersion()
		table.Continue = m.GetContinue()
		table.RemainingItemCount = m.GetRemainingItemCount()
	} else {
		if m, err := meta.CommonAccessor(obj); err == nil {
			table.ResourceVersion = m.GetResourceVersion()
		}
	}
	var err error
	table.Rows, err = metatable.MetaToTableRow(obj,
		func(obj runtime.Object, m metav1.Object, name, age string) ([]interface{}, error) {
			npStatus := obj.(*runtimev1alpha1.VirtualMachinePolicy)
			return []interface{}{name, npStatus.Status.Realization, len(npStatus.Status.NetworkPolicyDetails)}, nil
		})
	return table, err
}

func (r *REST) convertToVMP(internal *cloud.NetworkPolicyStatus) *runtimev1alpha1.VirtualMachinePolicy {
	i := cloud.InProgress{}
	failed := false
	inProgress := false
	npStatusList := make(map[string]*runtimev1alpha1.NetworkPolicyStatus)
	for anp, status := range internal.NPStatus {
		if status == cloud.NetworkPolicyStatusApplied {
			npStatusList[anp] = &runtimev1alpha1.NetworkPolicyStatus{Realization: runtimev1alpha1.Success, Reason: NoneString}
		} else if strings.Contains(status, i.String()) {
			npStatusList[anp] = &runtimev1alpha1.NetworkPolicyStatus{Realization: runtimev1alpha1.InProgress, Reason: NoneString}
			inProgress = true
		} else {
			npStatusList[anp] = &runtimev1alpha1.NetworkPolicyStatus{Realization: runtimev1alpha1.Failed, Reason: status}
			failed = true
		}
	}
	realization := runtimev1alpha1.Success
	if failed {
		realization = runtimev1alpha1.Failed
	} else if inProgress {
		realization = runtimev1alpha1.InProgress
	}

	vmp := &runtimev1alpha1.VirtualMachinePolicy{}
	vmp.Namespace = internal.Namespace
	vmp.Name = internal.Name
	vmp.Status.Realization = realization
	vmp.Status.NetworkPolicyDetails = npStatusList
	return vmp
}
