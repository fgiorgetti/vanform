package kube

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fgiorgetti/vanform/internal/van"
	"github.com/skupperproject/skupper/pkg/apis/skupper/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

type TokenHandler struct {
	client *Client
	logger *slog.Logger
}

func NewTokenHandler(client *Client) *TokenHandler {
	loader := &TokenHandler{
		client: client,
		logger: slog.Default(),
	}
	return loader
}

func (t *TokenHandler) Load() ([]*van.Token, error) {
	linkCli := t.client.GetSkupperClient().SkupperV2alpha1().Links(t.client.Namespace)
	linkList, err := linkCli.List(context.Background(), v1.ListOptions{
		LabelSelector: "skupper.io/auto-van=true",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list links: %w", err)
	}
	tokens := make([]*van.Token, 0, len(linkList.Items))
	secretClient := t.client.GetKubeClient().CoreV1().Secrets(t.client.Namespace)
	for _, l := range linkList.Items {
		secretName := l.Spec.TlsCredentials
		if secretName == "" {
			secretName = l.ObjectMeta.Name
		}
		secret, err := secretClient.Get(context.Background(), secretName, v1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get secret: %w", err)
		}
		tokens = append(tokens, &van.Token{
			SiteName:   l.ObjectMeta.Labels["skupper.io/site-name"],
			SiteZone:   l.ObjectMeta.Labels["skupper.io/site-zone"],
			TargetZone: l.ObjectMeta.Labels["skupper.io/target-zone"],
			Link:       &l,
			Secret:     secret,
		})
	}
	return tokens, nil
}

func (t *TokenHandler) Save(token *van.Token) error {
	secretsCli := t.client.GetKubeClient().CoreV1().Secrets(t.client.Namespace)
	linksCli := t.client.GetSkupperClient().SkupperV2alpha1().Links(t.client.Namespace)
	token.Prepare()
	token.Secret.Namespace = t.client.Namespace
	token.Link.Namespace = t.client.Namespace
	_, err := secretsCli.Create(context.Background(), token.Secret, v1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create secret: %w", err)
	}
	_, err = linksCli.Create(context.Background(), token.Link, v1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create link: %w", err)
	}
	return nil
}

func (t *TokenHandler) Generate(config *van.Config) ([]*van.Token, error) {
	if !config.Zones.Reachable() {
		return nil, nil
	}

	site := t.getReachableSite()
	if site == nil {
		return nil, nil
	}

	var tokens []*van.Token
	for _, zone := range config.Zones {
		if !zone.Reachable() {
			continue
		}
		cert, err := t.createCertificate(zone.Name, site)
		if err != nil {
			return nil, fmt.Errorf("failed to create certificate: %w", err)
		}
		secret, err := t.getSecret(cert.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get secret: %w", err)
		}

		// Create a Link per endpoint group
		var groupEndpoints = map[string][]v2alpha1.Endpoint{}
		for _, endpoint := range site.Status.Endpoints {
			groupEndpoints[endpoint.Group] = append(groupEndpoints[endpoint.Group], endpoint)
		}
		for _, targetZone := range zone.ReachableFrom {
			for group, endpoints := range groupEndpoints {
				linkName := fmt.Sprintf("%s-%s-%s", zone.Name, site.Name, group)
				link := &v2alpha1.Link{
					ObjectMeta: v1.ObjectMeta{
						Name: linkName,
						Labels: map[string]string{
							"skupper.io/site-name":   site.Name,
							"skupper.io/site-zone":   zone.Name,
							"skupper.io/target-zone": targetZone,
						},
					},
					TypeMeta: v1.TypeMeta{
						Kind:       "Link",
						APIVersion: v2alpha1.SchemeGroupVersion.String(),
					},
					Spec: v2alpha1.LinkSpec{
						Endpoints:      endpoints,
						TlsCredentials: cert.Name,
						Cost:           1,
					},
				}
				tokens = append(tokens, &van.Token{
					SiteName:   site.Name,
					SiteZone:   zone.Name,
					TargetZone: targetZone,
					Link:       link,
					Secret:     secret,
				})
			}
		}
	}
	return tokens, nil
}

func (t *TokenHandler) Delete(token *van.Token) error {
	existingTokens, err := t.Load()
	if err != nil {
		return err
	}
	linksCli := t.client.GetSkupperClient().SkupperV2alpha1().Links(t.client.Namespace)
	secretsCli := t.client.GetKubeClient().CoreV1().Secrets(t.client.Namespace)

	linkName := token.Link.Name
	secretName := token.Secret.Name
	logger := t.logger.With(slog.String("link", linkName), slog.String("secret", secretName))

	hasOthersUsingSecret := false
	foundExistingToken := false
	for _, existingToken := range existingTokens {
		if existingToken.Secret.Name == secretName && existingToken.Link.Name == linkName {
			foundExistingToken = true
			continue
		}
		if existingToken.Secret.Name == secretName {
			hasOthersUsingSecret = true
		}
	}

	if !foundExistingToken {
		logger.Warn("Token does not exist")
	}

	if hasOthersUsingSecret {
		logger.Warn("Secret will not be deleted as there are other tokens using it")
	}

	err = linksCli.Delete(context.Background(), linkName, v1.DeleteOptions{})
	if err != nil {
		logger.Error("failed to delete link", slog.Any("error", err))
		return fmt.Errorf("failed to delete link: %w", err)
	}

	if !hasOthersUsingSecret {
		err = secretsCli.Delete(context.Background(), secretName, v1.DeleteOptions{})
		if err != nil {
			logger.Error("failed to delete secret", slog.Any("error", err))
			return fmt.Errorf("failed to delete secret: %w", err)
		}
	}

	return nil
}

func (t *TokenHandler) getReachableSite() *v2alpha1.Site {
	sitesCli := t.client.GetSkupperClient().SkupperV2alpha1().Sites(t.client.Namespace)
	sites, err := sitesCli.List(context.Background(), v1.ListOptions{})
	if err != nil {
		t.logger.Warn("Failed to list sites", slog.Any("error", err))
		return nil
	}
	for _, site := range sites.Items {
		if site.IsReady() && len(site.Status.Endpoints) > 0 {
			return &site
		}
	}
	return nil
}

func (t *TokenHandler) createCertificate(zone string, site *v2alpha1.Site) (*v2alpha1.Certificate, error) {
	// TODO Refresh certs if CA has changed
	certificateName := fmt.Sprintf("%s-%s", zone, site.Name)
	certsCli := t.client.GetSkupperClient().SkupperV2alpha1().Certificates(t.client.Namespace)
	cert, err := certsCli.Get(context.Background(), certificateName, v1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		cert = &v2alpha1.Certificate{
			ObjectMeta: v1.ObjectMeta{
				Name: certificateName,
				Labels: map[string]string{
					"skupper.io/auto-van": "true",
				},
			},
			TypeMeta: v1.TypeMeta{
				Kind:       "Certificate",
				APIVersion: v2alpha1.SchemeGroupVersion.String(),
			},
			Spec: v2alpha1.CertificateSpec{
				Ca:      site.Status.DefaultIssuer,
				Subject: fmt.Sprintf("client-%s", certificateName),
				Client:  true,
			},
		}
		return certsCli.Create(context.Background(), cert, v1.CreateOptions{})
	}
	return cert, err
}

func (t *TokenHandler) getSecret(certificateName string) (*corev1.Secret, error) {
	secretsCli := t.client.GetKubeClient().CoreV1().Secrets(t.client.Namespace)
	var secret *corev1.Secret
	var err error
	backoff := wait.Backoff{
		Steps:    60,
		Duration: 1 * time.Second,
		Factor:   1.0,
		Jitter:   0.1,
	}
	err = retry.OnError(backoff, func(err error) bool {
		if err != nil && errors.IsNotFound(err) {
			return true
		}
		if secret == nil || len(secret.Data) < 3 {
			return true
		}
		return false
	}, func() error {
		secret, err = secretsCli.Get(context.Background(), certificateName, v1.GetOptions{})
		return err
	})
	return secret, err
}
