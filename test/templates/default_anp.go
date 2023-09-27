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

type DefaultANPParameters struct {
	Namespace string
	Name      string
	Priority  int
	Entity    *EntitySelectorParameters
}

const DefaultANPAgentedSetup = `
apiVersion: crd.antrea.io/v1alpha1
kind: NetworkPolicy
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  priority: 2
  appliedTo:
    - externalEntitySelector:
        matchLabels:
          {{.Entity.Kind}}
  ingress:
    - action: Drop
      ports:
        - protocol: TCP
          port: 8080
      from:
        - ipBlock:
            cidr: 0.0.0.0/0
  egress:
    - action: Drop
      ports:
        - protocol: TCP
          port: 8080
      to:
        - ipBlock:
            cidr: 0.0.0.0/0
`
const DefaultANPSetup = `
apiVersion: crd.antrea.io/v1alpha1
kind: NetworkPolicy
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  tier: application
  priority: {{.Priority}}
  appliedTo:
    - externalEntitySelector:
        matchLabels:
          {{.Entity.Kind}}
  ingress:
    - action: Drop
      from:
        - ipBlock:
            cidr: 0.0.0.0/0
  egress:
    - action: Drop
      to:
        - ipBlock:
            cidr: 0.0.0.0/0
`
