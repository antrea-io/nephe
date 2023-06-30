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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/apiserver/registry/inventory/selector"
	"antrea.io/nephe/pkg/apiserver/registry/inventory/util"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/inventory/store"
	"antrea.io/nephe/pkg/labels"
)

// REST implements rest.Storage for VirtualMachine Inventory.
type REST struct {
	cloudInventory inventory.Interface
	logger         logger.Logger

	fieldKeysMap map[string]struct{}
	labelKeysMap map[string]struct{}
}

var (
	_ rest.Scoper  = &REST{}
	_ rest.Getter  = &REST{}
	_ rest.Watcher = &REST{}
	_ rest.Lister  = &REST{}
	_ rest.Updater = &REST{}
)

// NewREST returns a REST object that will work against API services.
func NewREST(cloudInventory inventory.Interface, l logger.Logger) *REST {
	r := REST{
		cloudInventory: cloudInventory,
		logger:         l,
	}
	r.setSupportedLabelKeysMap()
	r.setSupportedFieldKeysMap()
	return &r
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

func (r *REST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, _ rest.ValidateObjectFunc,
	_ rest.ValidateObjectUpdateFunc, _ bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	ns, ok := request.NamespaceFrom(ctx)
	if !ok || len(ns) == 0 {
		return nil, false, errors.NewBadRequest("namespace cannot be empty.")
	}
	r.logger.Info("Received update for vm", "vm", name)
	namespacedName := ns + "/" + name
	cachedVm, ok := r.cloudInventory.GetVmByKey(namespacedName)
	if !ok {
		return nil, false, errors.NewNotFound(runtimev1alpha1.Resource("virtualmachine"), name)
	}

	updatedObject, err := objInfo.UpdatedObject(context.TODO(), cachedVm)
	if err != nil {
		return nil, false, err
	}

	updatedVm, ok := updatedObject.(*runtimev1alpha1.VirtualMachine)
	if !ok {
		return nil, false, errors.NewBadRequest("invalid resource object")
	}
	if !reflect.DeepEqual(updatedVm.Spec, cachedVm.Spec) {
		err = r.cloudInventory.UpdateVm(updatedVm)
		if err != nil {
			return nil, false, errors.NewInternalError(fmt.Errorf("failed to update the resource"))
		}
	}
	return updatedVm, false, nil
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
	labelSelectors, fieldSelectors, err := getSelectors(options, r.labelKeysMap, r.fieldKeysMap)
	if err != nil {
		return nil, err
	}

	var objsByNamespace []interface{}
	contextNamespace, _ := request.NamespaceFrom(ctx)
	if contextNamespace == "" {
		objsByNamespace = r.cloudInventory.GetAllVms()
	} else {
		// Resources are listed based on the context Namespace.
		if labelSelectors[selector.CloudAccountNamespace] != "" &&
			labelSelectors[selector.CloudAccountNamespace] != contextNamespace {
			// Since account Namespace is different from context Namespace, return empty.
			return &runtimev1alpha1.VpcList{}, nil
		}
		if fieldSelectors[selector.MetaNamespace] != "" &&
			fieldSelectors[selector.MetaNamespace] != contextNamespace {
			// Since meta Namespace is different from context Namespace, return empty.
			return &runtimev1alpha1.VirtualMachineList{}, nil
		}

		objsByNamespace, _ = r.cloudInventory.GetVmFromIndexer(indexer.ByNamespace, contextNamespace)
	}

	filterObjsByLabels := util.GetFilteredObjsByLabels(objsByNamespace, labelSelectors, r.labelKeysMap)
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
	contextNamespace, _ := request.NamespaceFrom(ctx)
	if key != "" {
		if contextNamespace == "" {
			return nil, errors.NewBadRequest("cannot watch for a resources in all namespaces")
		} else {
			key = contextNamespace + "/" + key
		}
	} else if contextNamespace != "" {
		// Namespace specified and field selector doesn't have namespace.
		if _, ok := field.RequiresExactMatch("metadata.namespace"); !ok {
			field = fields.AndSelectors(field, fields.OneTermEqualSelector("metadata.namespace", contextNamespace))
		}
	}
	return r.cloudInventory.WatchVms(ctx, key, label, field)
}

// setSupportedFieldKeysMap sets the map of supported fields.
func (r *REST) setSupportedFieldKeysMap() {
	// Make sure field selectors are in sync with inventory store.
	r.fieldKeysMap = make(map[string]struct{})
	r.fieldKeysMap[selector.MetaName] = struct{}{}
	r.fieldKeysMap[selector.MetaNamespace] = struct{}{}
	r.fieldKeysMap[selector.StatusCloudId] = struct{}{}
	r.fieldKeysMap[selector.StatusCloudVpcId] = struct{}{}
	r.fieldKeysMap[selector.StatusRegion] = struct{}{}
}

// setSupportedLabelKeysMap set the map of supported label names.
func (r *REST) setSupportedLabelKeysMap() {
	r.labelKeysMap = make(map[string]struct{})
	r.labelKeysMap[selector.CloudAccountNamespace] = struct{}{}
	r.labelKeysMap[selector.CloudAccountName] = struct{}{}
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
	if err := util.GetLabelSelectors(options, labelSelectors, labelsNameMap); err != nil {
		return nil, nil, errors.NewBadRequest(fmt.Sprintf("unsupported label selector, supported labels are: "+
			"%v", reflect.ValueOf(labelsNameMap).MapKeys()))
	}
	if err := util.GetFieldSelectors(options, fieldSelectors, fieldsNameMap); err != nil {
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
		if fieldSelector[selector.MetaName] != "" &&
			vm.Name != fieldSelector[selector.MetaName] {
			continue
		}
		if fieldSelector[selector.MetaNamespace] != "" &&
			vm.Namespace != fieldSelector[selector.MetaNamespace] {
			continue
		}
		if fieldSelector[selector.StatusCloudId] != "" &&
			vm.Status.CloudId != fieldSelector[selector.StatusCloudId] {
			continue
		}
		if fieldSelector[selector.StatusCloudVpcId] != "" &&
			vm.Status.CloudVpcId != fieldSelector[selector.StatusCloudVpcId] {
			continue
		}
		if fieldSelector[selector.StatusRegion] != "" &&
			vm.Status.Region != fieldSelector[selector.StatusRegion] {
			continue
		}
		ret = append(ret, obj)
	}
	return ret
}
