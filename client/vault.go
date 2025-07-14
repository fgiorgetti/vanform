package client

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fgiorgetti/vanform/internal/van"
	vault "github.com/hashicorp/vault/api"
	corev1 "k8s.io/api/core/v1"
)

type Vault struct {
	client *vault.Client
	config *vault.Config
	auth   vault.AuthMethod
	van    *van.Config
	logger *slog.Logger
}

func newClient(vanConfig *van.Config) (*Vault, error) {
	config := vault.DefaultConfig()
	config.Address = vanConfig.URL
	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("error creating vault client: %v", err)
	}
	v := &Vault{
		client: client,
		config: config,
		van:    vanConfig,
		logger: slog.Default().With(slog.String("van", vanConfig.VAN)),
	}
	if v.van.Path == "" {
		v.logger.Info("Default Vault path has been set", slog.String("path", "skupper"))
		v.van.Path = "skupper"
	}
	return v, nil
}

func NewAppRoleClient(vanConfig *van.Config, vaultConfig *corev1.Secret) (*Vault, error) {
	var client *Vault
	var err error
	client, err = newClient(vanConfig)
	if err != nil {
		return nil, err
	}
	client.auth, err = NewAppRole(vaultConfig)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (v *Vault) Login(ctx context.Context) (*vault.Secret, error) {
	if v.auth == nil {
		return nil, fmt.Errorf("vault auth method not configured")
	}

	secret, err := v.auth.Login(ctx, v.client)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

func (v *Vault) GetAvailableTokens(mySiteName string) ([]*van.Token, error) {
	var tokens []*van.Token
	for _, zone := range v.van.Zones {
		availableLinksPath := v.getLogicalLinksListPath(zone.Name)
		logger := v.logger.With("zone", zone.Name).With("path", availableLinksPath)
		logger.Debug("getting available links")
		links, err := v.client.Logical().List(availableLinksPath)
		if err != nil {
			logger.Error("unable to get links list", slog.Any("error", err))
			continue
		}
		if links == nil {
			logger.Debug("no links found")
			continue
		}
		keys, ok := links.Data["keys"]
		if !ok {
			logger.Debug("no links found", slog.String("path", availableLinksPath))
			continue
		}

		for _, key := range keys.([]interface{}) {
			linkPath := v.getLinkGetPath(zone.Name, key.(string))
			logger := v.logger.With(
				slog.String("zone", zone.Name),
				slog.String("van", v.van.VAN),
				slog.String("mount", v.van.Path),
				slog.String("path", linkPath),
			)
			logger.Debug("getting link")

			secret, err := v.client.KVv2(v.van.Path).Get(context.Background(), linkPath)
			if err != nil {
				logger.Error("error getting link", slog.Any("error", err))
				return nil, fmt.Errorf("error getting link from %s at %s: %v", v.van.Path, linkPath, err)
			}
			tokenStr, ok := secret.Data["token"]
			if !ok {
				logger.Debug("token key not found - possibly deleted")
				continue
			}
			var token = new(van.Token)
			err = token.Unmarshal(tokenStr.(string))
			if err != nil {
				logger.Error("error unmarshalling token", slog.Any("error", err))
				return nil, fmt.Errorf("error unmarshalling token from %s at %s: %v", v.van.Path, availableLinksPath, err)
			}
			if token.SiteName == mySiteName {
				logger.Debug("ignoring self-token")
				continue
			}
			logger.Debug("link found", slog.Any("token", token))
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}

func (v *Vault) PublishToken(token van.Token) error {
	token.Prepare()
	tokenYaml, err := token.Marshal()
	if err != nil {
		return fmt.Errorf("error marshalling token: %v", err)
	}
	publishPath := v.getLinksPutPath(token.SiteName, token.SiteZone, token.TargetZone)
	logger := v.logger.With(
		slog.String("van", v.van.VAN),
		slog.String("mount", v.van.Path),
		slog.String("path", publishPath),
	)
	_, err = v.client.KVv2(v.van.Path).Put(context.Background(), publishPath, map[string]interface{}{
		"token": string(tokenYaml),
	})
	if err != nil {
		logger.Error("error publishing token", slog.String("site", token.SiteName), slog.String("target", token.TargetZone))
		return fmt.Errorf("error publishing token: %v", err)
	}
	logger.Info("token published", slog.String("site", token.SiteName), slog.String("target", token.TargetZone))
	return nil
}

func (v *Vault) GetPublishedToken(siteName, sourceZone, targetZone string) (*van.Token, error) {
	var err error
	linkKey := v.getLinkKey(siteName, sourceZone)
	linkPath := v.getLinkGetPath(targetZone, linkKey)
	logger := v.logger.With(
		slog.String("sourceZone", sourceZone),
		slog.String("targetZone", targetZone),
		slog.String("van", v.van.VAN),
		slog.String("mount", v.van.Path),
		slog.String("path", linkPath),
	)
	logger.Debug("getting link")
	secret, err := v.client.KVv2(v.van.Path).Get(context.Background(), linkPath)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			logger.Error("error getting link", slog.Any("error", err))
			return nil, fmt.Errorf("error getting link from %s at %s: %v", v.van.Path, linkPath, err)
		}
		logger.Debug("no published link found")
		return nil, nil
	}
	tokenStr, ok := secret.Data["token"]
	if !ok {
		logger.Error("token key not found")
		return nil, fmt.Errorf("token key not found for VAN %s at %s", v.van.VAN, linkPath)
	}
	var token = new(van.Token)
	tokenStrStr := tokenStr.(string)
	err = token.Unmarshal(tokenStrStr)
	if err != nil {
		logger.Error("error unmarshalling token", slog.Any("error", err))
		return nil, fmt.Errorf("error unmarshalling token from %s at %s: %v", v.van.Path, linkPath, err)
	}
	logger.Debug("link found", slog.Any("token", token))
	return token, nil
}

func (v *Vault) getLogicalLinksListPath(targetZone string) string {
	return fmt.Sprintf("%s/metadata/%s/%s/links", v.van.Path, v.van.VAN, targetZone)
}

func (v *Vault) getLinkGetPath(targetZone, linkKey string) string {
	return fmt.Sprintf("%s/%s/links/%s", v.van.VAN, targetZone, linkKey)
}

func (v *Vault) getLinksPutPath(siteName string, sourceZone string, targetZone string) string {
	return fmt.Sprintf("%s/%s/links/%s", v.van.VAN, targetZone, v.getLinkKey(siteName, sourceZone))
}

func (v *Vault) getLinkKey(siteName string, sourceZone string) string {
	return fmt.Sprintf("%s-%s", sourceZone, siteName)
}
