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

package templates

type CloudEntitySelectorParameters struct {
	Name                  string
	Namespace             string
	CloudAccountName      string
	CloudAccountNamespace string
	Selector              *SelectorParameters
	Kind                  string
	Agented               bool
}

type SelectorParameters struct {
	VPC string
	VMs []string
}

const CloudEntitySelector = `
apiVersion: crd.cloud.antrea.io/v1alpha1
kind: CloudEntitySelector
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  accountName: {{.CloudAccountName}}
  accountNamespace: {{.CloudAccountNamespace}}
  vmSelector:
{{- if .Selector.VPC }}
    - vpcMatch:
        matchID: {{.Selector.VPC}}
{{ else }}
    - vmMatch:
{{- range $vm := .Selector.VMs }}
        - matchID: {{$vm}}
{{ end }}
{{ end }}{{/* .Selector.VPC */}}
      agented: {{.Agented}}
`
