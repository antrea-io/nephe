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

type CloudAccountParameters struct {
	Name      string
	Namespace string
	Provider  string
	SecretRef AccountSecretParameters
	Aws       struct {
		Region string
	}
	Azure struct {
		Location string
	}
}

type AccountSecretParameters struct {
	Name       string
	Namespace  string
	Key        string
	Credential string
}

const AWSCloudAccount = `
apiVersion: crd.cloud.antrea.io/v1alpha1
kind: CloudProviderAccount
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  awsConfig:
    region: {{.Aws.Region}}
    secretRef:
      name: {{.SecretRef.Name}}
      namespace: {{.SecretRef.Namespace}}
      key: {{.SecretRef.Key}}
`

const AzureCloudAccount = `
apiVersion: crd.cloud.antrea.io/v1alpha1
kind: CloudProviderAccount
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  azureConfig:
    region:    {{.Azure.Location}}
    secretRef:
      name: {{.SecretRef.Name}}
      namespace: {{.SecretRef.Namespace}}
      key: {{.SecretRef.Key}}
`

const AccountSecret = `
apiVersion: v1
kind: Secret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
type: Opaque
data:
  {{.Key}}: {{.Credential}}
`
