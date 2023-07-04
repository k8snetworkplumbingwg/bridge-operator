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

package controllers

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	bridgeconfv1alpha1 "github.com/k8snetworkplumbingwg/bridge-operator/api/bridgeoperator.k8s.cni.npwg.io/v1alpha1"
	bridgeconfinformerb1alpha1 "github.com/k8snetworkplumbingwg/bridge-operator/pkg/client/informers/externalversions/bridgeoperator.k8s.cni.npwg.io/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

type BridgeConfHandler interface {
	OnBridgeConfigAdd(config *bridgeconfv1alpha1.BridgeConfiguration)
	OnBridgeConfigUpdate(oldConfig, config *bridgeconfv1alpha1.BridgeConfiguration)
	OnBridgeConfigDelete(config *bridgeconfv1alpha1.BridgeConfiguration)
	OnBridgeConfigSynced()
}

type BridgeConfConfig struct {
	listerSynced  cache.InformerSynced
	eventHandlers []BridgeConfHandler
}

func NewBridgeConfConfig(bridgeConfInformer bridgeconfinformerb1alpha1.BridgeConfigurationInformer, resyncPeriod time.Duration) *BridgeConfConfig {
	result := &BridgeConfConfig{
		listerSynced: bridgeConfInformer.Informer().HasSynced,
	}

	bridgeConfInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    result.handleAddBridgeConf,
			UpdateFunc: result.handleUpdateBridgeConf,
			DeleteFunc: result.handleDeleteBridgeConf,
		}, resyncPeriod,
	)

	return result
}

func (c *BridgeConfConfig) RegisterEventHandler(handler BridgeConfHandler) {
	c.eventHandlers = append(c.eventHandlers, handler)
}

func (c *BridgeConfConfig) Run(stopCh <-chan struct{}) {
	klog.Info("Starting bridgeconf config operator")

	if !cache.WaitForNamedCacheSync("bridgeconf", stopCh, c.listerSynced) {
		return
	}

	for i := range c.eventHandlers {
		klog.V(10).Infof("Calling handler.OnBridgeConfigSynced()")
		c.eventHandlers[i].OnBridgeConfigSynced()
	}
}

func (c *BridgeConfConfig) handleAddBridgeConf(obj interface{}) {
	bridgeConf, ok := obj.(*bridgeconfv1alpha1.BridgeConfiguration)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
		return
	}

	for i := range c.eventHandlers {
		klog.V(10).Infof("Calling handler.OnBridgeConfigAdd")
		c.eventHandlers[i].OnBridgeConfigAdd(bridgeConf)
	}
}

func (c *BridgeConfConfig) handleUpdateBridgeConf(oldObj, newObj interface{}) {
	oldBridgeConf, ok := oldObj.(*bridgeconfv1alpha1.BridgeConfiguration)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", oldObj))
		return
	}

	bridgeConf, ok := newObj.(*bridgeconfv1alpha1.BridgeConfiguration)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", oldObj))
		return
	}

	for i := range c.eventHandlers {
		klog.V(10).Infof("Calling handler.OnBridgeConfigUpdate")
		c.eventHandlers[i].OnBridgeConfigUpdate(oldBridgeConf, bridgeConf)
	}
}

func (c *BridgeConfConfig) handleDeleteBridgeConf(obj interface{}) {
	bridgeConf, ok := obj.(*bridgeconfv1alpha1.BridgeConfiguration)

	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
		}
		if bridgeConf, ok = tombstone.Obj.(*bridgeconfv1alpha1.BridgeConfiguration); !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
	}
	for i := range c.eventHandlers {
		klog.V(10).Infof("Calling handler.OnBridgeConfigDelete")
		c.eventHandlers[i].OnBridgeConfigDelete(bridgeConf)
	}
}

type BridgeConfInfo struct {
	BridgeConfig *bridgeconfv1alpha1.BridgeConfiguration
}

func (info *BridgeConfInfo) Name() string {
	return info.BridgeConfig.ObjectMeta.Name
}

func (info *BridgeConfInfo) Namespace() string {
	return info.BridgeConfig.ObjectMeta.Namespace
}

type BridgeConfMap map[types.NamespacedName]BridgeConfInfo

func (bcm *BridgeConfMap) Update(changes *BridgeConfChangeTracker) {
	if bcm != nil {
		bcm.apply(changes)
	}
}

func (bcm *BridgeConfMap) apply(changes *BridgeConfChangeTracker) {
	if bcm == nil || changes == nil {
		return
	}

	changes.lock.Lock()
	defer changes.lock.Unlock()
	for _, change := range changes.items {
		bcm.unmerge(change.previous)
		bcm.merge(change.current)
	}
	// clear changes after applying them to BridgeConfMap
	changes.items = make(map[types.NamespacedName]*bridgeConfChange)
	return
}

func (bcm *BridgeConfMap) merge(other BridgeConfMap) {
	for bridgeName, info := range other {
		(*bcm)[bridgeName] = info
	}
}

func (bcm *BridgeConfMap) unmerge(other BridgeConfMap) {
	for bridgeName := range other {
		delete(*bcm, bridgeName)
	}
}

type bridgeConfChange struct {
	previous BridgeConfMap
	current  BridgeConfMap
}

type BridgeConfChangeTracker struct {
	// lock protects items.
	lock sync.Mutex
	// items maps a name to its bridgeConfChange.
	items map[types.NamespacedName]*bridgeConfChange
}

func (bct *BridgeConfChangeTracker) newBridgeConfInfo(conf *bridgeconfv1alpha1.BridgeConfiguration) *BridgeConfInfo {
	info := &BridgeConfInfo{
		BridgeConfig: conf,
	}
	return info
}

func (bct *BridgeConfChangeTracker) bridgeConfToBridgeConfMap(conf *bridgeconfv1alpha1.BridgeConfiguration) BridgeConfMap {
	if conf == nil {
		return nil
	}

	bridgeConfMap := make(BridgeConfMap)
	bridgeConfInfo := bct.newBridgeConfInfo(conf)

	bridgeConfMap[types.NamespacedName{Namespace: conf.Namespace, Name: conf.Name}] = *bridgeConfInfo
	return bridgeConfMap
}

// Update
func (bct *BridgeConfChangeTracker) Update(previous, current *bridgeconfv1alpha1.BridgeConfiguration) bool {
	bridgeConf := current

	if bct == nil {
		return false
	}

	if bridgeConf == nil {
		bridgeConf = previous
	}
	if bridgeConf == nil {
		return false
	}

	namespacedName := types.NamespacedName{Namespace: bridgeConf.Namespace, Name: bridgeConf.Name}

	bct.lock.Lock()
	defer bct.lock.Unlock()

	change, exists := bct.items[namespacedName]
	if !exists {
		change = &bridgeConfChange{}
		prevBridgeConfMap := bct.bridgeConfToBridgeConfMap(previous)
		change.previous = prevBridgeConfMap
		bct.items[namespacedName] = change
	}

	curBridgeConfMap := bct.bridgeConfToBridgeConfMap(current)
	change.current = curBridgeConfMap
	if reflect.DeepEqual(change.previous, change.current) {
		delete(bct.items, namespacedName)
	}

	return len(bct.items) >= 0
}

func NewBridgeConfChangeTracker() *BridgeConfChangeTracker {
	return &BridgeConfChangeTracker{
		items: make(map[types.NamespacedName]*bridgeConfChange),
	}
}
