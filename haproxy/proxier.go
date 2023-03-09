/*
Copyright 2015 The Kubernetes Authors.

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

package haproxy

//
// NOTE: this needs to be tested in e2e since it uses iptables for everything.
//

import (
	"net"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amit7itz/goset"
	"github.com/haproxytech/models"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/util/async"
	"k8s.io/kubernetes/pkg/util/conntrack"
	utilnet "k8s.io/utils/net"

	"github.com/cylonchau/kube-haproxy/api"
	"github.com/cylonchau/kube-haproxy/controller"
)

// internal struct for string service information
type serviceInfo struct {
	*controller.BaseServiceInfo
	// The following fields are computed and stored for performance reasons.
	serviceNameString string
}

// returns a new proxy.ServicePort which abstracts a serviceInfo
func newServiceInfo(port *v1.ServicePort, service *v1.Service, baseInfo *controller.BaseServiceInfo) api.ServicePort {
	info := &serviceInfo{BaseServiceInfo: baseInfo}

	// Store the following for performance reasons.
	svcName := types.NamespacedName{Namespace: service.Namespace, Name: service.Name}
	svcPortName := api.ServicePortName{NamespacedName: svcName, Port: port.Name}
	info.serviceNameString = svcPortName.String()
	return info
}

// internal struct for endpoints information
type endpointsInfo struct {
	*controller.BaseEndpointInfo
	// The following fields we lazily compute and store here for performance
	// reasons. If the protocol is the same as you expect it to be, then the
	// chainName can be reused, otherwise it should be recomputed.
	protocol string
}

// returns a new proxy.Endpoint which abstracts a endpointsInfo
func newEndpointInfo(baseInfo *controller.BaseEndpointInfo) api.Endpoint {
	return &endpointsInfo{BaseEndpointInfo: baseInfo}
}

// Equal overrides the Equal() function implemented by proxy.BaseEndpointInfo.
func (e *endpointsInfo) Equal(other api.Endpoint) bool {
	o, ok := other.(*endpointsInfo)
	if !ok {
		klog.Error("Failed to cast endpointsInfo")
		return false
	}
	return e.Endpoint == o.Endpoint &&
		e.IsLocal == o.IsLocal &&
		e.protocol == o.protocol
}

// Proxier is an iptables based proxy for connections between a localhost:lport
// and services that provide the actual backends.
type Proxier struct {
	endpointsChanges *controller.EndpointChangeTracker
	serviceChanges   *controller.ServiceChangeTracker

	mu           sync.Mutex // protects the following fields
	serviceMap   controller.ServiceMap
	endpointsMap controller.EndpointsMap
	// Added as a member to the struct to allow injection for testing.
	haproxyHandle HaproxyHandle
	nodeLabels    map[string]string
	// endpointsSynced, endpointSlicesSynced, and servicesSynced are set to true
	// when corresponding objects are synced after startup. This is used to avoid
	// updating iptables with some partial data after kube-proxy restart.
	endpointsSynced      bool
	endpointSlicesSynced bool
	servicesSynced       bool
	initialized          int32
	syncRunner           *async.BoundedFrequencyRunner // governs calls to syncProxyRules
	syncPeriod           time.Duration
	serverAlgorithm      string
	// endpoints in haproxy object is Backend
	haproxyEndpointsList  map[string]*HaproxyInfo
	gracefuldeleteManager *GracefulTerminationManager
	//
	interfaceName string
	mode          string
	proxyIP       net.IP
	isHealthCheck bool
}

var _ api.Provider = &Proxier{}

func NewProxier(
	syncPeriod time.Duration,
	minSyncPeriod time.Duration,
	hostname string,
	interfaceName string,
	recorder record.EventRecorder,
	haproxyInfo *InitInfo,
	mode string,
) (*Proxier, error) {
	handler := NewHaproxyHandle(haproxyInfo)

	proxier := &Proxier{
		endpointsMap:          make(controller.EndpointsMap),
		serviceMap:            make(controller.ServiceMap),
		serviceChanges:        controller.NewServiceChangeTracker(newServiceInfo, recorder, nil),
		syncPeriod:            syncPeriod,
		haproxyHandle:         handler,
		interfaceName:         interfaceName,
		gracefuldeleteManager: NewGracefulTerminationManager(&handler),
		mode:                  mode,
		isHealthCheck:         haproxyInfo.IsCheckServer,
		serverAlgorithm:       haproxyInfo.ServerAlgorithm,
	}
	var (
		localIP      net.IP
		err          error
		allInterface = true
	)
	if interfaceName == "" {
		proxier.interfaceName = defaultInterface
	} else {
		localIP, err = GetLocalAddr(proxier.interfaceName)
		if err != nil {
			localIP = net.ParseIP("127.0.0.1")
		}
	}
	if allInterface {
		proxier.proxyIP = net.ParseIP("0.0.0.0")
	} else {
		if getProcessByName(processName) {
			remoteIP := haproxyInfo.Host
			u, err := url.Parse(remoteIP)
			if err != nil {
				proxier.proxyIP = net.ParseIP("")
			}
			proxier.proxyIP = net.ParseIP(u.Hostname())

		} else {
			proxier.proxyIP = localIP
		}
	}
	isIPv6 := utilnet.IsIPv6(proxier.proxyIP)
	endpointSlicesEnabled := utilfeature.DefaultFeatureGate.Enabled(features.EndpointSliceProxying)
	proxier.endpointsChanges = controller.NewEndpointChangeTracker(hostname, newEndpointInfo, &isIPv6, recorder, endpointSlicesEnabled, nil)

	burstSyncs := 2
	klog.V(2).Infof("haproxy rules sync params: minSyncPeriod=%v, syncPeriod=%v, burstSyncs=%d",
		minSyncPeriod, syncPeriod, burstSyncs)
	// We pass syncPeriod to ipt.Monitor, which will call us only if it needs to.
	// We need to pass *some* maxInterval to NewBoundedFrequencyRunner anyway though.
	// time.Hour is arbitrary.
	proxier.syncRunner = async.NewBoundedFrequencyRunner("sync-runner", proxier.syncProxyRules, minSyncPeriod, time.Hour, burstSyncs)
	proxier.gracefuldeleteManager.Run()
	return proxier, nil
}

func (proxier *Proxier) cleanLegacyService(activeServices map[string]bool, currentServices map[string]Service) {
	for cs := range currentServices {
		svc := currentServices[cs]
		if _, ok := activeServices[cs]; !ok {
			klog.V(4).Infof("Delete service %s", svc.Name)
			if proxier.haproxyHandle.DeleteFrontend(svc.Frontend.Name) {
				klog.Errorf("Failed to delete frontend %s.", svc.Frontend.Name)
			}
			if proxier.haproxyHandle.DeleteBackend(svc.Backend.Name) {
				klog.Errorf("Failed to delete backend %s.", svc.Backend.Name)
			}
		}
	}
}

func (proxier *Proxier) syncService(info HaproxyInfo) error {
	backend := proxier.haproxyHandle.GetBackend(info.Backend.Name)
	if backend.Mode != "udp" {
		if reflect.DeepEqual(backend, models.Backend{}) {
			klog.V(3).Infof("Adding new backend %s", info.Backend.Name)
			ok, err := proxier.haproxyHandle.AddBackend(&info.Backend)
			if err != nil && !ok {
				return err
			}
		} else {
			if !reflect.DeepEqual(backend, info.Backend) {
				klog.V(3).Infof("Backend service %s was changed", info.Backend.Name)
				_, err := proxier.haproxyHandle.ReplaceBackend(backend.Name, &info.Backend)
				if err != nil {
					return err
				}
			}
		}
	}

	frontend := proxier.haproxyHandle.GetFrontend(info.Frontend.Name)
	if frontend.Mode != "udp" {
		if reflect.DeepEqual(frontend, models.Frontend{}) {
			klog.V(3).Infof("Adding new frontend %s", info.Frontend.Name)
			ok, err := proxier.haproxyHandle.AddFrontend(&info.Frontend)
			if err != nil && !ok {
				return err
			}
		} else {
			if !reflect.DeepEqual(frontend, info.Frontend) {
				klog.V(3).Infof("Frontend service %s was changed", info.Frontend.Name)
				_, err := proxier.haproxyHandle.ReplaceFrontend(&frontend, &info.Frontend)
				if err != nil {
					return err
				}
			}
		}
	}

	bind := proxier.haproxyHandle.GetBind(info.Bind.Name, info.Frontend.Name)
	if reflect.DeepEqual(bind, models.Bind{}) {
		klog.V(3).Infof("Bind %v:%d to frontend %s", bind.Address, bind.Port, info.Frontend.Name)
		ok, err := proxier.haproxyHandle.AddBind(&info.Bind, info.Frontend.Name)
		if err != nil && !ok {
			return err
		}
	} else {
		if !reflect.DeepEqual(info.Bind, bind) {
			klog.V(3).Infof("Frontend bind %s:%d was changed", info.Bind.Name, info.Bind.Port)
			_, err := proxier.haproxyHandle.replaceBind(bind.Name, info.Frontend.Name, &info.Bind)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (proxier *Proxier) syncEndpoint(svcPortName api.ServicePortName, backendName string) error {
	// curEndpoints represents IPVS destinations listed from current system.
	curEndpoints := goset.NewSet[Server]()
	// newEndpoints represents Endpoints watched from API Server.
	newEndpoints := goset.NewSet[Server]()

	curDests := proxier.haproxyHandle.GetServers(backendName)
	if len(curDests) != 0 {
		for _, des := range curDests {
			curEndpoints.Add(des)
		}
	} else {
		klog.Warningf("Failed to list haproxy destinations, The server %s list is null", svcPortName)
	}

	endpoints := proxier.endpointsMap[svcPortName]
	//
	//// Service Topology will not be enabled in the following cases:
	//// 1. externalTrafficPolicy=Local (mutually exclusive with service topology).
	//// 2. ServiceTopology is not enabled.
	//// 3. EndpointSlice is not enabled (service topology depends on endpoint slice
	//// to get topology information).
	//
	for _, epInfo := range endpoints {
		port, _ := epInfo.Port()
		epSrv := Server{
			Address: epInfo.IP(),
			Port:    int64(port),
			Name:    SERVER_PREFIX + epInfo.String(),
		}
		if proxier.isHealthCheck {
			epSrv.Check = "enabled"
		}
		newEndpoints.Add(epSrv)
	}

	// Create new endpoints

	for _, item := range newEndpoints.Items() {
		if curEndpoints.Contains(item) {
			if !proxier.gracefuldeleteManager.InTerminationList(item, backendName) {
				continue
			}
			klog.V(4).Infof("new server %s is in graceful delete list", item.Address)
			err := proxier.gracefuldeleteManager.MoveRSOutofGracefulDeleteList(item, backendName)
			if err != nil {
				klog.Errorf("Failed to delete endpoint: %s in gracefulDeleteQueue, error: %v", item.Address, err)
				continue
			}
		} else {
			klog.V(4).Infof("Add a new server %s to backend %s", item.Address, backendName)
			if ok, err := proxier.haproxyHandle.AddServerToBackend(&item, backendName); err != nil && !ok {
				klog.Errorf("Failed to add destination: [%s=>%s], error: %v", backendName, item.Address, err)
				continue
			}
		}
	}

	// Delete old endpoints
	curEndpoints.Difference(newEndpoints).For(func(server Server) {
		if proxier.gracefuldeleteManager.InTerminationList(server, backendName) {
			return
		}
		klog.V(4).Infof("Using graceful delete to server: %s", server.Name)
		err := proxier.gracefuldeleteManager.GracefulDeleteSrv(server, backendName)
		if err != nil {
			klog.Errorf("Failed to delete destination: %v, error: %v", server.Address, err)
			return
		}
	})
	return nil
}

func (proxier *Proxier) syncProxyRules() {
	proxier.mu.Lock()
	defer proxier.mu.Unlock()
	// don't sync rules till we've received services and endpoints
	if !proxier.isInitialized() {
		klog.V(2).Info("Not syncing haproxy rules until Services and Endpoints have been received from master")
		return
	}

	start := time.Now()
	defer func() {
		klog.V(4).Infof("syncProxyRules took %v", time.Since(start))
	}()

	// We assume that if this was called, we really want to sync them,
	// even if nothing changed in the meantime. In other words, callers are
	// responsible for detecting no-op changes and not calling this function.
	serviceUpdateResult := controller.UpdateServiceMap(proxier.serviceMap, proxier.serviceChanges)
	endpointUpdateResult := proxier.endpointsMap.Update(proxier.endpointsChanges)

	staleServices := serviceUpdateResult.UDPStaleClusterIP

	// merge stale services gathered from updateEndpointsMap
	for _, svcPortName := range endpointUpdateResult.StaleServiceNames {
		//  conntrack.IsClearConntrackNeeded(svcInfo.Protocol()
		// 用于UDP与SCTP清理陈旧连接时使用
		if svcInfo, ok := proxier.serviceMap[svcPortName]; ok && svcInfo != nil && conntrack.IsClearConntrackNeeded(svcInfo.Protocol()) {
			klog.V(2).Infof("Stale %s service %v -> %s", strings.ToLower(string(svcInfo.Protocol())), svcPortName, svcInfo.ClusterIP().String())
			staleServices.Insert(svcInfo.ClusterIP().String())
			for _, extIP := range svcInfo.ExternalIPStrings() {
				staleServices.Insert(extIP)
			}
		}
	}

	klog.V(3).Infof("Syncing haproxy Proxier rules")
	if !proxier.haproxyHandle.EnsureHaproxy() {
		klog.Error("haproxy status is unkown, please check")
		return
	}

	activeServices := map[string]bool{}
	// currentIPVSServices represent IPVS services listed from the system
	currentServices := make(map[string]Service)

	// 暂时不涉及node port
	//hasNodePort := false
	// Build haproxy rules for each service.
	for svcName, svc := range proxier.serviceMap {
		// 拿到一个service
		svcInfo, ok := svc.(*serviceInfo)
		if !ok {
			klog.Errorf("Failed to cast serviceInfo %q", svcName.String())
			continue
		}

		//isIPv6 := utilnet.IsIPv6(svcInfo.ClusterIP())
		protocol := strings.ToLower(string(svcInfo.Protocol()))
		// Precompute svcNameString; with many services the many calls
		// to ServicePortName.String() show up in CPU profiles.
		svcNameString := svcName.String()
		svcNameString = strings.ReplaceAll(svcNameString, "/", ".")
		svcNameString = strings.ReplaceAll(svcNameString, ":", ".")
		port := int64(svcInfo.Port())

		// 这里是作为 kube-proxy 中 service 资源
		// 转换为 haproxy 中为 frontend + backend 资源
		// 拼装service部分
		backendEntry := models.Backend{
			Name: BACKEND_PREFIX + svcNameString,
			Balance: &models.Balance{
				Algorithm: proxier.serverAlgorithm,
			},
			Mode:         protocol,
			CheckTimeout: &checkTimeout,
		}
		switch backendEntry.Mode {
		case "tcp":

		case "http":
			backendEntry.Httpchk = &models.Httpchk{
				Method: "get",
				URI:    "/ping",
			}
			backendEntry.Forwardfor = &models.Forwardfor{Enabled: &forwordEnable}
		default:
			backendEntry.Mode = "tcp"
		}

		frontendEntry := models.Frontend{
			Name:           FRONTEND_PREFIX + svcNameString,
			DefaultBackend: BACKEND_PREFIX + svcNameString,
			Mode:           protocol,
		}

		switch frontendEntry.Mode {
		case "http":
			backendEntry.Forwardfor = &models.Forwardfor{Enabled: &forwordEnable}
		}
		bindEntry := models.Bind{
			Name:    BIND_PREFIX + svcNameString,
			Port:    &port,
			Address: proxier.proxyIP.String(),
		}
		obj := HaproxyInfo{
			Backend:  backendEntry,
			Frontend: frontendEntry,
			Bind:     bindEntry,
		}

		if proxier.mode == "of" && (strings.Contains(svcNameString, "default.kubernetes") || strings.Contains(svcNameString, "kube-system.kube-dns")) {
			continue
		}

		if err := proxier.syncService(obj); err == nil {
			activeServices[svcNameString] = true
			// ExternalTrafficPolicy only works for NodePort and external LB traffic, does not affect ClusterIP
			// So we still need clusterIP rules in onlyNodeLocalEndpoints mode.
			if err := proxier.syncEndpoint(svcName, obj.Backend.Name); err != nil {
				klog.Errorf("Failed to sync endpoint: %v, err: %v", proxier.endpointsMap[svcName], err)
			}
		} else {
			klog.Errorf("Failed to sync service: %v, err: %v", svcNameString, err)
		}
	}

	// Clean up legacy IPVS services and unbind addresses
	services := proxier.haproxyHandle.GetServices()
	if len(services) == 0 {
		klog.Warning("Get all services error, the server list is null")
	} else {
		if len(services) > 0 {
			for _, svc := range services {
				currentServices[svc.Name] = svc
			}
		}
	}
	proxier.cleanLegacyService(activeServices, currentServices)
}

// Sync is called to synchronize the proxier state to iptables as soon as possible.
func (proxier *Proxier) Sync() {
	proxier.syncRunner.Run()
}

// SyncLoop runs periodic work.  This is expected to run as a goroutine or as the main loop of the app.  It does not return.
func (proxier *Proxier) SyncLoop() {
	proxier.syncRunner.Loop(wait.NeverStop)
}

func (proxier *Proxier) setInitialized(value bool) {
	var initialized int32
	if value {
		initialized = 1
	}
	atomic.StoreInt32(&proxier.initialized, initialized)
}

func (proxier *Proxier) isInitialized() bool {
	return atomic.LoadInt32(&proxier.initialized) > 0
}

// OnServiceAdd is called whenever creation of new service object
// is observed.
func (proxier *Proxier) OnServiceAdd(service *v1.Service) {
	proxier.OnServiceUpdate(nil, service)
}

// OnServiceUpdate is called whenever modification of an existing
// service object is observed.
func (proxier *Proxier) OnServiceUpdate(oldService, service *v1.Service) {
	if proxier.serviceChanges.Update(oldService, service) && proxier.isInitialized() {
		proxier.Sync()
	}
}

// OnServiceDelete is called whenever deletion of an existing service
// object is observed.
func (proxier *Proxier) OnServiceDelete(service *v1.Service) {
	proxier.OnServiceUpdate(service, nil)

}

// OnServiceSynced is called once all the initial event handlers were
// called and the state is fully propagated to local cache.
func (proxier *Proxier) OnServiceSynced() {
	proxier.mu.Lock()
	proxier.servicesSynced = true
	proxier.setInitialized(proxier.endpointsSynced)
	proxier.mu.Unlock()

	// Sync unconditionally - this is called once per lifetime.
	proxier.syncProxyRules()
}

// OnEndpointsAdd is called whenever creation of new endpoints object
// is observed.
func (proxier *Proxier) OnEndpointsAdd(endpoints *v1.Endpoints) {
	proxier.OnEndpointsUpdate(nil, endpoints)
}

// OnEndpointsUpdate is called whenever modification of an existing
// endpoints object is observed.
func (proxier *Proxier) OnEndpointsUpdate(oldEndpoints, endpoints *v1.Endpoints) {
	if proxier.endpointsChanges.Update(oldEndpoints, endpoints) && proxier.isInitialized() {
		proxier.Sync()
	}
}

// OnEndpointsDelete is called whenever deletion of an existing endpoints
// object is observed.
func (proxier *Proxier) OnEndpointsDelete(endpoints *v1.Endpoints) {
	proxier.OnEndpointsUpdate(endpoints, nil)
}

// OnEndpointsSynced is called once all the initial event handlers were
// called and the state is fully propagated to local cache.
func (proxier *Proxier) OnEndpointsSynced() {
	proxier.mu.Lock()
	proxier.endpointsSynced = true
	proxier.setInitialized(proxier.servicesSynced)
	proxier.mu.Unlock()

	// Sync unconditionally - this is called once per lifetime.
	proxier.syncProxyRules()
}

// OnEndpointSliceAdd is called whenever creation of a new endpoint slice object
// is observed.
func (proxier *Proxier) OnEndpointSliceAdd(endpointSlice *discovery.EndpointSlice) {
	if proxier.endpointsChanges.EndpointSliceUpdate(endpointSlice, false) && proxier.isInitialized() {
		proxier.Sync()
	}
}

// OnEndpointSliceUpdate is called whenever modification of an existing endpoint
// slice object is observed.
func (proxier *Proxier) OnEndpointSliceUpdate(_, endpointSlice *discovery.EndpointSlice) {
	if proxier.endpointsChanges.EndpointSliceUpdate(endpointSlice, false) && proxier.isInitialized() {
		proxier.Sync()
	}
}

// OnEndpointSliceDelete is called whenever deletion of an existing endpoint slice
// object is observed.
func (proxier *Proxier) OnEndpointSliceDelete(endpointSlice *discovery.EndpointSlice) {
	if proxier.endpointsChanges.EndpointSliceUpdate(endpointSlice, true) && proxier.isInitialized() {
		proxier.Sync()
	}
}

// OnEndpointSlicesSynced is called once all the initial event handlers were
// called and the state is fully propagated to local cache.
func (proxier *Proxier) OnEndpointSlicesSynced() {
	proxier.mu.Lock()
	proxier.endpointSlicesSynced = true
	proxier.setInitialized(proxier.servicesSynced)
	proxier.mu.Unlock()

	// Sync unconditionally - this is called once per lifetime.
	proxier.syncProxyRules()
}
