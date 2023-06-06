# Nephe CI: Jenkins

## Table of Contents

<!-- toc -->
- [Introduction](#introduction)
- [Jenkins Deployment](#jenkins-deployment)
  - [Teraform](#teraform)
  - [Ansible](#ansible)
  - [Jenkins Job](#jenkins-job)
    - [Requirements](#requirements)
    - [Apply the jobs](#apply-the-jobs)
    - [Jenkins job updater](#jenkins-job-updater)
- [List of jenkins job](#list-of-jenkins-jobs)
<!-- /toc -->

## Introduction

This directory includes all the scripts required to run CI for nephe.

We have a cluster of static VMs configured to run as jenkins node.  When a jenkins
job is triggered, the nephe code will be cloned on the static VM. The jenkins node
will call terraform to deploy a dynamic VM and use ansible for installing any missing
packages like docker. Then tests will be run on that dynamic VM, Upon completion
of the test (success or fail), the dynamic VM will be destroyed.

## Jenkins Deployment

### Teraform

The persistent data stored in the backend belongs to a workspace. Named workspaces allow
conveniently switching between multiple instances of a single configuration within its single
backend. Multiple workspaces are used here to create multiple dynamic VMs in parallel, each
dynamic VM is in a workspace.

- [nephi-ci.sh](./nephe-ci.sh) is responsible for creating terraform tfvars files.
- [deploy.sh](./deploy.sh) is the script to create vm, and store the terraform state
in a specified directory, which can be used to retrieve VM information.
- [destroy.sh](./destroy.sh) helps to destroy the dynamic VM and delete terraform workspace as well.

Please refer to [terraform](https://www.terraform.io/docs) documentation for more information.

### Ansible

Ansible is used to configure VM once terraform deploys the dynamic VM. All anisble
tasks are in playbook/roles. For example, we add task to install docker in
[playbook/roles/docker/tasks/main.yml](playbook/roles/docker/tasks/main.yml).

Ansible is used to copy ssh keys to the dynamic VM, which is used to run the tests
and collect logs from the dynamic VM later.

Please refer to [ansible](https://docs.ansible.com/) documentation for more information.

### Jenkins Job

We use jenkins job-builder to manage jenkins jobs and the jenkins yamls are placed in
the directory [jobs](./jobs).

#### Requirements

Jenkins jobs can be updated through yaml files in [jobs](./jobs).
Yaml files under current directory can be generated via `jenkins-job-builder`.
If you want to verify the changes in yaml files, please install
[jenkins-job-builder](https://jenkins-job-builder.readthedocs.io/en/latest/index.html)

#### Validate Jobs

Run the command to test if jobs can be generated correctly.

```bash
jenkins-jobs test -r ci/jenkins/jobs
```

When a PR containing code changes to [jobs](./jobs) is merged, Jenkins jobs on
cloud will be automatically updated with the new changes.

## List of jenkins Jobs

- `nephe-test-e2e-aws-for-pull-request`: Run end-to-end AWS tests on a Kind cluster with AWS VMs
- `nephe-test-e2e-azure-for-pull-request`: Run end-to-end Azure tests on a Kind cluster with Azure VMs
- `nephe-test-e2e-eks-for-pull-request`: Run end-to-end AWS tests on EKS cluster with AWS VMs
- `nephe-test-e2e-aks-for-pull-request`: Run end-to-end Azure tests on AKS cluster with Azure VMs
- `nephe-test-e2e-aws-upgrade-for-pull-request`: Run end-to-end AWS upgrade tests on a Kind cluster with AWS VMs
- `nephe-test-e2e-azure-upgrade-for-pull-request`: Run end-to-end Azure upgrade tests on a Kind cluster with Azure VMs
- `nephe-test-e2e-with-agent-eks-for-pull-request`: Run end-to-end AWS tests on EKS cluster with AWS VMs with Linux agent installed.
- `nephe-test-e2e-with-agent-aks--for-pull-request`: Run end-to-end Azure tests on AKS cluster with Azure VMs with Linux agent installed.
- `nephe-test-e2e-with-windows-eks-for-pull-request`: Run end-to-end AWS tests on EKS cluster with AWS VMs with Windows agent installed.
- `nephe-test-e2e-with-windows-aks-for-pull-request`: Run end-to-end Azure tests on AKS cluster with Azure VMs with Windows agent installed.
