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

type EntitySelectorParameters struct {
	Kind              string
	CloudInstanceName string
	VPC               string
	Tags              map[string]string
}

type NamespaceParameters struct {
	Labels map[string]string
}

type ToFromParameters struct {
	Entity    *EntitySelectorParameters
	Namespace *NamespaceParameters
	IPBlock   string
	Ports     []*PortParameters
	DenyAll   bool
}

type PortParameters struct {
	Protocol string
	Port     string
}

type ANPParameters struct {
	Name         string
	Namespace    string
	To           *ToFromParameters
	From         *ToFromParameters
	AppliedTo    *EntitySelectorParameters
	FederatedKey *string
}

const CloudAntreaNetworkPolicy = `
apiVersion: crd.antrea.io/v1alpha1
kind: NetworkPolicy
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
{{ if .FederatedKey }}
  annotations:
    {{.FederatedKey}}: "true"
{{ end }}
spec:
  priority: 1
  appliedTo:
{{- if  .AppliedTo }}
  - externalEntitySelector:
      matchLabels:
{{- if .AppliedTo.Kind }}
        kind.nephe: {{.AppliedTo.Kind}}
{{ end }}
{{- if .AppliedTo.CloudInstanceName }}
        name.nephe: {{.AppliedTo.CloudInstanceName }}
{{ end }}
{{- if .AppliedTo.VPC }}
        vpc.nephe: {{ .AppliedTo.VPC }}
{{ end }}
{{- range $k, $v := .AppliedTo.Tags }}
        {{$k}}.tag.nephe: {{$v}}
{{ end }}
{{ end }} {{- /* .AppliedTo */}}
{{- if .From }}
{{- if  .From.DenyAll }}
  ingress: []
{{ else }}
  ingress:
  - action: Allow
    from: 
{{ end }}
{{- if .From.IPBlock }}
      - ipBlock:
          cidr: {{.From.IPBlock}}
{{ end }}
{{- if .From.Entity }}
      - externalEntitySelector:
          matchLabels:
{{- if .From.Entity.Kind }}
            kind.nephe: {{.From.Entity.Kind}}
{{ end }}
{{- if .From.Entity.CloudInstanceName }}
            name.nephe: {{ .From.Entity.CloudInstanceName }}
{{ end }}
{{- if .From.Entity.VPC }}
            vpc.nephe: {{ .From.Entity.VPC }}
{{ end }}
{{- range $k, $v := .From.Entity.Tags }}
            {{$k}}.tag.nephe: {{$v}}
{{ end }}
{{ end }} {{/*.From.Entity */}}
{{- if .From.Namespace }}
        namespaceSelector:
          matchLabels:
{{- range $k, $v := .From.Namespace.Labels }}
            {{$k}}: {{$v}}
{{ end }}
{{ end }} {{/* .From.Namespace */}}
{{- if .From.Ports }}
    ports:
{{- range $port := .From.Ports }}
    - protocol: {{$port.Protocol}}
      port: {{$port.Port}}
{{ end }}
{{- end }}{{/* .From.Ports */}}
{{ end }} {{/* .From */}}
{{- if .To }}
{{- if .To.DenyAll }}
  egress: []
{{ else }}
  egress:
  - action: Allow
    to:
{{ end }}
{{- if .To.IPBlock }}
      - ipBlock:
          cidr: {{.To.IPBlock}}
{{ end }}
{{- if .To.Entity }}
      - externalEntitySelector:
{{- if .To.Entity.Kind }}
          matchLabels:
            kind.nephe: {{.To.Entity.Kind}}
{{ end }}
{{- if .To.Entity.CloudInstanceName }}
            name.nephe: {{ .To.Entity.CloudInstanceName }}
{{ end }}
{{- if .To.Entity.VPC }}
            vpc.nephe: {{ .To.Entity.VPC }}
{{ end }}
{{- range $k, $v := .To.Entity.Tags }}
            {{$k}}.tag.nephe: {{$v}}
{{ end }}
{{ end }} {{/* .To.Entity */}}
{{- if .To.Namespace }}
        namespaceSelector:
          matchLabels:
{{- range $k, $v := .To.Namespace.Labels }}
            {{$k}}: {{$v}}
{{ end }}
{{ end }} {{/* .To.Namespace */}}
{{- if .To.Ports }}
    ports:
{{- range $port := .To.Ports }}
    - protocol: {{$port.Protocol}}
      port: {{$port.Port}}
{{ end }}
{{- end }}{{/* .To.Ports */}}
{{ end }} {{/* .To */}}
`
