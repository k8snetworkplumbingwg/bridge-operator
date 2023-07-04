# bridge-operator

## Description
Bridge-operator manages linux bridge in Kubernetes cluster node. Bridge-operator creates bridges to the specified node and it provides to connect physical interfaces (e.g. ens4s0) to bridges or it also provides to create (802.1q or 802.1ad) vlan interfaces and to connect the vlan interfaces to bridges.

This bridge-operator is expected to use with 'bridge' CNI plugin to connect the pods to the external network through the bridge.

## Quickstart

TL;DR
```
git clone https://github.com/k8snetworkplumbingwg/bridge-operator
make deploy IMG=ghcr.io/k8snetworkplumbingwg/bridge-operator:latest
```

## Requirements

- Linux which supports bridge
- Kubernetes (target to run with last 2 releases)

## Configurations

For example, following two BridgeConfigurations
- create linux bridge 'sample1' in all nodes that has the label, `kubernetes.io/os: linux` and attach `enp5s0` interface to the 'sample1' and
- create linux bridge 'sample2' in the node that has the label, `kubernetes.io/hostname: master0.test.example.local`, create 802.1ad(id:11) vlan interface in enp5s1 and attach the vlan interface to the 'sample2'

```
---
apiVersion: bridgeoperator.k8s.cni.npwg.io/v1alpha1
kind: BridgeConfiguration
metadata:
  name: sample1
spec:
  nodeSelector:
    matchLabels:
      kubernetes.io/os: linux
  egressInterfaces:
  - name: enp5s0
---
apiVersion: bridgeoperator.k8s.cni.npwg.io/v1alpha1
kind: BridgeConfiguration
metadata:
  name: sample2
spec:
  nodeSelector:
    matchLabels:
      kubernetes.io/hostname: master0.test.example.local
  egressVlanInterfaces:
  - name: enp5s1
    protocol: 802.1ad
    id: 11
```

- name: specify bridge name. the BridgeConfiguration name is used for bridge name as well
- nodeSelector.matchLabels: specify nodeSelector to the node that creates bridges
- egressInterfaces: specify egress interface of the bridge. this interface should be in the node
  - name: interface name (e.g. 'eth1', 'ens4s0' and so on). this interface should be in the node
- egressVlanInterfaces: specify egress 'vlan' interface of the bridge. egressVlanInterfaces will create vlan interface and attach it into the bridge
  - name: interface name (e.g. 'eth1', 'ens4s0' and so on) for the vlan. this interface should be in the node
  - protocol: Specify '802.1q' or '802.1ad'. '802.1q' is the default
  - id: specify vlan id (VID)

## TODO

* Bugfixing
* (TBD)

## Contact Us

For any questions about bridge-operator, feel free to ask a question in #general in the [NPWG Slack](https://npwg-team.slack.com/), or open up a GitHub issue. To be invited, use [this slack invite link](https://join.slack.com/t/npwg-team/shared_invite/zt-1u2vmsn2b-tKdOokdPY73zn9B32JoAOg).
