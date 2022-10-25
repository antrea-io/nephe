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
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"text/template"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	k8stemplates "antrea.io/nephe/test/templates"
)

// RestartOrWaitDeployment restarts an existing deployment.
func RestartOrWaitDeployment(k8sClient client.Client, name, namespace string, timeout time.Duration, restart bool) error {
	if !restart {
		return StartOrWaitDeployment(k8sClient, name, namespace, 0, timeout)
	}
	replicas, err := StopDeployment(k8sClient, name, namespace, timeout)
	if err != nil {
		return err
	}
	if err := StartOrWaitDeployment(k8sClient, name, namespace, replicas, timeout); err != nil {
		return err
	}
	return nil
}

// StartOrWaitDeployment start a stopped deployment with number of replicas.
// Or wait for the deployment to complete if replicas is 0.
func StartOrWaitDeployment(k8sClient client.Client, name, namespace string, replicas int32, timeout time.Duration) error {
	dep := &v1.Deployment{}
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k8sClient.Get(context.TODO(), key, dep); err != nil {
		return err
	}
	if replicas != 0 {
		if !(dep.Spec.Replicas == nil || *dep.Spec.Replicas == 0) {
			return fmt.Errorf("deployment is not in stopped state")
		}
		dep.Spec.Replicas = &replicas
		if err := k8sClient.Update(context.TODO(), dep); err != nil {
			return err
		}
	} else {
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas == 0 {
			return fmt.Errorf("empty replicas in deployment")
		}
	}
	if err := wait.Poll(time.Second, timeout, func() (bool, error) {
		dep := &v1.Deployment{}
		if err := k8sClient.Get(context.TODO(), key, dep); err != nil {
			return false, err
		}
		if dep.Status.ReadyReplicas != *dep.Spec.Replicas {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}
	// Give time for deployment to re-discover.
	time.Sleep(time.Second * 2)
	return nil
}

// StopDeployment stops an deployment..
func StopDeployment(k8sClient client.Client, name, namespace string, timeout time.Duration) (int32, error) {
	dep := &v1.Deployment{}
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k8sClient.Get(context.TODO(), key, dep); err != nil {
		return -1, err
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas == 0 {
		return -1, fmt.Errorf("deployment is already stopped")
	}
	replicas := *dep.Spec.Replicas
	*dep.Spec.Replicas = 0
	if err := k8sClient.Update(context.TODO(), dep); err != nil {
		return -1, err
	}
	if err := wait.Poll(time.Second, timeout, func() (bool, error) {
		dep := &v1.Deployment{}
		if err := k8sClient.Get(context.TODO(), key, dep); err != nil {
			return false, err
		}
		if dep.Status.ReadyReplicas != 0 {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return -1, err
	}
	return replicas, nil
}

// ConfigureK8s create or delete an configuration in yaml.
func ConfigureK8s(kubeCtl *KubeCtl, params interface{}, yaml string, isDelete bool) error {
	confParser, err := template.New("").Parse(yaml)
	if err != nil {
		return err
	}
	conf := bytes.NewBuffer(nil)
	if err := confParser.Execute(conf, params); err != nil {
		return fmt.Errorf("parse template failed: %v", err)
	}
	// logf.Log.V(1).Info("", "yaml", conf.String())
	if isDelete {
		err = kubeCtl.Delete("", conf.Bytes())
	} else {
		err = kubeCtl.Apply("", conf.Bytes())
	}
	if err != nil {
		return fmt.Errorf("kubectl failed with err %v yaml %v", err, conf.String())
	}
	return nil
}

// GetPodsFromDeployment returns Pods of Deployment.
func GetPodsFromDeployment(k8sClient client.Client, name, namespace string) ([]string, error) {
	replicaSetList := &v1.ReplicaSetList{}
	if err := k8sClient.List(context.TODO(), replicaSetList, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}
	var replicaSet *v1.ReplicaSet
	for _, r := range replicaSetList.Items {
		if len(r.OwnerReferences) > 0 &&
			r.OwnerReferences[0].Controller != nil && *r.OwnerReferences[0].Controller &&
			r.OwnerReferences[0].Kind == reflect.TypeOf(v1.Deployment{}).Name() && r.OwnerReferences[0].Name == name {
			replicaSet = r.DeepCopy()
			break
		}
	}
	if replicaSet == nil {
		logf.Log.V(1).Info("Failed to find ReplicaSet", "Deployment", name)
		return nil, nil
	}
	podList := &corev1.PodList{}
	pods := make([]string, 0)
	if err := k8sClient.List(context.TODO(), podList, &client.ListOptions{Namespace: namespace}); err != nil {
		return nil, err
	}
	for _, p := range podList.Items {
		if len(p.OwnerReferences) > 0 &&
			p.OwnerReferences[0].Controller != nil && *p.OwnerReferences[0].Controller &&
			p.OwnerReferences[0].Kind == reflect.TypeOf(*replicaSet).Name() && p.OwnerReferences[0].Name == replicaSet.Name {
			pods = append(pods, p.Name)
		}
	}
	return pods, nil
}

// GetServiceClusterIPPort returns clusterIP and first port of a service.
func GetServiceClusterIPPort(k8sClient client.Client, name, namespace string) (string, int32, error) {
	service := &corev1.Service{}
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := k8sClient.Get(context.TODO(), key, service); err != nil {
		return "", 0, err
	}
	return service.Spec.ClusterIP, service.Spec.Ports[0].Port, nil
}

// AddCloudAccount adds cloud account name to namespace.
func AddCloudAccount(kubeCtl *KubeCtl, params k8stemplates.CloudAccountParameters) error {
	var t string
	switch params.Provider {
	case string(v1alpha1.AWSCloudProvider):
		t = k8stemplates.AWSCloudAccount
	case string(v1alpha1.AzureCloudProvider):
		t = k8stemplates.AzureCloudAccount
	default:
		return fmt.Errorf("unknowner cloud provider %v", params.Provider)
	}

	// apply secret
	s := k8stemplates.AccountSecretParameters{
		Name:       params.SecretRef.Name,
		Namespace:  params.SecretRef.Namespace,
		Key:        params.SecretRef.Key,
		Credential: base64.StdEncoding.EncodeToString([]byte(params.SecretRef.Credential)),
	}
	if err := ConfigureK8s(kubeCtl, s, k8stemplates.AccountSecret, false); err != nil {
		return err
	}

	// apply CloudProviderAccount
	if err := ConfigureK8s(kubeCtl, params, t, false); err != nil {
		return err
	}
	return nil
}

// ConfigureEntitySelectorAndWait configures EntitySelector for cloud resources, and wait for them to be imported.
func ConfigureEntitySelectorAndWait(kubeCtl *KubeCtl, k8sClient client.Client, params k8stemplates.CloudEntitySelectorParameters,
	kind string, num int, namespace string, isDelete bool) error {
	if err := ConfigureK8s(kubeCtl, params, k8stemplates.CloudEntitySelector, isDelete); err != nil {
		return err
	}
	if err := wait.Poll(time.Second*2, time.Second*120, func() (bool, error) {
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			vmList := &v1alpha1.VirtualMachineList{}
			if err := k8sClient.List(context.TODO(), vmList, &client.ListOptions{Namespace: namespace}); err != nil {
				return false, err
			}
			if len(vmList.Items) != num {
				return false, nil
			}
			return true, nil
		}
		return false, fmt.Errorf("unknown kind %v", kind)
	}); err != nil {
		return fmt.Errorf("failed to get cloud resources %s(%d) in namespace %s: %w", kind, num, namespace, err)
	}
	return nil
}

// CheckCloudResourceNetworkPolicies checks NetworkPolicies has been applied to cloud resources.
func CheckCloudResourceNetworkPolicies(kubeCtl *KubeCtl, k8sClient client.Client, kind, namespace string, ids, anps []string,
	withAgent bool) error {
	getVMANPs := func(id string) (map[string]*runtimev1alpha1.NetworkPolicyStatus, error) {
		v := &runtimev1alpha1.VirtualMachinePolicy{}
		fetchKey := client.ObjectKey{Name: id, Namespace: namespace}
		if err := k8sClient.Get(context.TODO(), fetchKey, v); err != nil {
			if errors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return v.Status.NetworkPolicyDetails, nil
	}

	logf.Log.V(1).Info("Check NetworkPolicy on resources", "resources", ids, "nps", anps)

	if withAgent {
		err := wait.Poll(time.Second*5, time.Second*30, func() (bool, error) {
			for _, anp := range anps {
				cmd := fmt.Sprintf("get anp %s -n %s -o json -o=jsonpath={.status.phase}", anp, namespace)
				out, err := kubeCtl.Cmd(cmd)
				if err != nil {
					return false, nil
				}
				if strings.Compare(out, "Realized") != 0 {
					logf.Log.V(1).Info("ANP realization in progress", "IDS", ids, "ANP", anp)
					return false, nil
				}
			}
			return true, nil
		})
		if err != nil {
			logf.Log.Error(err, "timeout waiting for ANP realization")
		}
		return err
	}

	if err := wait.Poll(time.Second*2, time.Second*300, func() (bool, error) {
		var getter func(id string) (map[string]*runtimev1alpha1.NetworkPolicyStatus, error)
		if kind == reflect.TypeOf(v1alpha1.VirtualMachine{}).Name() {
			getter = getVMANPs
		} else {
			return false, fmt.Errorf("unknown kind %v", kind)
		}

		for _, id := range ids {
			npv, err := getter(id)
			if err != nil {
				logf.Log.Info("Get resource failed, tolerate", "Resource", id, "Error", err)
				return false, nil
			}
			if len(npv) != len(anps) {
				return false, nil
			}
			for _, a := range anps {
				v, ok := npv[a]
				if !ok {
					return false, nil
				}
				if v.Realization != runtimev1alpha1.Success {
					return false, nil
				}
			}
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to poll policies %v for resources %v: %v", anps, ids, err)
	}
	return nil
}

// ExecuteCmds excutes cmds on resource srcIDs in parallel, and returns error if oks mismatch.
func ExecuteCmds(vpc CloudVPC, kubctl *KubeCtl,
	srcIDs []string, ns string, cmds [][]string, oks []bool, retries int) error {
	var err error
	for i := 0; i < retries; i++ {
		chans := make([]chan error, len(oks))
		chIdx := 0
		for _, id := range srcIDs {
			for _, c := range cmds {
				ch := make(chan error)
				chans[chIdx] = ch
				chIdx++
				cmd := c
				iid := id
				go func() {
					var err error
					if vpc != nil {
						_, err = vpc.VMCmd(iid, cmd, time.Second*5)
					} else {
						_, err = kubctl.PodCmd(&types.NamespacedName{Name: iid, Namespace: ns}, cmd, time.Second*5)
					}
					ch <- err
				}()
			}
		}
		err = nil
		for i, ch := range chans {
			ret := <-ch
			if oks[i] && ret != nil {
				err = ret
				break
			} else if !oks[i] && ret == nil {
				err = fmt.Errorf("unexpected successful cmd [%v] on %v", cmds[i%len(cmds)], srcIDs[i/len(cmds)])
				break
			}
		}
		if err == nil {
			return nil
		}
		// Failure should be ANP not applied on time, sleep some before next retry.
		logf.Log.Info(fmt.Sprintf("Error executing command, retry in 10s. Error message: %s", err))
		time.Sleep(time.Second * 10)
	}
	return err
}

// ExecuteCurlCmds executes curl on resource srcIDs in parallel, and returns error if oks mismatch.
func ExecuteCurlCmds(vpc CloudVPC, kubctl *KubeCtl,
	srcIDs []string, ns string, destIPs []string, port string, oks []bool, retries int) error {
	cmds := make([][]string, 0, len(destIPs))
	for _, ip := range destIPs {
		cmds = append(cmds, []string{"curl", "--connect-timeout", "3", "http://" + ip + ":" + port})
	}
	return ExecuteCmds(vpc, kubctl, srcIDs, ns, cmds, oks, retries)
}

// CheckRestart returns error if nephe controller has restarted.
func CheckRestart(kubctl *KubeCtl) error {
	controllers := []string{"nephe-controller"}
	for _, c := range controllers {
		cmd := fmt.Sprintf(
			"get pods -l control-plane=%s -n nephe-system -o=jsonpath={.items[0].status.containerStatuses[0].restartCount}", c)
		out, err := kubctl.Cmd(cmd)
		if err != nil {
			return err
		}
		if out != "0" {
			return fmt.Errorf("%s has restarted %s times", c, out)
		}
	}
	return nil
}

// GenerateNameFromText returns a name derived from test.FullContext.
// Stripping space and focus.
func GenerateNameFromText(fullText string, focus []string) string {
	for _, f := range focus {
		fullText = strings.ReplaceAll(fullText, f, "")
	}
	fullText = strings.ReplaceAll(fullText, ",", "")
	fullText = strings.ReplaceAll(fullText, ":", "")
	return strings.ReplaceAll(fullText, " ", "")
}

// SetAgentConfig configures the cluster, generates agent kubeconfigs and sets terraform env vars.
func SetAgentConfig(c client.Client, ns *corev1.Namespace, cloudProviders, antreaVersion, kubeconfig string) error {
	err := c.Create(context.TODO(), ns)
	if err != nil {
		return fmt.Errorf("failed to create static vm namespace %+v", err)
	}

	// TODO: decouple cluster type with cloud provider type
	clusterType := ""
	switch cloudProviders {
	case string(cloudv1alpha1.AzureCloudProvider):
		clusterType = "aks"
	case string(cloudv1alpha1.AWSCloudProvider):
		clusterType = "eks"
	}

	dir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir = dir + "/tmp/integration/"

	err = os.MkdirAll(dir, 0777)
	if err != nil {
		return err
	}
	_ = os.Setenv("KUBECONFIG", kubeconfig)
	cmd := exec.Command("./ci/generate-agent-config.sh", "--cluster-type", clusterType, "--antrea-version", antreaVersion,
		"--target-dir", dir)
	bytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to generate antrea agent kubeconfigs %+v: %s", err, string(bytes))
	}
	path, err := filepath.Abs("./hack/install-wrapper.sh")
	if err != nil {
		return err
	}

	_ = os.Setenv("TF_VAR_with_agent", "true")
	_ = os.Setenv("TF_VAR_antrea_agent_k8s_config", dir+"antrea-agent.kubeconfig")
	_ = os.Setenv("TF_VAR_antrea_agent_antrea_config", dir+"antrea-agent.antrea.kubeconfig")
	_ = os.Setenv("TF_VAR_install_wrapper", path)
	return nil
}

// CollectAgentInfo collect ovs dump-flows from all bridges.
func CollectAgentInfo(kubctl *KubeCtl, dir string) error {
	getPodsCmd := "get pods -n kube-system -o=jsonpath='{range.items[*]}{.metadata.name}{\"\\n\"}{end}'"
	getPodsOutput, err := kubctl.Cmd(getPodsCmd)
	if err != nil {
		return err
	}
	pods := strings.Split(strings.Trim(getPodsOutput, "'"), "\n")
	for _, p := range pods {
		if !strings.HasPrefix(p, "antrea-agent") {
			continue
		}
		dirName := path.Join(dir, p)
		err := os.MkdirAll(dirName, 0777)
		if err != nil {
			return err
		}
		showBridgesCmd := fmt.Sprintf(
			"exec %s -c antrea-ovs -n kube-system -- ovs-vsctl show", p)
		showBridgesOutput, err := kubctl.Cmd(showBridgesCmd)
		if err != nil {
			return err
		}
		lines := strings.Split(showBridgesOutput, "\n")
		for _, l := range lines {
			l = strings.TrimLeft(l, " ")
			if strings.HasPrefix(l, "Bridge") {
				bridge := strings.Split(l, " ")[1]
				dumpFlowsCmd := fmt.Sprintf(
					"exec %s -n kube-system -c antrea-ovs -- ovs-ofctl dump-flows %s", p, bridge)
				dumpFlowsOutput, err := kubctl.Cmd(dumpFlowsCmd)
				if err != nil {
					return err
				}
				fn := path.Join(dirName, bridge+"_dump_flows")
				err = os.WriteFile(fn, []byte(dumpFlowsOutput), 0666)
				if err != nil {
					return err
				}
			}
		}
		// Retrieve logs on agents.
		containers := []string{"antrea-agent", "antrea-ovs"}
		for _, c := range containers {
			output, err := kubctl.Cmd(fmt.Sprintf("logs %s -c %s -n kube-system", p, c))
			if err != nil {
				continue
			}
			fn := path.Join(dirName, c+".log")
			err = os.WriteFile(fn, []byte(output), 0666)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// CollectVMAgentLog collects VM agent log from all imported VMs.
func CollectVMAgentLog(cloudVPC CloudVPC, dir string) error {
	err := os.MkdirAll(dir, 0777)
	if err != nil {
		return err
	}
	vmCmd := []string{"cat", "/var/log/antrea/antrea-agent.log"}
	for _, vm := range cloudVPC.GetVMs() {
		output, err := cloudVPC.VMCmd(vm, vmCmd, 10*time.Second)
		if err != nil {
			continue
		}
		fn := path.Join(dir, vm+"-agent.log")
		err = os.WriteFile(fn, []byte(output), 0666)
		if err != nil {
			return err
		}
	}
	return nil
}

// CollectCRDs collects related CRDs in the cluster.
func CollectCRDs(kubectl *KubeCtl, dir string) error {
	err := os.MkdirAll(dir, 0777)
	if err != nil {
		return err
	}
	crdKinds := []string{
		"vm",
		"anp",
		"vmp",
		"ee",
		"addressgroups",
		"appliedtogroups",
	}
	for _, k := range crdKinds {
		fName := path.Join(dir, fmt.Sprintf("%s-output", k))
		cmd := fmt.Sprintf("describe %s -A", k)
		output, _ := kubectl.Cmd(cmd)
		_ = os.WriteFile(fName, []byte(output), 0666)
	}
	return nil
}

// CollectControllerLogs collect logs from controllers.
func CollectControllerLogs(kubctl *KubeCtl, dir string) error {
	controllerInfo := map[string][]string{
		"nephe-controller":  {"control-plane", "nephe-system", ""},
		"antrea-controller": {"component", "kube-system", ""},
	}
	it := []string{"-p", ""}
	for k, v := range controllerInfo {
		dirName := path.Join(dir, k)
		err := os.MkdirAll(dirName, 0777)
		if err != nil {
			return err
		}
		for _, i := range it {
			cmd := fmt.Sprintf(
				"logs -l %s=%s --tail=-1 %s -n %s%s", v[0], k, i, v[1], v[2])
			output, err := kubctl.Cmd(cmd)
			if err != nil {
				continue
			}
			fn := path.Join(dirName, "log"+i)
			err = os.WriteFile(fn, []byte(output), 0666)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// CollectSupportBundle collect antrea and nephe logs.
func CollectSupportBundle(kubctl *KubeCtl, dir string, cloudVPC CloudVPC, withAgent bool) {
	logf.Log.Info("Collecting support bundles")
	if err := CollectAgentInfo(kubctl, dir); err != nil {
		logf.Log.Error(err, "Failed to collect OVS flows")
	}
	if err := CollectControllerLogs(kubctl, dir); err != nil {
		logf.Log.Error(err, "Failed to collect controller logs")
	}
	if err := CollectCRDs(kubctl, dir); err != nil {
		logf.Log.Error(err, "Failed to collect CRDs")
	}
	if withAgent {
		if err := CollectVMAgentLog(cloudVPC, dir); err != nil {
			logf.Log.Error(err, "Failed to collect VM agent logs")
		}
	}
}

// WaitApiServer wait for aggregated api server to be ready.
func WaitApiServer(k8sClient client.Client, timeout time.Duration) error {
	if err := wait.Poll(time.Second, timeout, func() (bool, error) {
		vmpList := &runtimev1alpha1.VirtualMachinePolicyList{}
		if err := k8sClient.List(context.TODO(), vmpList); err != nil {
			if errors.IsServiceUnavailable(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}); err != nil {
		return err
	}
	return nil
}
