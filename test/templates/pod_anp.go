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

type PodANPParameters struct {
	Name        string
	Namespace   string
	PodSelector string
	To          *ToFromParameters
	Port        *PortParameters
	Action      string
}

const PodAntreaNetworkPolicy = `
apiVersion: crd.antrea.io/v1alpha1
kind: NetworkPolicy
metadata:
  name: {{.Name}}
  namespace : {{.Namespace}}
spec:
  priority: 1
  appliedTo:
  - podSelector:
      matchLabels:
        app: {{.PodSelector}}
  egress:
{{ if .To }}
  - action: {{ .Action }}
    to:
{{ if .To.IPBlock }}
    - ipBlock:
        cidr: {{.To.IPBlock}}
{{ end }}
{{ if .To.Entity }}
    - externalEntitySelector:
        matchLabels:
{{ if .To.Entity.Kind }}
          kind.nephe: {{.To.Entity.Kind}}
{{ end }}
{{ if .To.Entity.CloudInstanceName }}
          name.nephe: {{ .To.Entity.CloudInstanceName }}
{{ end }}
{{ if .To.Entity.VPC }}
          vpc.nephe: {{ .To.Entity.VPC }}
{{ end }}
{{ range $k, $v := .To.Entity.Tags }}
          {{$k}}.tag.nephe: {{$v}}
{{ end }}
{{ end }}{{/* .To.Entity */}}
{{ if .To.Namespace }}
      namespaceSelector:
        matchLabels:
{{ range $k, $v := .To.Namespace.Labels }}
          {{$k}}: {{$v}}
{{ end }}
{{ end }}{{/* .To.Namespace */}}
{{ end }} {{/* .To */}}
{{ if .Port }}
    ports:
      - protocol: {{.Port.Protocol}}
{{ if .Port.Port }}
        port: {{.Port.Port}}
{{ end }}
{{ end }} {{/* .Port */}}
  - action: Allow
    to:
    - podSelector: {}
      namespaceSelector: {}
  - action: Drop
    to:
    - ipBlock:
        cidr: 0.0.0.0/0

`
