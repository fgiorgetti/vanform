package kube

import (
	skupperclient "github.com/skupperproject/skupper/pkg/generated/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Clients interface {
	GetKubeClient() kubernetes.Interface
	GetDynamicClient() dynamic.Interface
	GetDiscoveryClient() discovery.DiscoveryInterface
	GetSkupperClient() skupperclient.Interface
}

type Client struct {
	Namespace string
	Kube      kubernetes.Interface
	Rest      *rest.Config
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Skupper   skupperclient.Interface
}

func (c *Client) GetNamespace() string {
	return c.Namespace
}

func (c *Client) GetKubeClient() kubernetes.Interface {
	return c.Kube
}

func (c *Client) GetDynamicClient() dynamic.Interface {
	return c.Dynamic
}

func (c *Client) GetDiscoveryClient() discovery.DiscoveryInterface {
	return c.Discovery
}

func (c *Client) GetSkupperClient() skupperclient.Interface {
	return c.Skupper
}

func NewClient(namespace string, context string, kubeConfigPath string) (*Client, error) {
	c := &Client{}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeConfigPath != "" {
		loadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath}
	}
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		},
	)
	restconfig, err := kubeconfig.ClientConfig()
	if err != nil {
		return c, err
	}
	restconfig.ContentConfig.GroupVersion = &schema.GroupVersion{Version: "v1"}
	restconfig.APIPath = "/api"
	restconfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	c.Rest = restconfig
	c.Kube, err = kubernetes.NewForConfig(restconfig)
	if err != nil {
		return c, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restconfig)
	if err != nil {
		return c, err
	}

	c.Discovery = dc

	if namespace == "" {
		c.Namespace, _, err = kubeconfig.Namespace()
		if err != nil {
			return c, err
		}
	} else {
		c.Namespace = namespace
	}
	c.Dynamic, err = dynamic.NewForConfig(restconfig)
	if err != nil {
		return c, err
	}

	c.Skupper, err = skupperclient.NewForConfig(restconfig)
	if err != nil {
		return c, err
	}

	return c, nil
}
