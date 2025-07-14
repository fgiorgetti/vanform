package common

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/fgiorgetti/vanform/internal/client"
	"github.com/fgiorgetti/vanform/internal/van"
)

type vanFormClient struct {
	siteName  string
	namespace string
	vault     *client.Vault
	vanConfig *van.Config
	logger    *slog.Logger
}

type VanForm struct {
	ConfigLoader van.ConfigLoader
	TokenHandler van.PlatformTokenHandler
}

func (v *VanForm) Process(siteName, namespace string) error {
	config, secret, err := v.ConfigLoader.LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}
	vault, err := client.NewAppRoleClient(config, secret)
	if err != nil {
		return fmt.Errorf("error creating app role client: %w", err)
	}
	_, err = vault.Login(context.Background())
	if err != nil {
		return fmt.Errorf("vault login has failed: %w", err)
	}
	logger := slog.Default().With(
		slog.String("namespace", namespace),
		slog.String("siteName", siteName),
	)
	vfClient := &vanFormClient{
		siteName:  siteName,
		namespace: namespace,
		vault:     vault,
		vanConfig: config,
		logger:    logger,
	}
	err = v.publishTokens(vfClient)
	if err != nil {
		return fmt.Errorf("error publishing tokens: %w", err)
	}
	err = v.consumeTokens(vfClient)
	if err != nil {
		return fmt.Errorf("error consuming tokens: %w", err)
	}
	return nil
}

func (v *VanForm) publishTokens(client *vanFormClient) error {
	getToken := func(tokens []*van.Token, targetZone string) *van.Token {
		for _, token := range tokens {
			if token.TargetZone == targetZone {
				return token
			}
		}
		return nil
	}

	logger := client.logger
	if !client.vanConfig.Zones.Reachable() {
		logger.Debug("No tokens to publish as all zones are unreachable")
	}
	generatedTokens, err := v.TokenHandler.Generate(client.vanConfig)
	if err != nil {
		logger.Error("Error generating tokens", slog.Any("error", err))
		return err
	}
	// TODO retrieve all existing tokens for provided siteName
	// TODO delete old tokens created by this siteName
	var publishedTokens []*van.Token
	for _, zone := range client.vanConfig.Zones {
		for _, targetZone := range zone.ReachableFrom {
			token, err := client.vault.GetPublishedToken(client.siteName, zone.Name, targetZone)
			if err != nil {
				logger.Error("Error retrieving published token",
					slog.String("siteZone", zone.Name),
					slog.String("targetZone", targetZone),
					slog.Any("error", err))
				continue
			}
			if token == nil {
				logger.Debug("no published tokens found",
					slog.String("siteZone", zone.Name),
					slog.String("targetZone", targetZone),
				)
				continue
			}
			publishedTokens = append(publishedTokens, token)
		}
	}
	var tokensToPublish []*van.Token
	for _, token := range generatedTokens {
		pubToken := getToken(publishedTokens, token.TargetZone)
		if pubToken != nil && pubToken.Equals(token) {
			logger.Debug("token already published", "targetZone", token.TargetZone)
			continue
		}
		tokensToPublish = append(tokensToPublish, token)
	}
	for _, token := range tokensToPublish {
		logger := logger.With(
			slog.String("siteName", client.siteName),
			slog.String("siteZone", token.SiteZone),
			slog.String("targetZone", token.TargetZone),
		)
		logger.Info("publishing token")
		err = client.vault.PublishToken(*token)
		if err != nil {
			logger.Error("error publishing token",
				slog.Any("error", err))
			return fmt.Errorf("error publishing token: %w", err)
		}
	}
	return nil
}

func (v *VanForm) consumeTokens(client *vanFormClient) error {
	byName := func(tokens []*van.Token, linkName string) *van.Token {
		for _, token := range tokens {
			if token.Link.Name == linkName {
				return token
			}
		}
		return nil
	}
	logger := client.logger
	existingTokens, err := v.TokenHandler.Load()
	if err != nil {
		logger.Error("error loading existing links", slog.Any("error", err))
		return fmt.Errorf("error loading existing links: %v", err)
	}
	availableTokens, err := client.vault.GetAvailableTokens(client.siteName)
	if err != nil {
		logger.Error("error getting available tokens", slog.Any("error", err))
		return fmt.Errorf("error getting available tokens: %v", err)
	}
	var createList, deleteList []*van.Token
	for _, existingToken := range existingTokens {
		availableToken := byName(availableTokens, existingToken.Link.Name)
		if availableToken == nil {
			logger.Info("Link will be removed", slog.String("linkName", existingToken.Link.Name))
			deleteList = append(deleteList, existingToken)
			continue
		}
		if !client.vanConfig.Zones.HasZone(existingToken.TargetZone) {
			logger.Info("Existing link is no longer scoped, deleting",
				slog.String("linkName", existingToken.Link.Name),
				slog.String("targetZone", existingToken.TargetZone),
				slog.Any("validZones", client.vanConfig.Zones),
			)
			deleteList = append(deleteList, existingToken)
			continue
		}
		if !existingToken.Equals(availableToken) {
			logger.Info("Link will be updated", slog.String("linkName", existingToken.Link.Name))
			deleteList = append(deleteList, existingToken)
			createList = append(createList, availableToken)
			continue
		}
		logger.Debug("Link has no changes", slog.String("linkName", existingToken.Link.Name))
	}
	for _, availableToken := range availableTokens {
		existingToken := byName(existingTokens, availableToken.Link.Name)
		if existingToken == nil {
			logger.Info("Link will be created", slog.String("linkName", availableToken.Link.Name))
			createList = append(createList, availableToken)
			continue
		}
	}
	for _, tokenDelete := range deleteList {
		err = v.TokenHandler.Delete(tokenDelete)
		if err != nil {
			logger.Error("error deleting link",
				slog.String("linkName", tokenDelete.Link.Name),
				slog.Any("error", err),
			)
		}
	}
	for _, tokenCreate := range createList {
		err = v.TokenHandler.Save(tokenCreate)
		if err != nil {
			logger.Error("error creating link",
				slog.String("linkName", tokenCreate.Link.Name),
				slog.Any("error", err),
			)
		}
	}
	return nil
}
