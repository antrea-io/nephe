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

package target

import (
	"reflect"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	"antrea.io/nephe/pkg/controllers/config"
)

const (
	// LabelSizeLimit K8s label requirements, https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/.
	LabelSizeLimit  = 64
	LabelExpression = "[^a-zA-Z0-9_-]+"
)

type ExternalEntitySource interface {
	client.Object
	// GetEndPointAddresses returns IP addresses of ExternalEntitySource.
	// Passing client in case there are references needs to be retrieved from local cache.
	GetEndPointAddresses() ([]string, error)
	// GetEndPointPort returns port and port name, if applicable, of ExternalEntitySource.
	GetEndPointPort(client client.Client) []antreatypes.NamedPort
	// GetTags returns tags of ExternalEntitySource.
	GetTags() map[string]string
	// GetLabelsFromClient returns labels specific to ExternalEntitySource.
	GetLabelsFromClient(client client.Client) map[string]string
	// GetExternalNode returns external node/controller associated with this ExternalEntitySource.
	GetExternalNode(client client.Client) string
	// Copy return a duplicate of current ExternalEntitySource.
	Copy() ExternalEntitySource
	// EmbedType returns the underlying ExternalEntitySource source resource.
	EmbedType() client.Object
}

// GetExternalEntityLabelKind returns value of ExternalEntity kind label.
func GetExternalEntityLabelKind(obj runtime.Object) string {
	return strings.ToLower(reflect.TypeOf(obj).Elem().Name())
}

func GetExternalEntityName(obj runtime.Object) string {
	access, _ := meta.Accessor(obj)
	return strings.ToLower(reflect.TypeOf(obj).Elem().Name()) + "-" + access.GetName()
}

func GetObjectKeyFromSource(source ExternalEntitySource) client.ObjectKey {
	access, _ := meta.Accessor(source)
	return client.ObjectKey{Namespace: access.GetNamespace(),
		Name: GetExternalEntityName(source.EmbedType())}
}

// NewExternalEntityFrom generate a new ExternalEntity from source.
func NewExternalEntityFrom(
	source ExternalEntitySource, name, namespace string, cl client.Client, scheme *runtime.Scheme) *antreatypes.ExternalEntity {
	externEntity := &antreatypes.ExternalEntity{}
	PopulateExternalEntityFrom(source, externEntity, cl)
	externEntity.SetName(name)
	externEntity.SetNamespace(namespace)
	accessor, err := meta.Accessor(source.EmbedType())
	if err != nil {
		externEntity.SetName(name)
		externEntity.SetNamespace(namespace)
	}
	if err = ctrl.SetControllerReference(accessor, externEntity, scheme); err != nil {
		externEntity.SetName(name)
		externEntity.SetNamespace(namespace)
	}
	return externEntity
}

// PatchExternalEntityFrom generate a patch for existing ExternalEntity from source.
func PatchExternalEntityFrom(
	source ExternalEntitySource, patch *antreatypes.ExternalEntity, cl client.Client) *antreatypes.ExternalEntity {
	PopulateExternalEntityFrom(source, patch, cl)
	return patch
}

func PopulateExternalEntityFrom(source ExternalEntitySource, externEntity *antreatypes.ExternalEntity, cl client.Client) {
	labels := make(map[string]string)
	accessor, _ := meta.Accessor(source)
	labels[config.ExternalEntityLabelKeyKind] = GetExternalEntityLabelKind(source.EmbedType())
	labels[config.ExternalEntityLabelKeyName] = strings.ToLower(accessor.GetName())
	labels[config.ExternalEntityLabelKeyNamespace] = strings.ToLower(accessor.GetNamespace())
	for key, val := range source.GetLabelsFromClient(cl) {
		labels[key] = val
	}
	for key, val := range source.GetTags() {
		reg, _ := regexp.Compile(LabelExpression)
		fkey := reg.ReplaceAllString(key, "") + config.ExternalEntityLabelKeyTagPostfix
		if len(fkey) > LabelSizeLimit {
			fkey = fkey[:LabelSizeLimit]
		}
		fval := reg.ReplaceAllString(val, "")
		if len(fval) > LabelSizeLimit {
			fval = fval[:LabelSizeLimit]
		}
		labels[strings.ToLower(fkey)] = strings.ToLower(fval)
	}
	externEntity.SetLabels(labels)

	ipAddrs, _ := source.GetEndPointAddresses()
	endpoints := make([]antreatypes.Endpoint, 0, len(ipAddrs))
	for _, ip := range ipAddrs {
		endpoints = append(endpoints, antreatypes.Endpoint{IP: ip})
	}
	externEntity.Spec.Endpoints = endpoints
	externEntity.Spec.Ports = source.GetEndPointPort(cl)
	externEntity.Spec.ExternalNode = source.GetExternalNode(cl)
}
