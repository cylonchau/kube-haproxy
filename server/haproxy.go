package main

import (
	goflag "flag"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	"github.com/cylonchau/kube-haproxy/server/app"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	command := app.NewProxyCommand()
	flagset := goflag.CommandLine
	klog.InitFlags(flagset)
	// TODO: once we switch everything over to Cobra commands, we can go back to calling
	// utilflag.InitFlags() (by removing its pflag.Parse() call). For now, we have to set the
	// normalize func and add the go flag set by hand.
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flagset)
	// utilflag.InitFlags()

	//defer klog.Flush()

	//
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
