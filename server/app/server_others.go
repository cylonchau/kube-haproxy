/*
Copyright 2014 The Kubernetes Authors.

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

// Package app does all of the work necessary to configure and run a
// Kubernetes app process.
package app

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	toolswatch "k8s.io/client-go/tools/watch"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	proxyconfigapi "k8s.io/kubernetes/pkg/proxy/apis/config"
	proxyconfigscheme "k8s.io/kubernetes/pkg/proxy/apis/config/scheme"
	utilnode "k8s.io/kubernetes/pkg/util/node"
	utilsnet "k8s.io/utils/net"

	kube_haproxy "github.com/cylonchau/kube-haproxy/api"
	"github.com/cylonchau/kube-haproxy/haproxy"
)

// timeoutForNodePodCIDR is the time to wait for allocators to assign a PodCIDR to the
// node after it is registered.
var timeoutForNodePodCIDR = 5 * time.Minute

// NewProxyServer returns a new ProxyServer.
func NewProxyServer(o *Options) (*ProxyServer, error) {
	return newProxyServer(o)
}

func newProxyServer(opt *Options) (*ProxyServer, error) {

	// We omit creation of pretty much everything if we run in cleanup mode
	if opt.cleanupAndExit {
		return &ProxyServer{}, nil
	}

	hostname, err := utilnode.GetHostname("")
	if err != nil {
		return nil, err
	}

	client, eventClient, err := createClients(opt.config)
	if err != nil {
		return nil, err
	}

	// Create event recorder
	eventBroadcaster := record.NewBroadcaster()
	recorder := eventBroadcaster.NewRecorder(proxyconfigscheme.Scheme, v1.EventSource{Component: "kube-proxy", Host: hostname})

	nodeRef := &v1.ObjectReference{
		Kind:      "Node",
		Name:      hostname,
		UID:       types.UID(hostname),
		Namespace: "",
	}

	var proxier kube_haproxy.Provider
	proxyMode := getProxyMode(opt.haproxyInfo.Mode)
	algorithm := Algorithm(int(opt.algorithm))
	opt.haproxyInfo.ServerAlgorithm = algorithm
	switch proxyMode {
	case proxyModeOF:
		proxier, err = haproxy.NewProxier(
			opt.syncPeriod,
			opt.minSyncPeriod,
			hostname,
			opt.haproxyInfo.Dev,
			recorder,
			&opt.haproxyInfo,
			proxyModeOF,
		)
	case proxyModeLocal:
		proxier, err = haproxy.NewProxier(
			opt.syncPeriod,
			opt.minSyncPeriod,
			hostname,
			opt.haproxyInfo.Dev,
			recorder,
			&opt.haproxyInfo,
			proxyModeLocal,
		)
	case proxyModeMeshAll:
		proxier, err = haproxy.NewProxier(
			opt.syncPeriod,
			opt.minSyncPeriod,
			hostname,
			opt.haproxyInfo.Dev,
			recorder,
			&opt.haproxyInfo,
			proxyModeMeshAll,
		)
	}

	return &ProxyServer{
		Client:            client,
		EventClient:       eventClient,
		Proxier:           proxier,
		Broadcaster:       eventBroadcaster,
		Recorder:          recorder,
		ProxyMode:         proxyMode,
		NodeRef:           nodeRef,
		ConfigSyncPeriod:  opt.configSyncPeriod.Duration,
		UseEndpointSlices: utilfeature.DefaultFeatureGate.Enabled(features.EndpointSliceProxying),
	}, nil
}

func waitForPodCIDR(client clientset.Interface, nodeName string) (*v1.Node, error) {
	// since allocators can assign the podCIDR after the node registers, we do a watch here to wait
	// for podCIDR to be assigned, instead of assuming that the Get() on startup will have it.
	ctx, cancelFunc := context.WithTimeout(context.TODO(), timeoutForNodePodCIDR)
	defer cancelFunc()

	fieldSelector := fields.OneTermEqualSelector("metadata.name", nodeName).String()
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (object runtime.Object, e error) {
			options.FieldSelector = fieldSelector
			return client.CoreV1().Nodes().List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (i watch.Interface, e error) {
			options.FieldSelector = fieldSelector
			return client.CoreV1().Nodes().Watch(ctx, options)
		},
	}
	condition := func(event watch.Event) (bool, error) {
		if n, ok := event.Object.(*v1.Node); ok {
			return n.Spec.PodCIDR != "" && len(n.Spec.PodCIDRs) > 0, nil
		}
		return false, fmt.Errorf("event object not of type Node")
	}

	evt, err := toolswatch.UntilWithSync(ctx, lw, &v1.Node{}, nil, condition)
	if err != nil {
		return nil, fmt.Errorf("timeout waiting for PodCIDR allocation to configure detect-local-mode %v: %v", proxyconfigapi.LocalModeNodeCIDR, err)
	}
	if n, ok := evt.Object.(*v1.Node); ok {
		return n, nil
	}
	return nil, fmt.Errorf("event object not of type node")
}

// detectNodeIP returns the nodeIP used by the proxier
// The order of precedence is:
// 1. config.bindAddress if bindAddress is not 0.0.0.0 or ::
// 2. the primary IP from the Node object, if set
// 3. if no IP is found it defaults to 127.0.0.1 and IPv4
func detectNodeIP(client clientset.Interface, hostname, bindAddress string) net.IP {
	nodeIP := net.ParseIP(bindAddress)
	if nodeIP.IsUnspecified() {
		nodeIP = utilnode.GetNodeIP(client, hostname)
	}
	if nodeIP == nil {
		klog.V(0).Infof("can't determine this node's IP, assuming 127.0.0.1; if this is incorrect, please set the --bind-address flag")
		nodeIP = net.ParseIP("127.0.0.1")
	}
	return nodeIP
}

// cidrTuple takes a comma separated list of CIDRs and return a tuple (ipv4cidr,ipv6cidr)
// The returned tuple is guaranteed to have the order (ipv4,ipv6) and if no cidr from a family is found an
// empty string "" is inserted.
func cidrTuple(cidrList string) [2]string {
	cidrs := [2]string{"", ""}
	foundIPv4 := false
	foundIPv6 := false

	for _, cidr := range strings.Split(cidrList, ",") {
		if utilsnet.IsIPv6CIDRString(cidr) && !foundIPv6 {
			cidrs[1] = cidr
			foundIPv6 = true
		} else if !foundIPv4 {
			cidrs[0] = cidr
			foundIPv4 = true
		}
		if foundIPv6 && foundIPv4 {
			break
		}
	}

	return cidrs
}

// nodeIPTuple takes an addresses and return a tuple (ipv4,ipv6)
// The returned tuple is guaranteed to have the order (ipv4,ipv6). The address NOT of the passed address
// will have "any" address (0.0.0.0 or ::) inserted.
func nodeIPTuple(bindAddress string) [2]net.IP {
	nodes := [2]net.IP{net.IPv4zero, net.IPv6zero}

	adr := net.ParseIP(bindAddress)
	if utilsnet.IsIPv6(adr) {
		nodes[1] = adr
	} else {
		nodes[0] = adr
	}

	return nodes
}

func getProxyMode(proxyMode string) string {
	switch proxyMode {
	case proxyModeLocal:
		return proxyModeLocal
	case proxyModeOF:
		return proxyModeOF
	case proxyModeMeshAll:
		return proxyModeMeshAll
	}
	klog.Warningf("Unknown proxy mode %q, assuming haproxy only fetchy mode", proxyMode)
	return proxyModeOF
}
