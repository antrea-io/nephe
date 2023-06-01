// Copyright 2023 Antrea Authors.
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

package securitygroup

import (
	"context"
	"sort"

	logger "github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metatable "k8s.io/apimachinery/pkg/api/meta/table"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/inventory/store"
	"antrea.io/nephe/pkg/labels"
)

// REST implements rest.Storage for VPC Inventory.
type REST struct {
	cloudInventory inventory.Interface
	logger         logger.Logger
}

var (
	_ rest.Scoper  = &REST{}
	_ rest.Getter  = &REST{}
	_ rest.Watcher = &REST{}
	_ rest.Lister  = &REST{}
)

// NewREST returns a REST object that will work against API services.
func NewREST(cloudInventory inventory.Interface, l logger.Logger) *REST {
	return &REST{
		cloudInventory: cloudInventory,
		logger:         l,
	}
}

func (r *REST) New() runtime.Object {
	return &runtimev1alpha1.SecurityGroup{}
}

func (r *REST) Destroy() {
}

func (r *REST) NewList() runtime.Object {
	return &runtimev1alpha1.SecurityGroupList{}
}

func (r *REST) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, ok := request.NamespaceFrom(ctx)
	if !ok || len(ns) == 0 {
		return nil, errors.NewBadRequest("namespace cannot be empty.")
	}
	namespacedName := ns + "/" + name
	sg, ok := r.cloudInventory.GetSgByKey(namespacedName)
	if !ok {
		return nil, errors.NewNotFound(runtimev1alpha1.Resource("virtualmachine"), name)
	}
	return sg, nil
}

func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	var objsByNamespace []interface{}
	contextNamespace, _ := request.NamespaceFrom(ctx)
	if contextNamespace == "" {
		objsByNamespace = r.cloudInventory.GetAllSgs()
	} else {
		objsByNamespace, _ = r.cloudInventory.GetSgsFromIndexer(indexer.ByNamespace, contextNamespace)
	}
	// TODO: Add label and field selectors.
	return sortAndConvertObjsToSgList(objsByNamespace), nil
}

func (r *REST) NamespaceScoped() bool {
	return true
}

func (r *REST) ConvertToTable(_ context.Context, obj runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "NAME", Type: "string", Description: "Name"},
			{Name: "CLOUD-PROVIDER", Type: "string", Description: "Cloud Provider"},
			{Name: "REGION", Type: "string", Description: "Region"},
			{Name: "VIRTUAL-PRIVATE-CLOUD", Type: "string", Description: "VPC/VNET"},
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
		func(obj runtime.Object, _ metav1.Object, _, _ string) ([]interface{}, error) {
			sg := obj.(*runtimev1alpha1.SecurityGroup)
			if sg.Name == "" {
				return nil, nil
			}
			return []interface{}{sg.Name, sg.Status.Provider, sg.Status.Region, sg.Labels[labels.VpcName]}, nil
		})
	return table, err
}

func (r *REST) Watch(ctx context.Context, options *internalversion.ListOptions) (watch.Interface, error) {
	key, label, field := store.GetSelectors(options)
	return r.cloudInventory.WatchVpcs(ctx, key, label, field)
}

// sortAndConvertObjsToSgList sorts the objs based on Namespace and Name and returns the SG list.
func sortAndConvertObjsToSgList(objs []interface{}) *runtimev1alpha1.SecurityGroupList {
	sort.Slice(objs, func(i, j int) bool {
		sgI := objs[i].(*runtimev1alpha1.SecurityGroup)
		sgJ := objs[j].(*runtimev1alpha1.SecurityGroup)
		// First Sort with Namespace.
		if sgI.Namespace < sgJ.Namespace {
			return true
		} else if sgI.Namespace > sgJ.Namespace {
			return false
		}
		// Second Sort with Name.
		return sgI.Name < sgJ.Name
	})

	sgList := &runtimev1alpha1.SecurityGroupList{}
	for _, obj := range objs {
		sg := obj.(*runtimev1alpha1.SecurityGroup)
		sgList.Items = append(sgList.Items, *sg)
	}
	return sgList
}
