package system

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/fgiorgetti/vanform/internal/van"
	"github.com/skupperproject/skupper/pkg/apis/skupper/v2alpha1"
	"github.com/skupperproject/skupper/pkg/nonkube/api"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

type TokenHandler struct {
	Namespace string
}

func NewTokenHandler(namespace string) *TokenHandler {
	return &TokenHandler{
		Namespace: namespace,
	}
}

func (t *TokenHandler) Load() ([]*van.Token, error) {
	links, err := LoadResources[v2alpha1.Link](t.Namespace, "Link", false)
	if err != nil {
		return nil, err
	}
	var tokens []*van.Token
	// filtering auto-van links only
	var filteredLinks []v2alpha1.Link
	for _, link := range links {
		if link.ObjectMeta.Labels == nil {
			continue
		}
		if _, found := link.ObjectMeta.Labels["skupper.io/auto-van"]; found {
			filteredLinks = append(filteredLinks, link)
		}
	}
	// retrieving secrets
	secrets, err := LoadResources[v1.Secret](t.Namespace, "Secret", false)
	if err != nil {
		return nil, err
	}
	secretsMap := map[string]*v1.Secret{}
	for _, secret := range secrets {
		secretsMap[secret.Name] = &secret
	}
	for _, link := range filteredLinks {
		secretName := link.Spec.TlsCredentials
		if secretName == "" {
			secretName = link.ObjectMeta.Name
		}
		tokens = append(tokens, &van.Token{
			SiteName:   link.ObjectMeta.Labels["skupper.io/site-name"],
			SiteZone:   link.ObjectMeta.Labels["skupper.io/site-zone"],
			TargetZone: link.ObjectMeta.Labels["skupper.io/target-zone"],
			Link:       &link,
			Secret:     secretsMap[secretName],
		})
	}
	return tokens, nil
}

func (t *TokenHandler) Save(token *van.Token) error {
	token.Prepare()
	token.Link.ObjectMeta.Namespace = t.Namespace
	token.Secret.ObjectMeta.Namespace = t.Namespace
	err := WriteResource(t.Namespace, "Link", token.Link.Name, token.Link)
	if err != nil {
		return fmt.Errorf("failed to write link: %w", err)
	}
	err = WriteResource(t.Namespace, "Secret", token.Secret.Name, token.Secret)
	if err != nil {
		return fmt.Errorf("failed to write secret: %w", err)
	}
	return nil
}

func (t *TokenHandler) Generate(config *van.Config) ([]*van.Token, error) {
	var tokens []*van.Token
	var err error

	if !config.Zones.Reachable() {
		return nil, nil
	}

	sites, err := LoadResources[v2alpha1.Site](t.Namespace, "Site", true)
	if err != nil {
		return nil, fmt.Errorf("failed to load site: %w", err)
	}
	if len(sites) != 1 {
		return nil, fmt.Errorf("only one runtime site is expected, found: %d", len(sites))
	}
	site := sites[0]

	for _, zone := range config.Zones {
		if !zone.Reachable() {
			continue
		}
		linkName := fmt.Sprintf("%s-%s", zone.Name, site.Name)
		for _, targetZone := range zone.ReachableFrom {
			token, err := t.loadTokenForHost(zone.EndpointHost)
			if err != nil {
				return nil, err
			}
			token.Link.ObjectMeta.Name = linkName
			token.Link.Spec.TlsCredentials = linkName
			token.Secret.ObjectMeta.Name = linkName
			token.SiteZone = zone.Name
			token.TargetZone = targetZone
			token.SiteName = site.Name
			tokens = append(tokens, token)
		}
	}

	return tokens, err
}

func (t *TokenHandler) Delete(token *van.Token) error {
	inputPath := api.GetInternalOutputPath(t.Namespace, api.InputSiteStatePath)
	linkPath := path.Join(inputPath, fmt.Sprintf("Link-%s.yaml", token.Link.Name))
	secretPath := path.Join(inputPath, fmt.Sprintf("Secret-%s.yaml", token.Secret.Name))
	var err error
	err = errors.Join(os.Remove(linkPath), os.Remove(secretPath))
	if err != nil {
		return fmt.Errorf("failed to delete link: %w", err)
	}
	return nil
}

func (t *TokenHandler) loadTokenForHost(host string) (*van.Token, error) {
	tokensPath := api.GetInternalOutputPath(t.Namespace, api.RuntimeTokenPath)
	files, err := os.ReadDir(tokensPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tokens directory: %w", err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		tokenFileName := path.Join(tokensPath, file.Name())
		tokenData, err := os.ReadFile(tokenFileName)
		if err != nil {
			return nil, fmt.Errorf("failed to read token file %s: %w", tokenFileName, err)
		}
		token := &van.Token{}
		err = token.Unmarshal(string(tokenData))
		if err != nil {
			return nil, fmt.Errorf("failed to load token file %s: %w", tokenFileName, err)
		}
		if host == "" || token.Link.Spec.Endpoints[0].Host == host {
			return token, nil
		}
	}
	notFoundMessage := fmt.Sprintf("No token found")
	if host != "" {
		notFoundMessage = fmt.Sprintf("No token found for host %q", host)
	}
	return nil, fmt.Errorf("%s", notFoundMessage)
}

func LoadResources[T any](namespace, kind string, runtime bool) ([]T, error) {
	var resources []T
	inputPath := api.GetInternalOutputPath(namespace, api.InputSiteStatePath)
	if runtime {
		inputPath = api.GetInternalOutputPath(namespace, api.RuntimeSiteStatePath)
	}
	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read directory %s: %w", inputPath, err)
	}
	for _, entry := range entries {
		fileName := path.Join(inputPath, entry.Name())
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), fmt.Sprintf("%s-", kind)) && strings.HasSuffix(entry.Name(), ".yaml") {
			resourceData, err := os.ReadFile(fileName)
			if err != nil {
				return nil, fmt.Errorf("unable to read file %s: %w", fileName, err)
			}
			resource := new(T)
			err = yaml.Unmarshal(resourceData, resource)
			if err != nil {
				return nil, fmt.Errorf("unable to unmarshal file %s as %s: %w", fileName, kind, err)
			}
			resources = append(resources, *resource)
		}
	}

	return resources, nil
}

func WriteResource[T any](namespace, kind, name string, resource T) error {
	inputPath := api.GetInternalOutputPath(namespace, api.InputSiteStatePath)
	fileName := path.Join(inputPath, fmt.Sprintf("%s-%s.yaml", kind, name))
	resourceData, err := yaml.Marshal(resource)
	if err != nil {
		return fmt.Errorf("unable to marshal file %s as %s: %w", fileName, kind, err)
	}
	err = os.WriteFile(fileName, resourceData, 0644)
	if err != nil {
		return fmt.Errorf("unable to write file %s as %s: %w", fileName, kind, err)
	}
	return nil
}
