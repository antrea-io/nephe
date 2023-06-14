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

package selector

import "antrea.io/nephe/pkg/labels"

const (
	MetaName         = "metadata.name"
	MetaNamespace    = "metadata.namespace"
	StatusCloudId    = "status.cloudId"
	StatusCloudVpcId = "status.cloudVpcId"
	StatusRegion     = "status.region"

	CloudAccountName      = labels.CloudAccountName
	CloudAccountNamespace = labels.CloudAccountNamespace
)
