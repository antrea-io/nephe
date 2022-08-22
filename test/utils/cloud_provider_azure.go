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

package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/utils"
	k8stemplates "antrea.io/nephe/test/templates"
)

type azureVPC struct {
	output             map[string]interface{}
	currentAccountName string
}

// createAzureSVPC creates Azure VPC that contains some VMs. It returns VPC id if successful.
func createAzureVPC(timeout time.Duration) (map[string]interface{}, error) {
	envs := []string{"TF_VAR_azure_client_secret", "TF_VAR_azure_client_id",
		"TF_VAR_azure_client_subscription_id", "TF_VAR_azure_client_tenant_id"}
	for _, key := range envs {
		if _, ok := os.LookupEnv(key); !ok {
			return nil, fmt.Errorf("environment variable %v not set", key)
		}
	}
	homeDir := os.Getenv("HOME")
	bin := homeDir + "/terraform/azure-tf"
	_, err := os.Stat(bin)
	if err != nil {
		return nil, fmt.Errorf("%v not found, %v", bin, err)
	}
	args := []string{
		"-s", "SIGKILL",
		fmt.Sprint(timeout.Seconds()),
		bin,
		"create",
	}
	cmd := exec.Command("timeout", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("create failed (%v): %v, %v", args, err, string(output))
	}
	cmd = exec.Command(bin, "output", "-json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("output failed: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %v", err)
	}
	return result, nil
}

func newAzureVPC() CloudVPC {
	vpc := &azureVPC{}
	homeDir := os.Getenv("HOME")
	bin := homeDir + "/terraform/azure-tf"
	_, err := os.Stat(bin)
	if err != nil {
		return vpc
	}
	cmd := exec.Command(bin, "output", "-json")
	if output, err := cmd.Output(); err == nil {
		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err == nil {
			vpc.output = result
		}
	}
	return vpc
}

func (p *azureVPC) IsConfigured() bool {
	return len(p.output) > 0
}

func (p *azureVPC) GetVPCID() string {
	v, ok := p.output["vnet_id"]
	if !ok {
		return ""
	}
	vpcID := v.(map[string]interface{})
	return vpcID["value"].(string)
}

func (p *azureVPC) GetVPCName() string {
	v, ok := p.output["vnet_name"]
	if !ok {
		return ""
	}
	vpcName := v.(map[string]interface{})
	return vpcName["value"].(string)
}

func (p *azureVPC) GetCRDVPCID() string {
	vpc := p.GetVPCID()
	if len(vpc) == 0 {
		return vpc
	}
	tokens := strings.Split(vpc, "/")
	return utils.GenerateShortResourceIdentifier(vpc, tokens[len(tokens)-1])
}

func (p *azureVPC) GetVMs() []string {
	ids := getListFromOutput(p.output, "vm_ids")
	var shortids []string
	for _, id := range ids {
		tokens := strings.Split(id, "/")
		name := tokens[len(tokens)-1]
		shortids = append(shortids, utils.GenerateShortResourceIdentifier(id, name))
	}
	return shortids
}

func (p *azureVPC) GetVMIDs() []string {
	return getListFromOutput(p.output, "vm_ids")
}

func (p *azureVPC) GetVMNames() []string {
	ids := getListFromOutput(p.output, "vm_ids")
	var vmNames []string
	for _, id := range ids {
		tokens := strings.Split(id, "/")
		name := tokens[len(tokens)-1]
		vmNames = append(vmNames, name)
	}
	return vmNames
}

func (p *azureVPC) GetNICs() []string {
	ids := getListFromOutput(p.output, "primary_nics")
	var shortids []string
	for _, id := range ids {
		tokens := strings.Split(id, "/")
		name := tokens[len(tokens)-1]
		shortids = append(shortids, utils.GenerateShortResourceIdentifier(id, name))
	}
	return shortids
}

func (p *azureVPC) GetVMIPs() []string {
	return getListFromOutput(p.output, "public_ips")
}

func (p *azureVPC) GetVMPrivateIPs() []string {
	return getListFromOutput(p.output, "private_ips")
}

func (p *azureVPC) GetTags() []map[string]string {
	return getMapFromOutput(p.output, "tags")
}

func (p *azureVPC) Delete(timeout time.Duration) error {
	homeDir := os.Getenv("HOME")
	bin := homeDir + "/terraform/azure-tf"
	_, err := os.Stat(bin)
	if err != nil {
		return fmt.Errorf("%v not found, %v", bin, err)
	}
	args := []string{
		"-s", "SIGKILL",
		fmt.Sprint(timeout.Seconds()),
		bin,
		"destroy",
	}
	cmd := exec.Command("timeout", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("destroy failed: %v", err)
	}
	return nil
}

func (p *azureVPC) Reapply(timeout time.Duration) error {
	output, err := createAzureVPC(timeout)
	if err != nil {
		return err
	}
	p.output = output
	// Wait for servers on VMs come to live.
	err = wait.Poll(time.Second*5, time.Second*240, func() (bool, error) {
		for _, ip := range p.GetVMIPs() {
			cmd := exec.Command("timeout", []string{"5", "curl", "http://" + ip}...)
			if _, err = cmd.CombinedOutput(); err != nil {
				return false, nil
			}
		}
		return true, nil
	})
	return err
}

func (p *azureVPC) GetCloudAccountParameters(name, namespace string, _ bool) k8stemplates.CloudAccountParameters {
	p.currentAccountName = name
	out := k8stemplates.CloudAccountParameters{
		Name:      name,
		Namespace: namespace,
		Provider:  string(v1alpha1.AzureCloudProvider),
		SecretRef: k8stemplates.AccountSecretParameters{
			Name:      name + "-azure-cred",
			Namespace: "nephe-system",
			Key:       "credential",
		},
	}
	out.Azure.Location = strings.ReplaceAll(p.output["location"].(map[string]interface{})["value"].(string), " ", "")

	cred := v1alpha1.AzureAccountCredential{
		SubscriptionID: os.Getenv("TF_VAR_azure_client_subscription_id"),
		ClientID:       os.Getenv("TF_VAR_azure_client_id"),
		TenantID:       os.Getenv("TF_VAR_azure_client_tenant_id"),
		ClientKey:      os.Getenv("TF_VAR_azure_client_secret"),
	}
	secretString, _ := json.Marshal(cred)
	out.SecretRef.Credential = string(secretString)
	return out
}

func (p *azureVPC) GetEntitySelectorParameters(name, namespace, kind string) k8stemplates.CloudEntitySelectorParameters {
	return k8stemplates.CloudEntitySelectorParameters{
		Name:             name,
		Namespace:        namespace,
		VPC:              p.GetVPCID(),
		CloudAccountName: p.currentAccountName,
		Kind:             kind,
	}
}

func (p *azureVPC) VMCmd(vm string, vmCmd []string, timeout time.Duration) (string, error) {
	var ip, user string
	for i, ivm := range p.GetVMs() {
		if vm == ivm {
			ip = p.GetVMIPs()[i]
			user = "azureuser"
			break
		}
	}
	if len(ip) == 0 {
		return "", fmt.Errorf("unknown VM %v", vm)
	}
	args := []string{
		"-t",
		"-o",
		"StrictHostKeyChecking=no",
		"-o",
		"ServerAliveInterval=10",
		"-o",
		"ServerAliveCountMax=3",
		"-o",
		fmt.Sprintf("ConnectTimeout=%v", timeout.Seconds()),
		fmt.Sprintf("%v@%v", user, ip),
	}
	args = append(args, vmCmd...)
	cmd := exec.Command("ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("execute cmd [%s] on VM %s failed: err %w, output %s", cmd.String(), vm, err, output)
	}
	return string(output), nil
}
