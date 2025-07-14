package system

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fgiorgetti/vanform/internal/van"
	"github.com/fgiorgetti/vanform/internal/van/common"
	"github.com/skupperproject/skupper/pkg/apis/skupper/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
)

func NewVanForm(namespace string) *VanForm {
	return &VanForm{
		namespace: namespace,
		logger:    slog.Default().With("namespace", namespace),
	}
}

type VanForm struct {
	namespace string
	logger    *slog.Logger
	mu        sync.Mutex
}

func (f *VanForm) LoadConfig() (*van.Config, *corev1.Secret, error) {
	configMaps, err := LoadResources[*corev1.ConfigMap](f.namespace, "ConfigMap", true)
	if err != nil {
		return nil, nil, fmt.Errorf("error loading configmaps: %v", err)
	}
	var vanFormConfigMap *corev1.ConfigMap
	for _, configMap := range configMaps {
		if configMap.Name == "skupper-van-form" {
			vanFormConfigMap = configMap
			break
		}
	}
	if vanFormConfigMap == nil {
		return nil, nil, fmt.Errorf("could not find skupper-van-form configmap")
	}
	configJson, ok := vanFormConfigMap.Data["config.json"]
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
	secrets, err := LoadResources[*corev1.Secret](f.namespace, "Secret", true)
	if err != nil {
		return nil, nil, fmt.Errorf("error loading secrets: %v", err)
	}
	var vaultSecret *corev1.Secret
	for _, secret := range secrets {
		if secret.Name == vaultSecretName {
			vaultSecret = secret
			break
		}
	}
	if vaultSecret == nil {
		return nil, nil, fmt.Errorf("could not find vault secret: %s", vaultSecretName)
	}
	return &config, vaultSecret, nil
}

func (f *VanForm) Start(stopCh chan struct{}) error {
	go f.run(stopCh)
	return nil
}

func (f *VanForm) run(stopCh chan struct{}) {
	f.logger.Info("VanForm has started")
	resync := time.NewTicker(time.Minute)
	tokenLoader := NewTokenHandler(f.namespace)
	vanForm := &common.VanForm{
		ConfigLoader: f,
		TokenHandler: tokenLoader,
	}
	for {
		site, err := f.getSite()
		if err != nil {
			f.logger.Error("error loading site", "error", err.Error())
		} else {
			err = vanForm.Process(site.Name, f.namespace)
			if err != nil {
				f.logger.Error("error processing tokens", "error", err.Error())
			}
		}
		select {
		case <-resync.C:
			continue
		case <-stopCh:
			f.logger.Info("VanForm has stopped")
			return
		}
	}
}

func (f *VanForm) getSite() (*v2alpha1.Site, error) {
	sites, err := LoadResources[*v2alpha1.Site](f.namespace, "Site", true)
	if err != nil {
		f.logger.Error("error loading site", "error", err.Error())
		return nil, err
	}
	if len(sites) != 1 {
		f.logger.Error("unexpected number of sites", "found", len(sites))
		return nil, fmt.Errorf("unexpected number of sites: %d", len(sites))
	}
	return sites[0], nil
}
