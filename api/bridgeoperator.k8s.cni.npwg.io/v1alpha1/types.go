/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	//"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/util/intstr"
)

//+genclient
//+genclient:nonNamespaced
//+genclient:noStatus
//+resourceName=bridgeconfigurations
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster

// BridgeConfiguration is the schema for bridgeconfiguration API
type BridgeConfiguration struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec BridgeConfigurationSpec `json:"spec,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BridgeConfiguration is the schema list for bridgeconfiguration API
type BridgeConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []BridgeConfiguration `json:"items"`
}

// BridgeConfiguration spec defines the desired state of a bridge
type BridgeConfigurationSpec struct {
	// NodeSelector is a selector which must be true for the
	// bridge to fit on a node. Selector which must match a node''s
	// labels for the pod to be scheduled on that node. More info:
	// https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	NodeSelector metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// +optional
	EgressVlanInterfaces []EgressVlanInterface `json:"egressVlanInterfaces,omitempty"`

	// +optional
	EgressInterfaces []EgressInterface `json:"egressInterfaces,omitempty"`
}

// EgressVlanInterfaces represents egress interfaces of bridge with specific vlan,
// which will be created by bridge-operator'
type EgressVlanInterface struct {
	// name represents parent interfaces of vlan interface
	Name string `json:"name"`

	// Protocol specifies vlan protocol (802.1q or 802.1ad). default: 802.1q.
	Protocol string `json:"protocol"`

	// Id specifies vlan id of given protocol
	Id int `json:"id"`
}

// EgressInterfaces represents egress interfaces of bridge
type EgressInterface struct {
	// Name represents interface to be added into the bridge
	Name string `json:"name"`
}

//+genclient
//+genclient:nonNamespaced
//+resourceName=bridgeinformation
//+k8s:deepcopy-gen=true
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+k8s:defaulter-gen=TypeMeta
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// BridgeInformation is the schema for bridge information API
type BridgeInformation struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Status represents per node bridge status
	Status BridgeInformationStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BridgeInformationList is the schema list for bridge information API
type BridgeInformationList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []BridgeInformation `json:"items"`
}

// BridgeInformationStatus defines the status of a bridge
type BridgeInformationStatus struct {
	// Name specifies bridge name
	Name string `json:"name"`
	// Node specifies node name that runs the bridge
	Node string `json:"node"`
	// Managed specifies whether this bridge is manged by bridge-operator
	Managed bool `json:"managed"`
	// Ports specifies bridge port managed by bridge-operator
	Ports []BridgeInformationPortStatus `json:"ports"`
}

// BridgeInformationPortStatus defines the status of bridge ports
type BridgeInformationPortStatus struct {
	// Name specifies bridge port name, managed by bridge-operator
	Name string `json:"name"`
	// Managed specifies whether this port is manged by bridge-operator
	Managed bool `json:"managed"`
}

func init() {
	SchemeBuilder.Register(&BridgeConfiguration{}, &BridgeConfigurationList{})
	SchemeBuilder.Register(&BridgeInformation{}, &BridgeInformationList{})
}
