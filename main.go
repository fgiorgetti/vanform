package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fgiorgetti/vanform/internal/van"
	"github.com/fgiorgetti/vanform/internal/van/kube"
	"github.com/fgiorgetti/vanform/internal/van/system"
	corev1 "k8s.io/api/core/v1"
)

const (
	version = "1.0.0"
)

func main() {
	cfg := parseFlags()
	var err error
	var controller van.Controller
	var stopCh = make(chan struct{})
	if cfg.Platform == "kubernetes" || cfg.Platform == "" {
		controller, err = kube.NewController(cfg)
		if err != nil {
			fmt.Println("Error creating controller:", err)
			os.Exit(1)
		}
	} else {
		controller = system.NewController()
	}
	doneCh := controller.Start(stopCh)
	handleShutdown(stopCh, doneCh)
}

func handleShutdown(stopCh, doneCh chan struct{}) {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	log.Println("Shutting down VAN Form controller")
	close(stopCh)

	gracefulTimeout := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-sigs:
			log.Println("Second interrupt, forcing VAN Form controller shutdown")
			os.Exit(1)
		case <-gracefulTimeout.C:
			log.Println("Graceful shutdown timed out, exiting now")
			os.Exit(1)
		case <-doneCh:
			log.Println("VAN Form controller shutdown completed")
			os.Exit(0)
		}
	}
}

func parseFlags() *van.ControllerConfig {
	// Use better approach for handling platform and namespace
	flags := flag.NewFlagSet("", flag.ExitOnError)
	c := new(van.ControllerConfig)
	StringVar(flags, &c.Platform, "platform", "SKUPPER_PLATFORM", "kubernetes", "The platform to use (choices: kubernetes, podman, docker or linux)")
	StringVar(flags, &c.Namespace, "namespace", "NAMESPACE", "", "The namespace scope for the controller")
	StringVar(flags, &c.WatchNamespace, "watch-namespace", "WATCH_NAMESPACE", corev1.NamespaceAll, "The namespace the controller should monitor for controlled resources (will monitor all if not specified)")
	StringVar(flags, &c.Kubeconfig, "kubeconfig", "KUBECONFIG", "", "A path to the kubeconfig file to use (kubernetes platform only")
	isVersion := flags.Bool("version", false, "Report the version of the Skupper System Controller")
	err := flags.Parse(os.Args[1:])
	if err != nil {
		fmt.Printf("error parsing flags: %v\n", err)
		os.Exit(1)
	}
	if *isVersion {
		fmt.Println(version)
		os.Exit(0)
	}
	return c
}

func StringVar(flags *flag.FlagSet, output *string, flagName string, envVarName string, defaultValue string, usage string) {
	flags.StringVar(output, flagName, stringEnvVar(envVarName, defaultValue), usage)
}

func stringEnvVar(name string, defaultValue string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return defaultValue
}
