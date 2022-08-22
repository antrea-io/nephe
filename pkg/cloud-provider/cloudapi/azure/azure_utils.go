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

package azure

import (
	"fmt"
	"strings"
)

// nolint:unparam
func extractFieldsFromAzureResourceID(resourceID string) (string, string, string, error) {
	tokens := strings.Split(fmt.Sprintf("%q", resourceID), "/")
	var lowerCaseIDFields []string
	for _, token := range tokens {
		str1 := strings.Trim(token, "\"")
		if len(str1) > 0 {
			lowerCaseIDFields = append(lowerCaseIDFields, strings.ToLower(str1))
		}
	}
	if lowerCaseIDFields == nil || len(lowerCaseIDFields) < 8 {
		return "", "", "", fmt.Errorf("not a valid azure resource id format (%v)", resourceID)
	}
	subscriptionID := lowerCaseIDFields[1]
	resourceGroupName := lowerCaseIDFields[3]
	resourceName := lowerCaseIDFields[7]
	return subscriptionID, resourceGroupName, resourceName, nil
}

func convertStrSliceToLowercaseCommaSeparatedStr(strSlice []string) string {
	var lowerCase []string
	for _, str := range strSlice {
		if len(str) > 0 {
			lowerCase = append(lowerCase, strings.ToLower(str))
		}
	}
	if len(lowerCase) == 0 {
		return ""
	}
	tokens := strings.Split(fmt.Sprintf("%q", lowerCase), " ")
	return strings.Trim(strings.Join(tokens, ", "), "[]")
}

func mergeSet(ms ...map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	for _, m := range ms {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
