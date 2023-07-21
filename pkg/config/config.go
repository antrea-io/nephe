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

package config

const (
	DefaultCloudResourcePrefix = "nephe"
	DefaultCloudSyncInterval   = 300
	MinimumCloudSyncInterval   = 60
)

type ControllerConfig struct {
	CloudResourcePrefix string `yaml:"cloudResourcePrefix,omitempty"`
	CloudSyncInterval   int64  `yaml:"cloudSyncInterval,omitempty"`
	// AntreaKubeconfig The path to access the kubeconfig file used in the connection to Antrea Controller.
	AntreaKubeconfig string `yaml:"antreaKubeconfig,omitempty"`
}
