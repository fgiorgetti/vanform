package system

import (
	"log"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"

	"github.com/fgiorgetti/vanform/internal/filesystem"
	"github.com/skupperproject/skupper/pkg/nonkube/api"
)

const (
	lockFileName = "vanform.lock"
)

func NewController() *Controller {
	return &Controller{
		namespaces: map[string]chan struct{}{},
		logger:     slog.Default(),
	}
}

type Controller struct {
	namespaces map[string]chan struct{}
	watcher    *filesystem.FileWatcher
	logger     *slog.Logger
	mu         sync.Mutex
}

func (c *Controller) Start(stopCh chan struct{}) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureSingleInstance(stopCh)
	c.logger.Info("Starting controller")
	doneCh := make(chan struct{})
	go c.run(stopCh, doneCh)
	return doneCh
}

func (c *Controller) run(stopCh chan struct{}, doneCh chan struct{}) {
	var err error
	c.watcher, err = filesystem.NewWatcher(slog.String("component", "Controller"))
	if err != nil {
		c.logger.Error("unable to create file watcher", "error", err)
		close(doneCh)
		return
	}
	c.watcher.Add(api.GetDefaultOutputNamespacesPath(), c)
	c.watcher.Start(stopCh)
	c.handleShutdown(stopCh, doneCh)
}

func (c *Controller) ensureSingleInstance(stopCh chan struct{}) {
	internalLockFile := path.Join(api.GetDefaultOutputNamespacesPath(), lockFileName)
	lock, err := os.OpenFile(internalLockFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatalf("Unable to create lock file: %v", err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Fatalf("VAN Form controller is already running, exiting")
	}
	go func() {
		<-stopCh
		if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
			log.Fatalf("Error releasing lock file: %v", err)
		}
	}()
}

func (c *Controller) handleShutdown(stopCh chan struct{}, doneCh chan struct{}) {
	<-stopCh
	for _, ch := range c.namespaces {
		close(ch)
	}
	c.logger.Info("all VanForm instances stopped")
	close(doneCh)
}

func (c *Controller) namespace(path string) string {
	paths := strings.Split(path, "/")
	return paths[len(paths)-1]
}

func (c *Controller) OnBasePathAdded(basePath string) {
}

func (c *Controller) OnCreate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ns := c.namespace(path)
	cmHandler := &ConfigMapHandler{
		Namespace: ns,
	}
	stopCh := make(chan struct{})
	c.namespaces[ns] = stopCh
	cmHandler.Start(stopCh)
	c.logger.Info("Watching for skupper-van-form ConfigMap", "namespace", ns)
}

func (c *Controller) OnUpdate(path string) {
}

func (c *Controller) OnRemove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ns := c.namespace(path)
	stopCh, ok := c.namespaces[ns]
	if ok {
		delete(c.namespaces, ns)
		close(stopCh)
		c.logger.Info("Stopped watching for skupper-van-form ConfigMap", "namespace", ns)
	}
}

func (c *Controller) Filter(path string) bool {
	ns := c.namespace(path)
	_, ok := c.namespaces[ns]
	stat, _ := os.Stat(path)
	return ok || stat != nil && stat.IsDir()
}

type ConfigMapHandler struct {
	Namespace     string
	vanFormStopCh chan struct{}
	mu            sync.Mutex
}

func (c *ConfigMapHandler) Start(stopCh chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	logger := slog.Default().With("namespace", c.Namespace)
	watcher, err := filesystem.NewWatcher(
		slog.String("namespace", c.Namespace),
		slog.String("component", "ConfigMapHandler"),
	)
	if err != nil {
		logger.Error("unable to create file watcher", "error", err)
		return
	}
	logger.Info("Start watching for skupper-van-form ConfigMap")
	runtimeStatePath := api.GetInternalOutputPath(c.Namespace, api.RuntimeSiteStatePath)
	watcher.Add(runtimeStatePath, c)
	watcher.Start(stopCh)
}

func (c *ConfigMapHandler) OnBasePathAdded(basePath string) {
}

func (c *ConfigMapHandler) OnCreate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	logger := slog.Default().With("namespace", c.Namespace)
	if c.vanFormStopCh != nil {
		logger.Warn("VanForm is already running")
		return
	}
	c.vanFormStopCh = make(chan struct{})
	vanForm := NewVanForm(c.Namespace)
	err := vanForm.Start(c.vanFormStopCh)
	if err != nil {
		logger.Error("unable to start VanForm", "error", err)
		return
	}
}

func (c *ConfigMapHandler) OnUpdate(path string) {
}

func (c *ConfigMapHandler) OnRemove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.vanFormStopCh == nil {
		return
	}
	close(c.vanFormStopCh)
	c.vanFormStopCh = nil
}

func (c *ConfigMapHandler) Filter(path string) bool {
	const fileName = "/ConfigMap-skupper-van-form.yaml"
	return strings.HasSuffix(path, fileName)
}
