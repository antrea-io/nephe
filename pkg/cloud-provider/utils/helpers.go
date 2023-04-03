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

package utils

import (
	"fmt"
	"strings"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
)

func GenerateShortResourceIdentifier(id string, prefixToAdd string) string {
	idTrim := strings.Trim(id, " ")
	if len(idTrim) == 0 {
		return ""
	}

	// Ascii value of the characters will be added to generate unique name
	var sum uint32 = 0
	for _, value := range strings.ToLower(idTrim) {
		sum += uint32(value)
	}

	str := fmt.Sprintf("%v-%v", strings.ToLower(prefixToAdd), sum)
	return str
}

// GetCloudResourceCRName gets corresponding cr name from cloud resource id based on cloud type.
func GetCloudResourceCRName(providerType, name string) string {
	switch providerType {
	case string(runtimev1alpha1.AWSCloudProvider):
		return name
	case string(runtimev1alpha1.AzureCloudProvider):
		tokens := strings.Split(name, "/")
		return GenerateShortResourceIdentifier(name, tokens[len(tokens)-1])
	default:
		return name
	}
}
