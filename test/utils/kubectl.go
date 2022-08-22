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
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

type KubeCtl struct {
	config  string
	context string
}

func NewKubeCtl(config string) (*KubeCtl, error) {
	kubectl := &KubeCtl{config: config}
	if err := kubectl.IsPresent(); err != nil {
		return nil, err
	}
	return kubectl, nil
}

func (c *KubeCtl) getCommand(ins ...string) *exec.Cmd {
	var args []string
	if len(c.config) > 0 {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", c.config))
	}
	if len(c.context) > 0 {
		args = append(args, "--context", c.context)
	}
	args = append(args, ins...)
	return exec.Command("kubectl", args...)
}

func (c *KubeCtl) SetContext(clusterCtx string) {
	c.context = clusterCtx
}

// IsPresent returns error if kubectl cannot connect to K8s.
func (c *KubeCtl) IsPresent() error {
	cmd := c.getCommand("version")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kubeclt check failed, err %w, cmd %v, output %v", err, cmd.String(), string(output))
	}
	return nil
}

// Apply calls "kubectl apply".
func (c *KubeCtl) Apply(path string, content []byte) error {
	if err := c.IsPresent(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if len(path) == 0 {
		cmd = c.getCommand("apply", "-f", "-")
		cmd.Stdin = bytes.NewReader(content)
	} else {
		cmd = c.getCommand("apply", "-f", path)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmd [%v] failed, err %w, output %s", cmd.String(), err, string(output))
	}
	return nil
}

// Delete calls "kubectl delete".
func (c *KubeCtl) Delete(path string, content []byte) error {
	if err := c.IsPresent(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if len(path) == 0 {
		cmd = c.getCommand("delete", "-f", "-")
		cmd.Stdin = bytes.NewReader(content)
	} else {
		cmd = c.getCommand("delete", "-f", path)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmd [%v] failed, err %w, output %s", cmd.String(), err, string(output))
	}
	return nil
}

// Patch calls "kubectl patch".
func (c *KubeCtl) Patch(path string, content []byte, kind, name string) error {
	if err := c.IsPresent(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	if len(path) == 0 {
		cmd = c.getCommand("patch", kind, name, "-p", string(content))
	} else {
		cmd = c.getCommand("patch", kind, name, "-p", fmt.Sprintf("$(cat %s", path))
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmd [%v] failed, err %w, output %s", cmd.String(), err, string(output))
	}
	return nil
}

// Cmd calls arbitrary kubectl commands.
func (c *KubeCtl) Cmd(inCmd string) (string, error) {
	if err := c.IsPresent(); err != nil {
		return "", err
	}
	cmdTokens := strings.Fields(inCmd)
	cmd := c.getCommand(cmdTokens...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cmd [%v] failed, err %w, output %s", cmd.String(), err, string(output))
	}
	return string(output), nil
}

// PodCmd execute podCmd on pod.
func (c *KubeCtl) PodCmd(pod *types.NamespacedName, podCmd []string, timeout time.Duration) (string, error) {
	args := []string{
		"exec",
		pod.Name,
		"-n",
		pod.Namespace,
		"--",
		"timeout",
		fmt.Sprint(timeout.Seconds()),
	}

	args = append(args, podCmd...)
	cmd := c.getCommand(args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("execute cmd [%s] on pod %s failed: err %w, output %s", cmd.String(), pod.String(), err, output)
	}
	return string(output), nil
}
