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

package tags

import (
	"regexp"

	"antrea.io/nephe/pkg/labels"
)

const (
	LabelSizeLimit    = 63
	TagCharacterClass = `^[a-zA-Z0-9][\.\-a-zA-Z0-9_]*[a-zA-Z0-9]$`
)

// ImportTags function returns tags which comply with kubernetes labels format.
func ImportTags(tags map[string]string) map[string]string {
	importedTags := make(map[string]string)
	// Tags are used as kubernetes labels in External Entity in nephe.antrea.io/tag-<tag key>=<tag value> format.
	// Filter out the tags which don't comply with kubernetes label format.
	// For Label format, refer to https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set.
	for key, value := range tags {
		if len(key) > (LabelSizeLimit - len(labels.ExternalEntityLabelKeyTagPrefix)) {
			continue
		}
		regExp, err := regexp.Compile(TagCharacterClass)
		if err != nil {
			continue
		}
		if matched := regExp.MatchString(key); !matched {
			continue
		}

		if len(value) > LabelSizeLimit {
			continue
		}
		if len(value) > 0 {
			if matched := regExp.MatchString(value); !matched {
				continue
			}
		}

		importedTags[key] = value
	}

	return importedTags
}
