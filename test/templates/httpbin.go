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

type ServiceParameters struct {
	DeploymentParameters
	Type           *string
	FedServiceName *string
	FedServiceKey  string
	Port           string
}

const HTTPBinService = `
apiVersion: v1
kind: Service
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app: httpbin
{{ if .FedServiceName }}
  annotations:
    {{.FedServiceKey}}: {{.FedServiceName}}
{{ end }}
spec:
{{ if .Type }}
  type: {{.Type}}
{{ end }}
  ports:
    - name: http
      port: {{ .Port }}
      targetPort: 80
  selector:
    app: httpbin
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  replicas: {{.Replicas}}
  selector:
    matchLabels:
      app: httpbin
      version: v1
  template:
    metadata:
      labels:
        app: httpbin
        version: v1
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - httpbin
              topologyKey: "kubernetes.io/hostname"
      containers:
        - image: kennethreitz/httpbin
          imagePullPolicy: Never
          name: {{.Name}}
          ports:
            - containerPort: 80
          livenessProbe:
            httpGet:
              path: /status/200
              port: 80
            initialDelaySeconds: 3
            periodSeconds: 3
      terminationGracePeriodSeconds: 0
`
