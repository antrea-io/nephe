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
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	k8stemplates "antrea.io/nephe/test/templates"
)

type awsVPC struct {
	output             map[string]interface{}
	currentAccountName string
}

// createAWSVPC creates AWS VPC that contains some VMs. It returns VPC id if successful.
func createAWSVPC(timeout time.Duration) (map[string]interface{}, error) {
	envs := []string{"TF_VAR_aws_access_key_secret",
		"TF_VAR_aws_access_key_id"}
	for _, key := range envs {
		if _, ok := os.LookupEnv(key); !ok {
			return nil, fmt.Errorf("environment variable %v not set", key)
		}
	}
	homeDir := os.Getenv("HOME")
	bin := homeDir + "/terraform/aws-tf"
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

func newAWSVPC() CloudVPC {
	vpc := &awsVPC{}
	homeDir := os.Getenv("HOME")
	bin := homeDir + "/terraform/aws-tf"
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

func (p *awsVPC) IsConfigured() bool {
	return len(p.output) > 0
}

func (p *awsVPC) GetVPCID() string {
	v, ok := p.output["vpc_id"]
	if !ok {
		return ""
	}
	vpcID := v.(map[string]interface{})
	return vpcID["value"].(string)
}

func (p *awsVPC) GetVPCName() string {
	v, ok := p.output["vpc_name"]
	if !ok {
		return ""
	}
	vpcName := v.(map[string]interface{})
	return vpcName["value"].(string)
}

func (p *awsVPC) GetCRDVPCID() string {
	return p.GetVPCID()
}

func (p *awsVPC) GetVMs() []string {
	return getListFromOutput(p.output, "vm_ids")
}

func (p *awsVPC) GetVMIDs() []string {
	return getListFromOutput(p.output, "vm_ids")
}

func (p *awsVPC) GetVMNames() []string {
	tags := p.GetTags()
	var vmNames []string
	for _, tag := range tags {
		vmNames = append(vmNames, tag["Name"])
	}
	return vmNames
}

func (p *awsVPC) GetNICs() []string {
	return getListFromOutput(p.output, "primary_nics")
}

func (p *awsVPC) GetVMIPs() []string {
	return getListFromOutput(p.output, "public_ips")
}

func (p *awsVPC) GetVMPrivateIPs() []string {
	return getListFromOutput(p.output, "private_ips")
}

func (p *awsVPC) GetTags() []map[string]string {
	return getMapFromOutput(p.output, "tags")
}

func (p *awsVPC) Delete(timeout time.Duration) error {
	homeDir := os.Getenv("HOME")
	bin := homeDir + "/terraform/aws-tf"
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

func (p *awsVPC) Reapply(timeout time.Duration) error {
	output, err := createAWSVPC(timeout)
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

func (p *awsVPC) GetCloudAccountParameters(name, namespace string, cloudCluster bool) k8stemplates.CloudAccountParameters {
	p.currentAccountName = name
	out := k8stemplates.CloudAccountParameters{
		Name:      name,
		Namespace: namespace,
		Provider:  string(v1alpha1.AWSCloudProvider),
		SecretRef: k8stemplates.AccountSecretParameters{
			Name:      name + "-aws-cred",
			Namespace: "nephe-system",
			Key:       "credential",
		},
	}
	out.Aws.Region = p.output["region"].(map[string]interface{})["value"].(string)

	cred := v1alpha1.AwsAccountCredential{}
	// use role access if cloud cluster and the role is set in env variable
	roleArn := os.Getenv("TF_VAR_nephe_controller_role_arn")
	if cloudCluster && len(roleArn) != 0 {
		cred.RoleArn = roleArn
		logf.Log.Info("Using AWS role based access")
	} else {
		cred.AccessKeyID = os.Getenv("TF_VAR_aws_access_key_id")
		cred.AccessKeySecret = os.Getenv("TF_VAR_aws_access_key_secret")
	}
	secretString, _ := json.Marshal(cred)
	out.SecretRef.Credential = string(secretString)
	return out
}

func (p *awsVPC) GetEntitySelectorParameters(name, namespace, kind string) k8stemplates.CloudEntitySelectorParameters {
	return k8stemplates.CloudEntitySelectorParameters{
		Name:             name,
		Namespace:        namespace,
		VPC:              p.GetVPCID(),
		CloudAccountName: p.currentAccountName,
		Kind:             kind,
	}
}

func (p *awsVPC) VMCmd(vm string, vmCmd []string, timeout time.Duration) (string, error) {
	var ip, user string
	for i, ivm := range p.GetVMs() {
		if vm == ivm {
			ip = p.GetVMIPs()[i]
			user = p.GetTags()[i]["Login"]
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
		fmt.Sprintf("ConnectTimeout=%v", timeout.Seconds()),
		"-o",
		"ServerAliveInterval=10",
		"-o",
		"ServerAliveCountMax=3",
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
