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

package vpc

import (
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/labels"
	"context"
	"strings"

	logger "github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metatable "k8s.io/apimachinery/pkg/api/meta/table"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/inventory/store"
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
	return &runtimev1alpha1.Vpc{}
}

func (r *REST) NewList() runtime.Object {
	return &runtimev1alpha1.VpcList{}
}

func (r *REST) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	ns, ok := request.NamespaceFrom(ctx)
	if !ok || len(ns) == 0 {
		return nil, errors.NewBadRequest("Namespace cannot be empty.")
	}
	namespacedName := ns + "/" + name

	objs, err := r.cloudInventory.GetVpcsFromIndexer(indexer.ByNamespacedName, namespacedName)
	if err != nil {
		return nil, err
	}

	if len(objs) == 0 {
		return nil, errors.NewNotFound(runtimev1alpha1.Resource("vpc"), name)
	}
	vpc := objs[0].(*runtimev1alpha1.Vpc)
	return vpc, nil
}

func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	// List only supports four types of input options:
	// 1. All namespace.
	// 2. Labelselector with only the specific namespace, the only valid labelselectors are "cpa.name=<accountname>",
	//    "cpa.namespace=<accountNamespace> and "region=<region>".
	// 3. Fieldselector with only the specific namespace, the only valid fieldselectors is "metadata.name=<metadata.name>".
	// 4. Specific Namespace.
	accountName := ""
	accountNamespace := ""
	region := ""

	if options != nil && options.LabelSelector != nil && options.LabelSelector.String() != "" {
		labelSelectorStrings := strings.Split(options.LabelSelector.String(), ",")
		for _, labelSelectorString := range labelSelectorStrings {
			labelKeyAndValue := strings.Split(labelSelectorString, "=")
			if labelKeyAndValue[0] == labels.CloudAccountName {
				accountName = labelKeyAndValue[1]
			} else if labelKeyAndValue[0] == labels.CloudAccountNamespace {
				accountNamespace = labelKeyAndValue[1]
			} else if labelKeyAndValue[0] == labels.CloudRegion {
				region = strings.ToLower(labelKeyAndValue[1])
			} else {
				return nil, errors.NewBadRequest("unsupported label selector, supported labels are: cpa.name and region")
			}
		}
	}

	name := ""
	namespace := ""
	if options != nil && options.FieldSelector != nil && options.FieldSelector.String() != "" {
		fieldSelectorStrings := strings.Split(options.FieldSelector.String(), ",")
		for _, fieldSelectorString := range fieldSelectorStrings {
			fieldKeyAndValue := strings.Split(fieldSelectorString, "=")
			if fieldKeyAndValue[0] == "metadata.name" {
				name = fieldKeyAndValue[1]
			} else if fieldKeyAndValue[0] == "metadata.namespace" {
				namespace = fieldKeyAndValue[1]
			} else {
				return nil, errors.NewBadRequest("unsupported field selector, supported labels are: metadata.name and metadata.namespace")
			}
		}
	}

	ns, _ := request.NamespaceFrom(ctx)
	if ns != metav1.NamespaceDefault && namespace != "" && ns != namespace {
		return nil, errors.NewBadRequest("namespace in field selector is different from namespace filter")
	}
	if namespace == "" {
		namespace = ns
	}

	if namespace == "" && (accountName != "" || region != "" || name != "") {
		return nil, errors.NewBadRequest("cannot query with all namespaces. Namespace should be specified")
	}

	var objs []interface{}
	if namespace == "" {
		objs = r.cloudInventory.GetAllVpcs()
	} else if accountName != "" {
		accountNameSpacedName := types.NamespacedName{
			Name:      accountName,
			Namespace: accountNamespace,
		}
		// If account namespace is not specified in the label selector, then use the namespace specified.
		if accountNamespace == "" {
			accountNameSpacedName.Namespace = namespace
		}
		objs, _ = r.cloudInventory.GetVpcsFromIndexer(indexer.VpcByNamespacedAccountName, accountNameSpacedName.String())
	} else if name != "" {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}
		objs, _ = r.cloudInventory.GetVpcsFromIndexer(indexer.ByNamespacedName, namespacedName.String())
	} else if region != "" {
		namespacedRegion := types.NamespacedName{
			Namespace: namespace,
			Name:      region,
		}
		objs, _ = r.cloudInventory.GetVpcsFromIndexer(indexer.VpcByNamespacedRegion, namespacedRegion.String())
	} else {
		objs, _ = r.cloudInventory.GetVpcsFromIndexer(indexer.ByNamespace, namespace)
	}
	vpcList := &runtimev1alpha1.VpcList{}
	for _, obj := range objs {
		vpc := obj.(*runtimev1alpha1.Vpc)
		vpcList.Items = append(vpcList.Items, *vpc)
	}

	return vpcList, nil
}

func (r *REST) NamespaceScoped() bool {
	return true
}

func (r *REST) ConvertToTable(_ context.Context, obj runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "NAME", Type: "string", Description: "Name"},
			{Name: "CLOUD PROVIDER", Type: "string", Description: "Cloud Provider"},
			{Name: "REGION", Type: "string", Description: "Region"},
			{Name: "MANAGED", Type: "bool", Description: "Managed VPC"},
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
			vpc := obj.(*runtimev1alpha1.Vpc)
			if vpc.Name == "" {
				return nil, nil
			}
			return []interface{}{vpc.Name, vpc.Status.Provider, vpc.Status.Region, vpc.Status.Managed}, nil
		})
	return table, err
}

func (r *REST) Watch(ctx context.Context, options *internalversion.ListOptions) (watch.Interface, error) {
	key, label, field := store.GetSelectors(options)
	return r.cloudInventory.WatchVpcs(ctx, key, label, field)
}
