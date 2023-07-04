#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..

bash vendor/k8s.io/code-generator/generate-groups.sh defaulter,client,lister,informer \
  github.com/k8snetworkplumbingwg/bridge-operator/pkg/client github.com/k8snetworkplumbingwg/bridge-operator/api \
  bridgeoperator.k8s.cni.npwg.io:v1alpha1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt
