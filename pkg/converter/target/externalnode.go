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
	"sort"
	"strings"

	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	"antrea.io/nephe/apis/crd/v1alpha1"
)

type ExternalNodeSource interface {
	client.Object
	// GetEndPointAddresses returns IP addresses of ExternalNodeSource.
	// Passing client in case there are references needs to be retrieved from local cache.
	GetEndPointAddresses() ([]string, error)
	// GetNetworkInterfaces returns list of NetworkInterfaces of ExternalNodeSource.
	GetNetworkInterfaces() ([]v1alpha1.NetworkInterface, error)
	// GetTags returns tags of ExternalNodeSource.
	GetTags() map[string]string
	// GetLabelsFromClient returns labels specific to ExternalNodeSource.
	GetLabelsFromClient(client client.Client) map[string]string
	// Copy return a duplicate of current ExternalNodeSource.
	Copy() (duplicate interface{})
	// EmbedType returns the underlying ExternalNodeSource source resource.
	EmbedType() client.Object
}

// NewExternalNodeFrom generate a new ExternalNode from source.
func NewExternalNodeFrom(
	source ExternalNodeSource, name, namespace string, cl client.Client, scheme *runtime.Scheme) *antreav1alpha1.ExternalNode {
	externalNode := &antreav1alpha1.ExternalNode{}
	populateExternalNodeFrom(source, externalNode, cl)
	externalNode.SetName(name)
	externalNode.SetNamespace(namespace)
	accessor, _ := meta.Accessor(source.EmbedType())
	if err := ctrl.SetControllerReference(accessor, externalNode, scheme); err != nil {
		externalNode.SetName(name)
		externalNode.SetNamespace(namespace)
	}
	return externalNode
}

// PatchExternalNodeFrom generate a patch for existing ExternalNode from source.
func PatchExternalNodeFrom(
	source ExternalNodeSource, patch *antreav1alpha1.ExternalNode, cl client.Client) (*antreav1alpha1.ExternalNode, bool) {
	changed := populateExternalNodeFrom(source, patch, cl)
	return patch, changed
}

func populateExternalNodeFrom(source ExternalNodeSource, externalNode *antreav1alpha1.ExternalNode, cl client.Client) bool {
	changed := false
	if !reflect.DeepEqual(externalNode.GetLabels(), genTargetEntityLabels(source, cl)) {
		externalNode.SetLabels(genTargetEntityLabels(source, cl))
		changed = true
	}
	interfaces, _ := source.GetNetworkInterfaces()
	networkInterface := make([]antreav1alpha1.NetworkInterface, 0, len(interfaces))
	for _, intf := range interfaces {
		var ips []string
		for _, ip := range intf.IPs {
			ips = append(ips, ip.Address)
		}
		networkInterface = append(networkInterface, antreav1alpha1.NetworkInterface{
			Name: intf.Name,
			IPs:  ips,
		})
	}
	sortNetworkInterface(externalNode.Spec.Interfaces)
	sortNetworkInterface(networkInterface)
	if !reflect.DeepEqual(externalNode.Spec.Interfaces, networkInterface) {
		externalNode.Spec.Interfaces = networkInterface
		changed = true
	}
	return changed
}

func sortNetworkInterface(networkInterface []antreav1alpha1.NetworkInterface) {
	sort.Slice(networkInterface, func(i, j int) bool {
		if networkInterface[i].Name == networkInterface[j].Name {
			return slices.Compare(networkInterface[i].IPs, networkInterface[j].IPs) < 0
		}
		return strings.Compare(networkInterface[i].Name, networkInterface[j].Name) < 0
	})
}
