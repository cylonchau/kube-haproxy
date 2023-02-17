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
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/component-base/version/verflag"

	gerrors "github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/component-base/version"
	"k8s.io/klog/v2"
	"k8s.io/kube-proxy/config/v1alpha1"
	api "k8s.io/kubernetes/pkg/apis/core"
	kubeproxyconfig "k8s.io/kubernetes/pkg/proxy/apis/config"
	proxyconfigscheme "k8s.io/kubernetes/pkg/proxy/apis/config/scheme"
	kubeproxyconfigv1alpha1 "k8s.io/kubernetes/pkg/proxy/apis/config/v1alpha1"
	"k8s.io/kubernetes/pkg/proxy/config"
	proxyutil "k8s.io/kubernetes/pkg/proxy/util"

	kube_haproxy "github.com/cylonchau/kube-haproxy/api"
	"github.com/cylonchau/kube-haproxy/haproxy"
)

const (
	proxyModeOF      = "of"
	proxyModeLocal   = "local"
	proxyModeMeshAll = "mesh"
)

// proxyRun defines the interface to run a specified ProxyServer
type proxyRun interface {
	Run() error
	CleanupAndExit(o *haproxy.InitInfo) error
}

// Options contains everything necessary to create and run a proxy server.
type Options struct {
	config componentbaseconfig.ClientConnectionConfiguration
	// CleanupAndExit, when true, makes the proxy server clean up iptables and ipvs rules, then exit.
	cleanupAndExit bool
	// proxyServer is the interface to run the proxy server
	proxyServer proxyRun
	// errCh is the channel that errors will be sent
	errCh                     chan error
	syncPeriod, minSyncPeriod time.Duration
	configSyncPeriod          metav1.Duration
	haproxyInfo               haproxy.InitInfo
}

func (o *Options) addOSFlags(fs *pflag.FlagSet) {}

// AddFlags adds flags to fs and binds them to options.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	o.addOSFlags(fs)
	fs.DurationVar(&o.syncPeriod, "sync-period", time.Duration(30), "The maximum interval of how often haproxy rules are refreshed (e.g. '5s', '1m', '2h22m').  Must be greater than 0.")
	fs.DurationVar(&o.minSyncPeriod, "min-sync-period", time.Duration(10), "The minimum interval of how often the haproxy rules can be refreshed as endpoints and services change (e.g. '5s', '1m', '2h22m').")
	fs.BoolVar(&o.cleanupAndExit, "cleanup", false, "If true cleanup haproxy frontends/backends/servers/binds rules and exit.")
	fs.DurationVar(&o.configSyncPeriod.Duration, "config-sync-period", o.configSyncPeriod.Duration, "How often configuration from the apiserver is refreshed.  Must be greater than 0.")

	fs.StringVar(&o.config.Kubeconfig, "kubeconfig", o.config.Kubeconfig, "Path to kubeconfig file with authorization information (the master location is set by the master flag).")
	fs.StringVar(&o.config.ContentType, "kube-api-content-type", o.config.ContentType, "Content type of requests sent to apiserver.")
	fs.Float32Var(&o.config.QPS, "kube-api-qps", o.config.QPS, "QPS to use while talking with kubernetes apiserver")
	fs.Int32Var(&o.config.Burst, "kube-api-burst", o.config.Burst, "Burst to use while talking with kubernetes apiserver")

	fs.StringVar(&o.haproxyInfo.Mode, "proxy-mode", "of", "Which proxy mode to use: 'onlyfetch' or 'local' (similar kube-proxy) or without proxy. If blank, default only fetch.")
	fs.StringVar(&o.haproxyInfo.Dev, "interface", "eth0", "can specify special network interface name (only local mode).")
	fs.StringVar(&o.haproxyInfo.User, "user", o.haproxyInfo.User, "control access to frontend/backend/listen sections or to http stats by allowing only authenticated and authorized user.")
	fs.StringVar(&o.haproxyInfo.Passwd, "passwd", o.haproxyInfo.Passwd, "specify current user's password. Both secure (encrypted) and insecure (unencrypted) passwords can be used.")
	fs.StringVar(&o.haproxyInfo.Host, "dataplaneapi", "http://127.0.0.1:5555", "specify dataplaneapi address.")

}

// NewOptions returns initialized Options
func NewOptions() *Options {
	return &Options{
		errCh: make(chan error),
	}
}

// Complete completes all the required options.
func (o *Options) Complete() error {
	return nil
}

func (o *Options) errorHandler(err error) {
	o.errCh <- err
}

// Run runs the specified ProxyServer.
func (o *Options) Run() error {
	defer close(o.errCh)

	proxyServer, err := NewProxyServer(o)
	if err != nil {
		return err
	}

	if o.cleanupAndExit {
		return proxyServer.CleanupAndExit(&o.haproxyInfo)
	}

	o.proxyServer = proxyServer
	return o.runLoop()
}

// runLoop will watch on the update change of the proxy server's configuration file.
// Return an error when updated
func (o *Options) runLoop() error {
	// run the proxy in goroutine
	go func() {
		err := o.proxyServer.Run()
		o.errCh <- err
	}()

	for {
		err := <-o.errCh
		if err != nil {
			return err
		}
	}
}

// addressFromDeprecatedFlags returns server address from flags
// passed on the command line based on the following rules:
// 1. If port is 0, disable the server (e.g. set address to empty).
// 2. Otherwise, set the port portion of the config accordingly.
func addressFromDeprecatedFlags(addr string, port int32) string {
	if port == 0 {
		return ""
	}
	return proxyutil.AppendPortIfNeeded(addr, port)
}

// newLenientSchemeAndCodecs returns a scheme that has only v1alpha1 registered into
// it and a CodecFactory with strict decoding disabled.
func newLenientSchemeAndCodecs() (*runtime.Scheme, *serializer.CodecFactory, error) {
	lenientScheme := runtime.NewScheme()
	if err := kubeproxyconfig.AddToScheme(lenientScheme); err != nil {
		return nil, nil, fmt.Errorf("failed to add kube-proxy config API to lenient scheme: %v", err)
	}
	if err := kubeproxyconfigv1alpha1.AddToScheme(lenientScheme); err != nil {
		return nil, nil, fmt.Errorf("failed to add kube-proxy config v1alpha1 API to lenient scheme: %v", err)
	}
	lenientCodecs := serializer.NewCodecFactory(lenientScheme, serializer.DisableStrict)
	return lenientScheme, &lenientCodecs, nil
}

// loadConfigFromFile loads the contents of file and decodes it as a
// KubeProxyConfiguration object.
func (o *Options) loadConfigFromFile(file string) (*kubeproxyconfig.KubeProxyConfiguration, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	return o.loadConfig(data)
}

// loadConfig decodes a serialized KubeProxyConfiguration to the internal type.
func (o *Options) loadConfig(data []byte) (*kubeproxyconfig.KubeProxyConfiguration, error) {

	configObj, gvk, err := proxyconfigscheme.Codecs.UniversalDecoder().Decode(data, nil, nil)
	if err != nil {
		// Try strict decoding first. If that fails decode with a lenient
		// decoder, which has only v1alpha1 registered, and log a warning.
		// The lenient path is to be dropped when support for v1alpha1 is dropped.
		if !runtime.IsStrictDecodingError(err) {
			return nil, gerrors.Wrap(err, "failed to decode")
		}

		_, lenientCodecs, lenientErr := newLenientSchemeAndCodecs()
		if lenientErr != nil {
			return nil, lenientErr
		}

		configObj, gvk, lenientErr = lenientCodecs.UniversalDecoder().Decode(data, nil, nil)
		if lenientErr != nil {
			// Lenient decoding failed with the current version, return the
			// original strict error.
			return nil, fmt.Errorf("failed lenient decoding: %v", err)
		}

		// Continue with the v1alpha1 object that was decoded leniently, but emit a warning.
		klog.Warningf("using lenient decoding as strict decoding failed: %v", err)
	}

	proxyConfig, ok := configObj.(*kubeproxyconfig.KubeProxyConfiguration)
	if !ok {
		return nil, fmt.Errorf("got unexpected config type: %v", gvk)
	}
	return proxyConfig, nil
}

// ApplyDefaults applies the default values to Options.
func (o *Options) ApplyDefaults(in *kubeproxyconfig.KubeProxyConfiguration) (*kubeproxyconfig.KubeProxyConfiguration, error) {
	external, err := proxyconfigscheme.Scheme.ConvertToVersion(in, v1alpha1.SchemeGroupVersion)
	if err != nil {
		return nil, err
	}

	proxyconfigscheme.Scheme.Default(external)

	internal, err := proxyconfigscheme.Scheme.ConvertToVersion(external, kubeproxyconfig.SchemeGroupVersion)
	if err != nil {
		return nil, err
	}

	out := internal.(*kubeproxyconfig.KubeProxyConfiguration)

	return out, nil
}

// NewProxyCommand creates a *cobra.Command object with default parameters
func NewProxyCommand() *cobra.Command {
	opts := NewOptions()
	cmd := &cobra.Command{
		Use: "kube-haproxy",
		Long: `The Kubernetes network proxy proxier. This
reflects services as defined in the Kubernetes API on each node and can do simple
TCP, UDP, and SCTP stream forwarding or round robin TCP, UDP, and SCTP forwarding across a set of backends.
Service cluster IPs and ports are currently found through Docker-links-compatible
environment variables specifying ports opened by the service proxy. There is an optional
addon that provides cluster DNS for these cluster IPs. The user must create a service
with the apiserver API to configure the proxy.`,
		Run: func(cmd *cobra.Command, args []string) {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			if err := opts.Complete(); err != nil {
				klog.Fatalf("failed complete: %v", err)
			}

			if err := opts.Run(); err != nil {
				klog.Exit(err)
			}
		},
		Args: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if len(arg) > 0 {
					return fmt.Errorf("%q does not take any arguments, got %q", cmd.CommandPath(), args)
				}
			}
			return nil
		},
	}

	var err error
	if err != nil {
		klog.Fatalf("unable to create flag defaults: %v", err)
	}

	opts.AddFlags(cmd.Flags())
	// TODO handle error
	cmd.MarkFlagFilename("config", "yaml", "yml", "json")

	return cmd
}

// ProxyServer represents all the parameters required to start the Kubernetes proxy server. All
// fields are required.
type ProxyServer struct {
	ProxyMode           string
	Proxier             kube_haproxy.Provider
	Client              clientset.Interface
	EventClient         v1core.EventsGetter
	Broadcaster         record.EventBroadcaster
	Recorder            record.EventRecorder
	NodeRef             *v1.ObjectReference
	BindAddressHardFail bool
	ConfigSyncPeriod    time.Duration
}

// createClients creates a kube client and an event client from the given config and masterOverride.
// TODO remove masterOverride when CLI flags are removed.
func createClients(config componentbaseconfig.ClientConnectionConfiguration) (clientset.Interface, v1core.EventsGetter, error) {
	var kubeConfig *rest.Config
	var err error

	if len(config.Kubeconfig) == 0 {
		klog.Info("Neither kubeconfig file nor master URL was specified. Falling back to in-cluster config.")
		kubeConfig, err = rest.InClusterConfig()
	} else {
		kubeConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: config.Kubeconfig},
			&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: ""}}).ClientConfig()
	}
	if err != nil {
		return nil, nil, err
	}

	kubeConfig.AcceptContentTypes = config.AcceptContentTypes
	kubeConfig.ContentType = config.ContentType
	kubeConfig.QPS = config.QPS
	kubeConfig.Burst = int(config.Burst)

	client, err := clientset.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	eventClient, err := clientset.NewForConfig(kubeConfig)
	if err != nil {
		return nil, nil, err
	}

	return client, eventClient.CoreV1(), nil
}

// Run runs the specified ProxyServer.  This should never exit (unless CleanupAndExit is set).
// TODO: At the moment, Run() cannot return a nil error, otherwise it's caller will never exit. Update callers of Run to handle nil errors.
func (s *ProxyServer) Run() error {
	// To help debugging, immediately log version
	klog.Infof("Version: %+v", version.Get())

	if s.Broadcaster != nil && s.EventClient != nil {
		s.Broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: s.EventClient.Events("")})
	}

	var errCh chan error
	if s.BindAddressHardFail {
		errCh = make(chan error)
	}

	// Make informers that filter out objects that want a non-default service proxy.
	informerFactory := informers.NewSharedInformerFactoryWithOptions(s.Client, s.ConfigSyncPeriod)

	// Create configs (i.e. Watches for Services and Endpoints or EndpointSlices)
	// Note: RegisterHandler() calls need to happen before creation of Sources because sources
	// only notify on changes, and the initial update (on process start) may be lost if no handlers
	// are registered yet.
	serviceConfig := config.NewServiceConfig(informerFactory.Core().V1().Services(), s.ConfigSyncPeriod)
	serviceConfig.RegisterEventHandler(s.Proxier)
	go serviceConfig.Run(wait.NeverStop)

	endpointsConfig := config.NewEndpointsConfig(informerFactory.Core().V1().Endpoints(), s.ConfigSyncPeriod)
	endpointsConfig.RegisterEventHandler(s.Proxier)
	go endpointsConfig.Run(wait.NeverStop)

	// This has to start after the calls to NewServiceConfig and NewEndpointsConfig because those
	// functions must configure their shared informer event handlers first.
	informerFactory.Start(wait.NeverStop)

	// Birth Cry after the birth is successful
	s.birthCry()

	go s.Proxier.SyncLoop()

	return <-errCh
}

func (s *ProxyServer) birthCry() {
	s.Recorder.Eventf(s.NodeRef, api.EventTypeNormal, "Starting", "Starting kube-proxy.")
}

// CleanupAndExit remove iptables rules and ipset/ipvs rules in ipvs proxy mode
// and exit if success return nil
func (s *ProxyServer) CleanupAndExit(o *haproxy.InitInfo) error {
	haproxyHandler := haproxy.NewHaproxyHandle(o)
	infos := haproxyHandler.GetAllService()
	if reflect.DeepEqual(infos, haproxy.Services{}) {
		return errors.New("Get all services error.")
	}

	var encounteredError bool
	klog.V(3).Infof("Removing haproxy backend rules.")
	for _, svc := range infos.Backend {
		encounteredError = haproxyHandler.DeleteBackend(svc.Name)
	}
	klog.V(3).Infof("Removing haproxy frontend rules.")
	for _, svc := range infos.Frontend {
		if svc.Name == "stats" {
			continue
		}
		encounteredError = haproxyHandler.DeleteFrontend(svc.Name)
	}

	if encounteredError {
		return errors.New("encountered an error while tearing down rules")
	}

	return nil
}
