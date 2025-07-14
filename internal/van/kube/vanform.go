package kube

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fgiorgetti/vanform/internal/van"
	"github.com/fgiorgetti/vanform/internal/van/common"
	"github.com/skupperproject/skupper/pkg/apis/skupper/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
)

func NewVanForm(client *Client) *VanForm {
	logger := slog.Default().With(
		slog.String("namespace", client.Namespace),
	)
	return &VanForm{
		Namespace: client.Namespace,
		logger:    logger,
		client:    client,
		stopCh:    make(chan struct{}),
	}
}

type VanForm struct {
	Namespace string
	stopCh    chan struct{}
	logger    *slog.Logger
	client    *Client
	mu        sync.Mutex
}

func (f *VanForm) LoadConfig() (*van.Config, *corev1.Secret, error) {
	cmCli := f.client.GetKubeClient().CoreV1().ConfigMaps(f.Namespace)
	cm, err := cmCli.Get(context.Background(), "skupper-van-form", v1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("unable to get configmap: %w", err)
		f.logger.Error(err.Error())
		return nil, nil, err
	}
	configJson, ok := cm.Data["config.json"]
	if !ok {
		err = fmt.Errorf("unable to find config.json in skupper-van-form ConfigMap")
		f.logger.Error(err.Error())
		return nil, nil, err
	}
	var config van.Config
	err = json.Unmarshal([]byte(configJson), &config)
	if err != nil {
		err = fmt.Errorf("unable to parse config.json in skupper-van-form ConfigMap: %w", err)
		f.logger.Error(err.Error())
		return nil, nil, err
	}
	vaultSecretName := config.Secret
	if vaultSecretName == "" {
		vaultSecretName = "skupper-van-form"
	}
	secretsCli := f.client.GetKubeClient().CoreV1().Secrets(f.Namespace)
	secret, err := secretsCli.Get(context.Background(), vaultSecretName, v1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("unable to get secret: %w", err)
		f.logger.Error(err.Error())
		return nil, nil, err
	}
	return &config, secret, nil
}

func (f *VanForm) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.stopCh)
	f.stopCh = nil
}

func (f *VanForm) Start(parentCh chan struct{}) {
	go f.run(parentCh)
}

func (f *VanForm) IsRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCh != nil
}

func (f *VanForm) run(parentCh chan struct{}) {
	var err error
	var site *v2alpha1.Site
	vanForm := &common.VanForm{
		ConfigLoader: f,
		TokenHandler: NewTokenHandler(f.client),
	}
	resync := time.NewTicker(time.Minute)
	for {
		site, err = f.getSite()
		if err != nil {
			f.logger.Error("unable to get ready site", "error", err.Error())
		} else {
			err = vanForm.Process(site.Name, f.Namespace)
			if err != nil {
				f.logger.Error("error processing tokens", slog.Any("error", err))
			}
		}
		select {
		case <-resync.C:
			continue
		case <-parentCh:
			f.logger.Info("VanForm has stopped - parent requested")
			return
		case <-f.stopCh:
			f.logger.Info("VanForm has stopped")
			return
		}
	}
}

func (f *VanForm) getSite() (*v2alpha1.Site, error) {
	siteCli := f.client.GetSkupperClient().SkupperV2alpha1().Sites(f.Namespace)
	sites, err := siteCli.List(context.Background(), v1.ListOptions{})
	if err != nil {
		f.logger.Error("error listing sites", "error", err)
		return nil, fmt.Errorf("error listing sites: %v", err)
	}
	var site *v2alpha1.Site
	for _, s := range sites.Items {
		if s.IsReady() {
			site = &s
			break
		}
	}
	if site == nil {
		noSiteFoundMsg := "no ready site found"
		f.logger.Debug(noSiteFoundMsg)
		return nil, fmt.Errorf("%s", noSiteFoundMsg)
	}

	return site, nil
}
