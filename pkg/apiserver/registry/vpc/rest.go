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
	"context"

	logger "github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/tools/cache"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/cloud"
)

// REST implements rest.Storage for Vpc.
type REST struct {
	vpcIndexer cache.Indexer
	logger     logger.Logger
}

var (
	_ rest.Scoper = &REST{}
	_ rest.Getter = &REST{}
)

// NewREST returns a REST object that will work against API services.
func NewREST(indexer cache.Indexer, l logger.Logger) *REST {
	return &REST{
		vpcIndexer: indexer, //indexer?
		logger:     l,
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
		return nil, errors.NewBadRequest("Namespace parameter required.")
	}
	fetchKey := types.NamespacedName{Namespace: ns, Name: name}
	obj, found, _ := r.vpcIndexer.GetByKey(fetchKey.String())
	if !found {
		return nil, errors.NewNotFound(runtimev1alpha1.Resource("vpc"), name)
	}
	return obj.(*runtimev1alpha1.Vpc), nil
}

func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) { // assume cpa account is in labels here
	labelSelector := labels.Everything()
	if options != nil && options.LabelSelector != nil {
		labelSelector = options.LabelSelector
	}
	ns, _ := request.NamespaceFrom(ctx)
	var objs []interface{}
	if ns == "" {
		objs = r.vpcIndexer.List()
	} else {
		objs, _ = r.vpcIndexer.ByIndex(cloud.NetworkPolicyStatusIndexerByNamespace, ns)
	}
	vpcList := &runtimev1alpha1.VpcList{}
	for _, obj := range objs {
		vpc := obj.(*runtimev1alpha1.Vpc)
		if labelSelector.Matches(labels.Set(vpc.Labels)) {
			vpcList.Items = append(vpcList.Items, *vpc)
		}
	}
	return vpcList, nil
}

func (r *REST) NamespaceScoped() bool {
	return true
}
