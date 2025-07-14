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

//func tokenHandlerSystem() {
//	handler := system.NewTokenHandler("west")
//	tokens, err := handler.Load()
//	if err != nil {
//		log.Fatalf("Error loading tokens: %v", err)
//	}
//	for _, token := range tokens {
//		fmt.Println(token.Link)
//		fmt.Println(token.Secret)
//	}
//}
//
//func tokenHandlerKubeSave() {
//	namespace := "sk5"
//	c, err := kube.NewClient(namespace, "", "")
//	if err != nil {
//		log.Fatal(err)
//	}
//	token := loadToken()
//	handler := kube.NewTokenHandler(c)
//	err = handler.Save(token)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println("Token created successfully on", namespace)
//}
//
//func tokenHandlerSystemSave() {
//	namespace := "west"
//	handler := system.NewTokenHandler(namespace)
//	token := loadToken()
//	err := handler.Save(token)
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println("Token created successfully on", namespace)
//}
//
//func loadToken() *van.Token {
//	var token = new(van.Token)
//	sk1Link, err := os.ReadFile("/tmp/sk1-link.yaml")
//	if err != nil {
//		log.Fatal(err)
//	}
//	err = token.Unmarshal(string(sk1Link))
//	if err != nil {
//		log.Fatal(err)
//	}
//	return token
//}
//
//func tokenHandlerKube() {
//	c, err := kube.NewClient("sk4", "", "")
//	if err != nil {
//		log.Fatal(err)
//	}
//	handler := kube.NewTokenHandler(c)
//	tokens, err := handler.Load()
//	if err != nil {
//		log.Fatal(err)
//	}
//	for _, token := range tokens {
//		fmt.Println(token)
//	}
//}
//
//func west() {
//	v := &van.Config{
//		VAN:    "company-a",
//		URL:    "http://localhost:8200",
//		Path:   "skupper",
//		Secret: "vault-secret",
//		Zones: []van.Zone{
//			{
//				Name:          "west",
//				ReachableFrom: []string{"east", "north", "south"},
//				EndpointHost:  "",
//			},
//		},
//	}
//	// the info below should come from the Secret defined through van.Config.Secret (vault-secret)
//	authPath := "approle"
//	roleId := "d859b2db-e31f-39e4-29d9-a07e7325ea94"
//	secretId := "6abfb20f-0007-338e-76aa-406cddd7eb9b"
//	c, err := client.NewAppRoleClient(v, authPath, roleId, secretId)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// authenticate
//	_, err = c.Login(context.Background())
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// publish token
//	err = c.PublishToken(fakeToken(), "sk1", "west", "east")
//	if err != nil {
//		log.Fatal(err)
//	}
//}
//
//func east() {
//	v := &van.Config{
//		VAN:    "company-a",
//		URL:    "http://localhost:8200",
//		Path:   "skupper",
//		Secret: "vault-secret",
//		Zones: []van.Zone{
//			{
//				Name: "east",
//			},
//		},
//	}
//	// the info below should come from the Secret defined through van.Config.Secret (vault-secret)
//	authPath := "approle"
//	roleId := "d859b2db-e31f-39e4-29d9-a07e7325ea94"
//	secretId := "6abfb20f-0007-338e-76aa-406cddd7eb9b"
//	c, err := client.NewAppRoleClient(v, authPath, roleId, secretId)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// authenticate
//	_, err = c.Login(context.Background())
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// available links
//	links, err := c.GetAvailableTokens()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	fmt.Println("Links available:", len(links))
//	for _, link := range links {
//		linkYaml, _ := link.Marshal()
//		fmt.Println(string(linkYaml))
//	}
//}
//
//func fakeToken() van.Token {
//	return van.Token{
//		Link: &v2alpha1.Link{
//			TypeMeta: v1.TypeMeta{
//				Kind:       "Link",
//				APIVersion: "skupper.io/v2alpha1",
//			},
//			ObjectMeta: v1.ObjectMeta{
//				Name: "sk1-link",
//			},
//			Spec: v2alpha1.LinkSpec{
//				Endpoints: []v2alpha1.Endpoint{
//					{
//						Name:  "inter-router",
//						Host:  "sk1.inter-router.host",
//						Port:  "55671",
//						Group: "router",
//					},
//					{
//						Name:  "edge",
//						Host:  "sk1.edge.host",
//						Port:  "45671",
//						Group: "router",
//					},
//				},
//				TlsCredentials: "sk1-link",
//				Cost:           1,
//			},
//		},
//		Secret: &corev1.Secret{
//			TypeMeta: v1.TypeMeta{
//				Kind:       "Secret",
//				APIVersion: "v1",
//			},
//			ObjectMeta: v1.ObjectMeta{
//				Name: "sk1-link",
//			},
//			Data: map[string][]byte{
//				"ca.crt":  []byte("ca.crt-data"),
//				"tls.crt": []byte("tls.crt-data"),
//				"tls.key": []byte("tls.key-data"),
//			},
//			Type: "kubernetes.io/tls",
//		},
//	}
//}
