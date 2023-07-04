/*
Copyright 2022 Red Hat, Inc

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

package server

import (
	"context"
	"fmt"
	bridgeconfv1alpha1 "github.com/k8snetworkplumbingwg/bridge-operator/api/bridgeoperator.k8s.cni.npwg.io/v1alpha1"
	bridgeconfclient "github.com/k8snetworkplumbingwg/bridge-operator/pkg/client/clientset/versioned"
	bridgeconfclientscheme "github.com/k8snetworkplumbingwg/bridge-operator/pkg/client/clientset/versioned/scheme"
	bridgeconfinformer "github.com/k8snetworkplumbingwg/bridge-operator/pkg/client/informers/externalversions"
	bridgeconflisterv1alpha1 "github.com/k8snetworkplumbingwg/bridge-operator/pkg/client/listers/bridgeoperator.k8s.cni.npwg.io/v1alpha1"
	"github.com/k8snetworkplumbingwg/bridge-operator/pkg/controllers"
	"github.com/vishvananda/netlink"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	/*
		discovery "k8s.io/api/discovery/v1"
		"k8s.io/apimachinery/pkg/fields"
		"k8s.io/apimachinery/pkg/selection"
	*/
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	//"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	/*
		corelisters "k8s.io/client-go/listers/core/v1"
		discoverylisters "k8s.io/client-go/listers/discovery/v1"
	*/
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	//api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/util/async"
	utilnode "k8s.io/kubernetes/pkg/util/node"
	//"k8s.io/utils/exec"
)

type managedBridgePortsInfo struct {
	managedBridgePorts      map[string]map[string]bool
	managedBridgePortsMutex sync.Mutex
}

type Server struct {
	bridgeConfChanges *controllers.BridgeConfChangeTracker
	bridgeConfMap     controllers.BridgeConfMap
	bridgeConfLister  bridgeconflisterv1alpha1.BridgeConfigurationLister
	bridgeConfClient  bridgeconfclient.Interface

	Client           clientset.Interface
	Hostname         string
	hostPrefix       string
	Broadcaster      record.EventBroadcaster
	Recorder         record.EventRecorder
	Options          *Options
	ConfigSyncPeriod time.Duration
	NodeRef          *v1.ObjectReference

	bridgeConfSynced bool

	managedBridges      map[string]bool
	managedBridgesMutex sync.Mutex

	managedBridgePortsInfo

	bridgeWatcher     *async.BoundedFrequencyRunner
	bridgeInfoUpdater *async.BoundedFrequencyRunner
	bridgePortWatcher *async.BoundedFrequencyRunner
	syncRunnerStopCh  chan struct{}
}

func init() {
	_ = bridgeconfclientscheme.AddToScheme(scheme.Scheme)
}

func NewServer(o *Options) (*Server, error) {
	var kubeConfig *rest.Config
	var err error

	if len(o.Kubeconfig) == 0 {
		klog.Info("Neither kubeconfig file nor master URL was specified. Falling back to in-cluster config.")
		kubeConfig, err = rest.InClusterConfig()
	} else {
		kubeConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: o.Kubeconfig},
			&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: o.master}},
		).ClientConfig()
	}
	if err != nil {
		return nil, err
	}

	client, err := clientset.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	bridgeConfClient, err := bridgeconfclient.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	hostname, err := utilnode.GetHostname(o.hostnameOverride)
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "multus-proxy", Host: hostname})

	nodeRef := &v1.ObjectReference{
		Kind:      "Node",
		Name:      hostname,
		UID:       types.UID(hostname),
		Namespace: "",
	}

	syncPeriod := 3 * time.Second
	minSyncPeriod := 500 * time.Millisecond
	burstSyncs := 2

	bridgeConfChanges := controllers.NewBridgeConfChangeTracker()
	if bridgeConfChanges == nil {
		return nil, fmt.Errorf("cannot create bridgeconf change tracker")
	}

	server := &Server{
		Options:           o,
		Client:            client,
		bridgeConfClient:  bridgeConfClient,
		Hostname:          hostname,
		Broadcaster:       eventBroadcaster,
		Recorder:          recorder,
		NodeRef:           nodeRef,
		bridgeConfChanges: bridgeConfChanges,
		bridgeConfMap:     make(controllers.BridgeConfMap),
		managedBridges:    make(map[string]bool),
		managedBridgePortsInfo: managedBridgePortsInfo{
			managedBridgePorts: make(map[string]map[string]bool),
		},
	}
	server.bridgeWatcher = async.NewBoundedFrequencyRunner("sync-runner-bridge", server.bridgeWatch, minSyncPeriod, syncPeriod, burstSyncs)
	server.bridgeInfoUpdater = async.NewBoundedFrequencyRunner("sync-runner-bridge-info", server.bridgeInfoUpdate, minSyncPeriod, syncPeriod, burstSyncs)
	server.bridgePortWatcher = async.NewBoundedFrequencyRunner("sync-runner-bridge-port", server.bridgePortWatch, minSyncPeriod, syncPeriod, burstSyncs)
	server.syncRunnerStopCh = make(chan struct{})

	return server, nil
}

func (s *Server) bridgePortWatch() {
	linkList, err := netlink.LinkList()
	if err != nil {
		klog.Errorf("failed to get link list: %v", err)
	}

	// watch new bridge ports
	for _, link := range linkList {
		masterIndex := link.Attrs().MasterIndex
		if masterIndex != 0 {
			masterLink, err := netlink.LinkByIndex(masterIndex)
			if err != nil {
				klog.Errorf("failed to get link by index(%d): %v", masterIndex, err)
			}
			bridgeName := masterLink.Attrs().Name
			bridgePortMap, ok := s.managedBridgePorts[bridgeName]
			if ok {
				linkName := link.Attrs().Name
				_, ok := bridgePortMap[linkName]
				if !ok {
					s.managedBridgePortsInfo.SetPortManagement(bridgeName, linkName, false)
				}
			}

		}
	}

	// check the port status
	for bridgeName, bridgePortMap := range s.managedBridgePorts {
		bridge, err := netlink.LinkByName(bridgeName)
		if err != nil {
			klog.Errorf("failed to get bridge %s: %v", bridgeName, err)
		}
		bridgeIndex := bridge.Attrs().Index

		for portName, _ := range bridgePortMap {
			link, err := netlink.LinkByName(portName)
			if err == nil && link.Attrs().MasterIndex == bridgeIndex {
				continue
			}
			s.managedBridgePortsInfo.RemovePortManagement(bridgeName, portName)
		}
	}
}

func newBridgeInfo(name string) *bridgeconfv1alpha1.BridgeInformation {
	return &bridgeconfv1alpha1.BridgeInformation{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BridgeInformation",
			APIVersion: bridgeconfv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func (s *Server) bridgeInfoUpdate() {
	klog.Infof("====\n")
	klog.Infof("Node: %s", s.Hostname)
	klog.Infof("bridge: %v\n", s.managedBridges)
	klog.Infof("bridge port: %v\n\n", s.managedBridgePorts)

	bridgeInfoList, err := s.bridgeConfClient.BridgeoperatorV1alpha1().BridgeInformations().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list bridge info: %v", err)
	} else {
		for _, bridgeInfo := range bridgeInfoList.Items {
			if bridgeInfo.Status.Node == s.Hostname {
				_, ok := s.managedBridges[bridgeInfo.Status.Name]
				if !ok {
					err := s.bridgeConfClient.BridgeoperatorV1alpha1().BridgeInformations().Delete(context.TODO(), bridgeInfo.Name, metav1.DeleteOptions{})
					if err != nil {
						klog.Errorf("failed to delete bridge info: %v", err)
					}
				}
			}
		}
	}

	for bridgeName := range s.managedBridges {
		infoName := fmt.Sprintf("%s-%s", s.Hostname, bridgeName)
		bridgeInfo, err := s.bridgeConfClient.BridgeoperatorV1alpha1().BridgeInformations().Get(context.TODO(), infoName, metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("bridge info update: get error: %v", err)
			}
			bridgeInfo = newBridgeInfo(infoName)
			bridgeInfo, err = s.bridgeConfClient.BridgeoperatorV1alpha1().BridgeInformations().Create(context.TODO(), bridgeInfo, metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("bridge info update: create error: %v", err)
			}
		}

		//XXX: could be improve to use UpdateStatus(), instead of Update()
		newBridgeInfo := bridgeInfo.DeepCopy()
		newBridgeInfo.APIVersion = bridgeconfv1alpha1.SchemeGroupVersion.String()
		newBridgeInfo.Kind = "BridgeInformation"
		newBridgeInfo.Status.Name = bridgeName
		newBridgeInfo.Status.Node = s.Hostname
		newBridgeInfo.Status.Managed = s.managedBridges[bridgeName]
		newBridgeInfo.Status.Ports = []bridgeconfv1alpha1.BridgeInformationPortStatus{}
		for portName, portManaged := range s.managedBridgePorts[bridgeName] {
			newBridgeInfo.Status.Ports = append(newBridgeInfo.Status.Ports,
				bridgeconfv1alpha1.BridgeInformationPortStatus{
					Name:    portName,
					Managed: portManaged,
				})
		}

		newBridgeInfo, err = s.bridgeConfClient.BridgeoperatorV1alpha1().BridgeInformations().UpdateStatus(context.TODO(), newBridgeInfo, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("bridge info updateStatus: update status error: %v", err)
		}
	}
}

func (s *Server) SetBridgeManagement(bridge string, isManaged bool) error {
	if val, ok := s.managedBridges[bridge]; ok {
		return fmt.Errorf("bridge %s is already configured in managedBridges(isManaged: %v)", bridge, val)
	}
	s.managedBridgesMutex.Lock()
	defer s.managedBridgesMutex.Unlock()
	s.managedBridges[bridge] = isManaged

	if _, ok := s.managedBridgePorts[bridge]; ok {
		return fmt.Errorf("bridge %s is already configured in managedBridgePorts", bridge)
	}
	s.managedBridgePortsMutex.Lock()
	defer s.managedBridgePortsMutex.Unlock()
	s.managedBridgePorts[bridge] = make(map[string]bool)

	return nil
}

func (s *Server) UnsetBridgeManagement(bridge string) error {
	s.managedBridgesMutex.Lock()
	defer s.managedBridgesMutex.Unlock()
	if _, ok := s.managedBridges[bridge]; ok {
		delete(s.managedBridges, bridge)
	}

	s.managedBridgePortsMutex.Lock()
	defer s.managedBridgePortsMutex.Unlock()
	if _, ok := s.managedBridgePorts[bridge]; ok {
		for k, _ := range s.managedBridgePorts[bridge] {
			delete(s.managedBridgePorts[bridge], k)
		}
		delete(s.managedBridgePorts, bridge)
	}
	delete(s.managedBridgePorts, bridge)
	return nil
}

func (s *Server) IsBridgeManaged(bridge string) bool {
	if val, ok := s.managedBridges[bridge]; ok {
		return val
	}
	return false
}

func (m *managedBridgePortsInfo) SetPortManagement(bridge string, port string, isManaged bool) error {
	if _, ok := m.managedBridgePorts[bridge]; !ok {
		return fmt.Errorf("no bridge %s in managedBridgePorts", bridge)
	}
	if val, ok := m.managedBridgePorts[bridge][port]; ok {
		return fmt.Errorf("%s/%s is already configured in managedBridgePorts(isManaged:%v)", bridge, port, val)
	}

	m.managedBridgePortsMutex.Lock()
	defer m.managedBridgePortsMutex.Unlock()
	m.managedBridgePorts[bridge][port] = isManaged

	return nil
}

func (m *managedBridgePortsInfo) RemovePortManagement(bridge, port string) error {
	m.managedBridgePortsMutex.Lock()
	defer m.managedBridgePortsMutex.Unlock()

	if ports, ok := m.managedBridgePorts[bridge]; ok {
		if _, ok := ports[port]; ok {
			delete(ports, port)
			return nil
		}
	}
	return fmt.Errorf("No port, %s, in the bridge %s", port, bridge)
}

func (m *managedBridgePortsInfo) IsPortManaged(bridge string, port string) bool {
	if ports, ok := m.managedBridgePorts[bridge]; ok {
		if val, ok := ports[port]; ok {
			return val
		}
	}
	return false
}

func (s *Server) OnBridgeConfigSynced() {
	klog.Info("Bridge Synced")
	if s.bridgeWatcher != nil {
		go s.bridgeWatcher.Loop(s.syncRunnerStopCh)
		s.bridgeWatcher.Run()
	}

	if s.bridgeInfoUpdater != nil {
		go s.bridgeInfoUpdater.Loop(s.syncRunnerStopCh)
		s.bridgeInfoUpdater.Run()
	}

	if s.bridgePortWatcher != nil {
		go s.bridgePortWatcher.Loop(s.syncRunnerStopCh)
		s.bridgePortWatcher.Run()
	}
}

func (s *Server) bridgeWatch() {
	bridgeConfigs, err := s.bridgeConfLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get bridgeConfig: %v", err)
		return
	}

	for _, bridgeConfig := range bridgeConfigs {
		bridgeReconcile(bridgeConfig)
	}
}

func (s *Server) Run(hostname string, stopCh chan struct{}) error {
	if s.Broadcaster != nil {
		s.Broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: s.Client.CoreV1().Events("")})
	}

	bridgeConfInformerFactory := bridgeconfinformer.NewSharedInformerFactoryWithOptions(
		s.bridgeConfClient, s.ConfigSyncPeriod)
	s.bridgeConfLister = bridgeConfInformerFactory.Bridgeoperator().V1alpha1().BridgeConfigurations().Lister()
	bridgeConfig := controllers.NewBridgeConfConfig(
		bridgeConfInformerFactory.Bridgeoperator().V1alpha1().BridgeConfigurations(), s.ConfigSyncPeriod)
	bridgeConfig.RegisterEventHandler(s)

	go bridgeConfig.Run(wait.NeverStop)
	bridgeConfInformerFactory.Start(wait.NeverStop)

	// Wait for stop signal
	<-stopCh

	// Stop the sync runner loop
	s.syncRunnerStopCh <- struct{}{}

	return nil
}

func bridgeConfNamespacedName(config *bridgeconfv1alpha1.BridgeConfiguration) string {
	if config == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s/%s", config.Namespace, config.Name)
}

func bridgeConfMatchesNodeLabel(node *v1.Node, config *bridgeconfv1alpha1.BridgeConfiguration) (bool, error) {
	if config.Spec.NodeSelector.Size() != 0 {
		nodeSelectorMap, err := metav1.LabelSelectorAsMap(&config.Spec.NodeSelector)
		if err != nil {
			klog.Errorf("bad label selector for node %q: %v", bridgeConfNamespacedName(config), err)
			return false, err
		}
		bridgeNodeSelector := labels.Set(nodeSelectorMap).AsSelectorPreValidated()
		if !bridgeNodeSelector.Matches(labels.Set(node.Labels)) {
			return false, nil
		}
		return true, nil
	}

	// if no nodeSelector, it must be matched.
	return true, nil
}

func (s *Server) OnBridgeConfigAdd(config *bridgeconfv1alpha1.BridgeConfiguration) {
	node, err := s.Client.CoreV1().Nodes().Get(context.TODO(), s.Hostname, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("cannot get node: %q: %v", s.Hostname, err)
	}

	matches, err := bridgeConfMatchesNodeLabel(node, config)
	if err != nil {
		klog.Errorf("failed to get label selector: %v", err)
	}
	if matches {
		klog.Infof("node matches!")
		exists, err := bridgeIsConfigured(config.Name)
		if err != nil {
			klog.Errorf("failed to configure bridge: %v", err)
		}
		err = s.SetBridgeManagement(config.Name, exists == false)
		if err != nil {
			klog.Errorf("failed to add bridge management: %v", err)
		}
		err = bridgeConfigured(config, &s.managedBridgePortsInfo)
		if err != nil {
			klog.Errorf("failed to configure bridge: %v", err)
		}
	}

}

func (s *Server) OnBridgeConfigUpdate(oldConfig, config *bridgeconfv1alpha1.BridgeConfiguration) {
	klog.Info("Bridge Update")
	node, err := s.Client.CoreV1().Nodes().Get(context.TODO(), s.Hostname, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("cannot get node: %q: %v", s.Hostname, err)
	}

	oldMatches, err := bridgeConfMatchesNodeLabel(node, oldConfig)
	if err != nil {
		klog.Errorf("failed to get label selector: %v", err)
	}

	matches, err := bridgeConfMatchesNodeLabel(node, oldConfig)
	if err != nil {
		klog.Errorf("failed to get label selector: %v", err)
	}

	// label selector is changed
	if matches != oldMatches {
		// false -> true: create new bridge
		if matches == true {
			exists, err := bridgeIsConfigured(config.Name)
			if err != nil {
				klog.Errorf("failed to configure bridge: %v", err)
			}
			err = s.SetBridgeManagement(config.Name, exists == false)
			if err != nil {
				klog.Errorf("failed to add bridge management: %v", err)
			}
			err = bridgeConfigured(config, &s.managedBridgePortsInfo)
			if err != nil {
				klog.Errorf("failed to configure bridge: %v", err)
			}
		} else { // true -> false: delete new bridge
			err = bridgeUnconfigured(config, s.managedBridges[config.Name], &s.managedBridgePortsInfo)
			if err != nil {
				klog.Errorf("failed to unconfigure bridge: %v", err)
			}
			s.UnsetBridgeManagement(config.Name)
			if err != nil {
				klog.Errorf("failed to remove bridge management: %v", err)
			}
		}
		// bridgeConfigured() calls bridgeUpdate(), so just return here
		return
	}
	err = bridgeUpdate(oldConfig, config, &s.managedBridgePortsInfo)
	if err != nil {
		klog.Errorf("failed to sync bridge config: %v", err)
	}
}

func (s *Server) OnBridgeConfigDelete(config *bridgeconfv1alpha1.BridgeConfiguration) {
	klog.Info("Bridge Delete")
	node, err := s.Client.CoreV1().Nodes().Get(context.TODO(), s.Hostname, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("cannot get node: %q: %v", s.Hostname, err)
	}

	matches, err := bridgeConfMatchesNodeLabel(node, config)
	if err != nil {
		klog.Errorf("failed to get label selector: %v", err)
	}
	if matches {
		klog.Infof("node matches!")
		err = bridgeUnconfigured(config, s.managedBridges[config.Name], &s.managedBridgePortsInfo)
		if err != nil {
			klog.Errorf("failed to unconfigure bridge: %v", err)
		}
		s.UnsetBridgeManagement(config.Name)
		if err != nil {
			klog.Errorf("failed to remove bridge management: %v", err)
		}
	}
}
