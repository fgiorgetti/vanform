package kube

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fgiorgetti/vanform/internal/van"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

type Controller struct {
	WatchNamespace string
	instances      map[string]*VanForm
	client         *Client
	logger         *slog.Logger
	config         *van.ControllerConfig
	mu             sync.Mutex
}

func NewController(config *van.ControllerConfig) (*Controller, error) {
	client, err := NewClient(config.WatchNamespace, "", config.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	c := &Controller{
		WatchNamespace: config.WatchNamespace,
		client:         client,
		instances:      make(map[string]*VanForm),
		logger:         slog.Default(),
		config:         config,
	}
	return c, nil
}

func (c *Controller) Start(stopCh chan struct{}) chan struct{} {
	c.logger.Info("Starting controller", "platform", c.config.Platform, "watch-namespace", c.WatchNamespace)
	doneCh := make(chan struct{})
	go c.run(stopCh, doneCh)
	return doneCh
}

func (c *Controller) run(stopCh chan struct{}, doneCh chan struct{}) {

	informerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(c.client.Dynamic, time.Minute, c.WatchNamespace, func(options *v1.ListOptions) {
		options.FieldSelector = "metadata.name=skupper-van-form"
	})
	resource := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	informer := informerFactory.ForResource(resource).Informer()

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			u := obj.(*unstructured.Unstructured)
			c.configmapAdded(u, stopCh)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {},
		DeleteFunc: func(obj interface{}) {
			u := obj.(*unstructured.Unstructured)
			c.configmapDeleted(u)
		},
	})
	if err != nil {
		c.logger.Error("Unable to add event handler", slog.Any("error", err))
		close(doneCh)
		return
	}
	informer.Run(stopCh)
	c.handleShutdown(stopCh, doneCh)
}

func (c *Controller) handleShutdown(stopCh chan struct{}, doneCh chan struct{}) {
	<-stopCh
	for _, vanForm := range c.instances {
		vanForm.Stop()
	}
	for {
		var runningNamespaces []string
		for namespace, vanForm := range c.instances {
			if vanForm.IsRunning() {
				runningNamespaces = append(runningNamespaces, namespace)
			}
		}
		if len(runningNamespaces) == 0 {
			c.logger.Info("all VanForm instances stopped")
			close(doneCh)
			return
		}
		c.logger.Info("VanForm instances still running", slog.Any("namespaces", runningNamespaces))
		time.Sleep(time.Second)
	}
}

func (c *Controller) configmapAdded(u *unstructured.Unstructured, stopCh chan struct{}) {
	cm := new(corev1.ConfigMap)
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, cm)
	if err != nil {
		c.logger.Error("failed to convert configmap", slog.Any("error", err))
		return
	}
	configJson, ok := cm.Data["config.json"]
	if !ok {
		c.logger.Warn("failed to find config.json in skupper-van-form configmap", slog.Any("namespace", cm.Namespace))
		return
	}
	var config van.Config
	err = json.Unmarshal([]byte(configJson), &config)
	if err != nil {
		c.logger.Warn("failed to parse config.json", slog.Any("error", err))
		return
	}
	namespace := cm.Namespace
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.instances[namespace]
	if !exists {
		vc, err := NewClient(namespace, "", c.config.Kubeconfig)
		if err != nil {
			c.logger.Error("failed to create kubernetes client for new VanForm",
				slog.String("namespace", namespace),
				slog.Any("error", err))
			return
		}
		v := NewVanForm(vc)
		c.logger.Info("launching VanForm", slog.Any("namespace", namespace))
		c.instances[namespace] = v
		v.Start(stopCh)
	}
}

func (c *Controller) configmapDeleted(u *unstructured.Unstructured) {
	cm := new(corev1.ConfigMap)
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, cm)
	if err != nil {
		c.logger.Error("failed to convert configmap", slog.Any("error", err))
		return
	}
	namespace := cm.Namespace
	c.mu.Lock()
	defer c.mu.Unlock()
	vanForm, exists := c.instances[namespace]
	if !exists {
		return
	}
	c.logger.Info("stopping VanForm", slog.Any("namespace", namespace))
	vanForm.Stop()
	delete(c.instances, namespace)
}
