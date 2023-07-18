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
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/apiserver/registry/inventory/selector"
	"antrea.io/nephe/pkg/apiserver/registry/inventory/util"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/labels"
)

// REST implements rest.Storage for VPC Inventory.
type REST struct {
	cloudInventory inventory.Interface
	logger         logger.Logger

	fieldKeysMap map[string]struct{}
	labelKeysMap map[string]struct{}
}

var (
	_ rest.Scoper = &REST{}
	_ rest.Getter = &REST{}
	_ rest.Lister = &REST{}
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
		return nil, errors.NewNotFound(runtimev1alpha1.Resource("securitygroup"), name)
	}
	return sg, nil
}

func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	labelSelectors, fieldSelectors, err := getSelectors(options, r.labelKeysMap, r.fieldKeysMap)
	if err != nil {
		return nil, err
	}

	var objsByNamespace []interface{}
	contextNamespace, _ := request.NamespaceFrom(ctx)
	if contextNamespace == "" {
		objsByNamespace = r.cloudInventory.GetAllSgs()
	} else {
		// Resources are listed based on the context Namespace.
		if labelSelectors[selector.CloudAccountNamespace] != "" &&
			labelSelectors[selector.CloudAccountNamespace] != contextNamespace {
			// Since account Namespace is different from context Namespace, return empty.
			return &runtimev1alpha1.SecurityGroupList{}, nil
		}
		if fieldSelectors[selector.MetaNamespace] != "" &&
			fieldSelectors[selector.MetaNamespace] != contextNamespace {
			// Since meta Namespace is different from context Namespace, return empty.
			return &runtimev1alpha1.SecurityGroupList{}, nil
		}

		objsByNamespace, _ = r.cloudInventory.GetSgsFromIndexer(indexer.ByNamespace, contextNamespace)
	}

	filterObjsByLabels := util.GetFilteredObjsByLabels(objsByNamespace, labelSelectors, r.labelKeysMap)
	filterObjs := getFilteredObjsByFields(filterObjsByLabels, fieldSelectors)
	return sortAndConvertObjsToSgList(filterObjs), nil
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
			{Name: "INGRESS-RULES", Type: "int", Description: "Number of Ingress Rules"},
			{Name: "EGRESS-RULES", Type: "int", Description: "Number of egress Rules"},
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
			var ingressRules, egressRules int
			for _, rule := range sg.Status.Rules {
				if rule.Ingress {
					ingressRules++
				} else {
					egressRules++
				}
			}
			return []interface{}{sg.Name, sg.Status.Provider, sg.Status.Region, sg.Labels[labels.VpcName], ingressRules, egressRules}, nil
		})
	return table, err
}

// setSupportedFieldKeysMap sets the map of supported fields.
func (r *REST) setSupportedFieldKeysMap() {
	// Make sure field selectors are in sync with inventory store.
	r.fieldKeysMap = make(map[string]struct{})
	r.fieldKeysMap[selector.MetaName] = struct{}{}
	r.fieldKeysMap[selector.MetaNamespace] = struct{}{}
	r.fieldKeysMap[selector.StatusCloudId] = struct{}{}
}

// setSupportedLabelKeysMap set the map of supported label names.
func (r *REST) setSupportedLabelKeysMap() {
	r.labelKeysMap = make(map[string]struct{})
	r.labelKeysMap[selector.CloudAccountNamespace] = struct{}{}
	r.labelKeysMap[selector.CloudAccountName] = struct{}{}
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
		sortSgRule(sg.Status.Rules)
		sgList.Items = append(sgList.Items, *sg)
	}
	return sgList
}

// sortSgRule sorts security group rules based on direction of rule and priority.
func sortSgRule(rules []runtimev1alpha1.Rule) {
	sort.Slice(rules, func(i, j int) bool {
		// First sort with rule direction.
		if rules[i].Ingress && !rules[j].Ingress {
			return true
		} else if !rules[i].Ingress && rules[j].Ingress {
			return false
		}
		// when direction is same, second level of sort with rule priority.
		return rules[i].Priority < rules[j].Priority
	})
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
		sg := obj.(*runtimev1alpha1.SecurityGroup)
		if fieldSelector[selector.MetaName] != "" &&
			sg.Name != fieldSelector[selector.MetaName] {
			continue
		}
		if fieldSelector[selector.MetaNamespace] != "" &&
			sg.Namespace != fieldSelector[selector.MetaNamespace] {
			continue
		}
		if fieldSelector[selector.StatusCloudId] != "" &&
			sg.Status.CloudId != fieldSelector[selector.StatusCloudId] {
			continue
		}
		ret = append(ret, obj)
	}
	return ret
}
