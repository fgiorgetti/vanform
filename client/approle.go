package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	vault "github.com/hashicorp/vault/api"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultAppRolePath = "approle"
)

func NewAppRole(secret *corev1.Secret) (*AppRole, error) {
	logger := slog.Default().With("namespace", secret.Namespace)
	path, ok := secret.Data["approle-path"]
	if !ok {
		path = []byte("approle")
	}
	roleId, ok := secret.Data["role-id"]
	if !ok {
		logger.Error("role-id not found in secret", "name", secret.Name)
		return nil, fmt.Errorf("role-id not found in secret")
	}
	secretId, ok := secret.Data["secret-id"]
	if !ok {
		logger.Error("secret-id not found in secret", "name", secret.Name)
		return nil, fmt.Errorf("secret-id not found in secret")
	}
	return &AppRole{
		RoleId:         string(roleId),
		SecretId:       string(secretId),
		AuthMethodPath: string(path),
		logger:         logger,
	}, nil
}

type AppRole struct {
	RoleId         string
	SecretId       string
	AuthMethodPath string
	logger         *slog.Logger
	mutex          sync.Mutex
	loggedIn       bool
	ctx            context.Context
	cancel         context.CancelFunc
}

func (a *AppRole) Login(ctx context.Context, client *vault.Client) (*vault.Secret, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.logger == nil {
		a.logger = slog.Default()
	}
	// Stop existing renew routine
	if a.cancel != nil {
		a.cancel()
	}
	defaultStr := func(v, dflt string) string {
		if v == "" {
			return dflt
		}
		return v
	}
	loginData := map[string]interface{}{
		"role_id":   a.RoleId,
		"secret_id": a.SecretId,
	}
	loginPath := fmt.Sprintf("auth/%s/login", defaultStr(a.AuthMethodPath, defaultAppRolePath))
	a.logger.Debug("Logging in using approle", slog.String("path", loginPath))
	secret, err := client.Logical().Write(loginPath, loginData)
	if err != nil {
		return nil, fmt.Errorf("unable to login: %v", err)
	}
	a.loggedIn = true
	token := secret.Auth.ClientToken
	client.SetToken(token)
	a.ctx, a.cancel = context.WithCancel(ctx)
	go a.renew(client, secret)
	return secret, nil
}

func (a *AppRole) renew(client *vault.Client, token *vault.Secret) {
	if !token.Auth.Renewable {
		a.logger.Warn("Token is not configured to be renewable.")
		return
	}
	watcher, err := client.NewLifetimeWatcher(&vault.LifetimeWatcherInput{
		Secret:    token,
		Increment: 3600,
	})
	if err != nil {
		a.logger.Error("unable to initialize new lifetime watcher for renewing auth token",
			slog.Any("error", err))
		return
	}
	go watcher.Start()
	defer watcher.Stop()

	for {
		select {
		// `DoneCh` will return if renewal fails, or if the remaining lease
		// duration is under a built-in threshold and either renewing is not
		// extending it or renewing is disabled. In any case, the caller
		// needs to attempt to log in again.
		case err := <-watcher.DoneCh():
			a.mutex.Lock()
			a.loggedIn = false
			a.mutex.Unlock()
			if err != nil {
				a.logger.Error("Failed to renew token", slog.Any("error", err))
				return
			}
			// This occurs once the token has reached max TTL.
			a.logger.Warn("Token can no longer be renewed.")
			return
		// Parent context is closed
		case <-a.ctx.Done():
			a.mutex.Lock()
			a.logger.Warn("Context is canceled.")
			a.loggedIn = false
			a.mutex.Unlock()
			return
		// Successfully completed renewal
		case renewal := <-watcher.RenewCh():
			a.logger.Debug("Successfully renewed", slog.Any("at", renewal.RenewedAt))
		}
	}
}
