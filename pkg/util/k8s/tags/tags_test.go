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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTags(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Import Tags")
}

var _ = Describe("Import Tags", func() {
	Context("Validate tags", func() {
		var (
			nameLongerThanMaxSupported = "aaaaaaaaaaAAAAAAAAAA0000000000bbbbbbbbbbBBBBBBBBBB11111111112222"
			nameFormatNotComplaint     = "ab/01"
			key                        = "key01"
			value                      = "value01"
		)
		It("Tag key is more than 63 characters long", func() {
			tags := map[string]string{
				nameLongerThanMaxSupported: value,
			}
			importedTags := ImportTags(tags)
			Expect(importedTags).Should(HaveLen(0))
		})
		It("Tag key is not compliant to kubernetes label key format", func() {
			tags := map[string]string{
				nameFormatNotComplaint: value,
			}
			importedTags := ImportTags(tags)
			Expect(importedTags).Should(HaveLen(0))
		})
		It("Tag value is more than 63 characters long", func() {
			tags := map[string]string{
				key: nameLongerThanMaxSupported,
			}
			importedTags := ImportTags(tags)
			Expect(importedTags).Should(HaveLen(0))
		})
		It("Tag value is not compliant to kubernetes label value format", func() {
			tags := map[string]string{
				key: nameFormatNotComplaint,
			}
			importedTags := ImportTags(tags)
			Expect(importedTags).Should(HaveLen(0))
		})
		It("Tag key value are compliant to kubernetes label format", func() {
			tags := map[string]string{
				key: value,
			}
			importedTags := ImportTags(tags)
			Expect(importedTags).Should(HaveLen(len(tags)))
		})
	})
})
