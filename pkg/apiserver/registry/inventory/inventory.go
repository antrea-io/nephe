// Copyright 2023 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inventory

import (
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/labels"
)

const (
	MetaName         = "metadata.name"
	MetaNamespace    = "metadata.namespace"
	StatusCloudId    = "status.cloudId"
	StatusCloudVpcId = "status.cloudVpcId"
	StatusRegion     = "status.region"

	CloudAccountName      = labels.CloudAccountName
	CloudAccountNamespace = labels.CloudAccountNamespace
)

// GetFieldSelectors extracts and populates the field selectors map from the list options.
func GetFieldSelectors(options *internalversion.ListOptions, selectors map[string]string,
	fieldKeysMap map[string]struct{}) error {
	if options != nil && options.FieldSelector != nil && options.FieldSelector.String() != "" {
		fieldSelectorStrings := strings.Split(options.FieldSelector.String(), ",")
		for _, fieldSelectorString := range fieldSelectorStrings {
			fieldKeyAndValue := strings.Split(fieldSelectorString, "=")
			_, exist := fieldKeysMap[fieldKeyAndValue[0]]
			if !exist {
				return errors.NewBadRequest("unsupported field selector")
			}
			selectors[fieldKeyAndValue[0]] = fieldKeyAndValue[1]
		}
	}
	return nil
}

// GetLabelSelectors extracts and populates the label selectors map from the list options.
func GetLabelSelectors(options *internalversion.ListOptions, selectors map[string]string,
	supportedLabelNameMap map[string]struct{}) error {
	if options != nil && options.LabelSelector != nil && options.LabelSelector.String() != "" {
		labelSelectorStrings := strings.Split(options.LabelSelector.String(), ",")
		for _, labelSelectorString := range labelSelectorStrings {
			labelKeyAndValue := strings.Split(labelSelectorString, "=")
			_, exists := supportedLabelNameMap[labelKeyAndValue[0]]
			if !exists {
				return errors.NewBadRequest("unsupported label selector")
			}
			selectors[labelKeyAndValue[0]] = labelKeyAndValue[1]
		}
	}
	return nil
}

// GetFilteredObjsByLabels filters the objs based on the labelSelector and returns a list.
func GetFilteredObjsByLabels(objs []interface{}, labelSelector map[string]string,
	supportedLabelNameMap map[string]struct{}) []interface{} {
	if len(labelSelector) == 0 {
		return objs
	}

	var ret []interface{}
	for _, obj := range objs {
		skipObj := false
		objLabels := getObjLabels(obj)
		for name := range supportedLabelNameMap {
			if !matchLabel(objLabels, labelSelector, name) {
				// If any of the labels are not matched, skip that particular object.
				skipObj = true
				break
			}
		}
		if !skipObj {
			ret = append(ret, obj)
		}
	}
	return ret
}

// getObjLabels returns the labels from the object.
func getObjLabels(obj interface{}) map[string]string {
	if reflect.TypeOf(obj).Elem().Name() == reflect.TypeOf(&runtimev1alpha1.Vpc{}).Elem().Name() {
		return obj.(*runtimev1alpha1.Vpc).Labels
	} else if reflect.TypeOf(obj).Elem().Name() == reflect.TypeOf(&runtimev1alpha1.VirtualMachine{}).Elem().Name() {
		return obj.(*runtimev1alpha1.VirtualMachine).Labels
	}
	return nil
}

// matchLabel matches the objects label with the provided selector and returns
// true if a match is found.
func matchLabel(vpcLabels, selector map[string]string, name string) bool {
	if selector[name] != "" {
		value, ok := vpcLabels[name]
		if !ok {
			return false
		}
		if value != selector[name] {
			return false
		}
	}
	return true
}
