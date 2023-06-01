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

package virtualmachine

import (
	"context"
	"fmt"
	"reflect"
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
	registryinventory "antrea.io/nephe/pkg/apiserver/registry/inventory"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/inventory/store"
	"antrea.io/nephe/pkg/labels"
)

// REST implements rest.Storage for VirtualMachine Inventory.
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
	return &runtimev1alpha1.VirtualMachine{}
}

func (r *REST) Destroy() {
}

func (r *REST) NewList() runtime.Object {
	return &runtimev1alpha1.VirtualMachine{}
}

func (r *REST) ShortNames() []string {
	return []string{"vm"}
}

func (r *REST) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, ok := request.NamespaceFrom(ctx)
	if !ok || len(ns) == 0 {
		return nil, errors.NewBadRequest("Namespace cannot be empty.")
	}

	namespacedName := ns + "/" + name
	vm, ok := r.cloudInventory.GetVmByKey(namespacedName)
	if !ok {
		return nil, errors.NewNotFound(runtimev1alpha1.Resource("virtualmachine"), name)
	}
	return vm, nil
}

// List returns a list of object based on Namespace and filtered by labels and field selectors.
func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	contextNamespace, _ := request.NamespaceFrom(ctx)
	// Return all the vm objects when 'all' is specified.
	if contextNamespace == "" {
		return sortAndConvertObjsToVmList(r.cloudInventory.GetAllVms()), nil
	}

	supportedLabelsKeyMap := getSupportedLabelKeysMap()
	supportedFieldsKeyMap := getSupportedFieldKeysMap()
	labelSelectors, fieldSelectors, err := getSelectors(options, supportedLabelsKeyMap, supportedFieldsKeyMap)
	if err != nil {
		return nil, err
	}

	// Resources are listed based on the context Namespace.
	if labelSelectors[registryinventory.CloudAccountNamespace] != "" &&
		labelSelectors[registryinventory.CloudAccountNamespace] != contextNamespace {
		// Since account Namespace is different from context Namespace, return empty.
		return &runtimev1alpha1.VpcList{}, nil
	}

	if fieldSelectors[registryinventory.MetaNamespace] != "" &&
		fieldSelectors[registryinventory.MetaNamespace] != contextNamespace {
		// Since meta Namespace is different from context Namespace, return empty.
		return &runtimev1alpha1.VirtualMachineList{}, nil
	}

	objsByNamespace, _ := r.cloudInventory.GetVmFromIndexer(indexer.ByNamespace, contextNamespace)
	filterObjsByLabels := registryinventory.GetFilteredObjsByLabels(objsByNamespace, labelSelectors, supportedLabelsKeyMap)
	filterObjs := getFilteredObjsByFields(filterObjsByLabels, fieldSelectors)
	return sortAndConvertObjsToVmList(filterObjs), nil
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
			{Name: "STATE", Type: "string", Description: "Running state"},
			{Name: "AGENTED", Type: "bool", Description: "Agent installed"},
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
			vm := obj.(*runtimev1alpha1.VirtualMachine)
			if vm.Name == "" {
				return nil, nil
			}
			return []interface{}{vm.Name, vm.Status.Provider, vm.Status.Region,
				vm.Labels[labels.VpcName], vm.Status.State, vm.Status.Agented}, nil
		})
	return table, err
}

func (r *REST) Watch(ctx context.Context, options *internalversion.ListOptions) (watch.Interface, error) {
	key, label, field := store.GetSelectors(options)
	return r.cloudInventory.WatchVms(ctx, key, label, field)
}

// sortAndConvertObjsToVmList sorts the objs based on Namespace and Name and returns the VPC list.
func sortAndConvertObjsToVmList(objs []interface{}) *runtimev1alpha1.VirtualMachineList {
	sort.Slice(objs, func(i, j int) bool {
		vmI := objs[i].(*runtimev1alpha1.VirtualMachine)
		vmJ := objs[j].(*runtimev1alpha1.VirtualMachine)
		// First Sort with Namespace.
		if vmI.Namespace < vmJ.Namespace {
			return true
		} else if vmI.Namespace > vmJ.Namespace {
			return false
		}
		// Second Sort with Name.
		return vmI.Name < vmJ.Name
	})

	vmList := &runtimev1alpha1.VirtualMachineList{}
	for _, obj := range objs {
		vm := obj.(*runtimev1alpha1.VirtualMachine)
		vmList.Items = append(vmList.Items, *vm)
	}
	return vmList
}

// getSelectors creates and returns a map of supported label and field selectors.
func getSelectors(options *internalversion.ListOptions, labelsNameMap map[string]struct{},
	fieldsNameMap map[string]struct{}) (map[string]string, map[string]string, error) {
	labelSelectors := make(map[string]string)
	fieldSelectors := make(map[string]string)
	if err := registryinventory.GetLabelSelectors(options, labelSelectors, labelsNameMap); err != nil {
		return nil, nil, errors.NewBadRequest(fmt.Sprintf("unsupported label selector, supported labels are: "+
			"%v", reflect.ValueOf(labelsNameMap).MapKeys()))
	}
	if err := registryinventory.GetFieldSelectors(options, fieldSelectors, fieldsNameMap); err != nil {
		return nil, nil, errors.NewBadRequest(fmt.Sprintf("unsupported field selector, supported fields are: "+
			"%v", reflect.ValueOf(fieldsNameMap).MapKeys()))
	}
	return labelSelectors, fieldSelectors, nil
}

// getFilteredObjsByFields filters the objs based on the fieldSelector and returns a list.
func getFilteredObjsByFields(objs []interface{}, fieldSelector map[string]string) []interface{} {
	if len(fieldSelector) == 0 {
		return objs
	}

	var ret []interface{}
	for _, obj := range objs {
		vm := obj.(*runtimev1alpha1.VirtualMachine)
		if fieldSelector[registryinventory.MetaName] != "" &&
			vm.Name != fieldSelector[registryinventory.MetaName] {
			continue
		}
		if fieldSelector[registryinventory.StatusCloudId] != "" &&
			vm.Status.CloudId != fieldSelector[registryinventory.StatusCloudId] {
			continue
		}
		if fieldSelector[registryinventory.StatusCloudVpcId] != "" &&
			vm.Status.CloudVpcId != fieldSelector[registryinventory.StatusCloudVpcId] {
			continue
		}
		if fieldSelector[registryinventory.StatusRegion] != "" &&
			vm.Status.Region != fieldSelector[registryinventory.StatusRegion] {
			continue
		}
		ret = append(ret, obj)
	}
	return ret
}

// getSupportedFieldKeysMap returns a map supported fields.
// Valid field selectors are "metadata.name=<name>", "metadata.namespace=<namespace>",
// "status.cloudId=<cloudId>", "status.cloudVpcId=<cloudVpcId>", "status.region=<region>".
func getSupportedFieldKeysMap() map[string]struct{} {
	fieldKeys := make(map[string]struct{})
	fieldKeys[registryinventory.MetaName] = struct{}{}
	fieldKeys[registryinventory.MetaNamespace] = struct{}{}
	fieldKeys[registryinventory.StatusCloudId] = struct{}{}
	fieldKeys[registryinventory.StatusCloudVpcId] = struct{}{}
	fieldKeys[registryinventory.StatusRegion] = struct{}{}
	return fieldKeys
}

// getSupportedFieldKeysMap returns a map supported label names.
// Valid label selectors are "nephe.io/cpa-name=<accountname>" and "nephe.io/cpa-namespace=<accountNamespace>".
func getSupportedLabelKeysMap() map[string]struct{} {
	labelKeys := make(map[string]struct{})
	labelKeys[registryinventory.CloudAccountNamespace] = struct{}{}
	labelKeys[registryinventory.CloudAccountName] = struct{}{}
	return labelKeys
}
