package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/raystone-ai/clawmini/internal/server"
)

type fileConfig struct {
	AdminToken      string `json:"adminToken"`
	AdminTokenSnake string `json:"admin_token"`
}

func loadFileConfig(path string) (fileConfig, error) {
	if strings.TrimSpace(path) == "" {
		return fileConfig{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, nil
		}
		return fileConfig{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fileConfig{}, nil
	}

	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return cfg, nil
}

func (c fileConfig) adminToken() string {
	if strings.TrimSpace(c.AdminToken) != "" {
		return strings.TrimSpace(c.AdminToken)
	}
	return strings.TrimSpace(c.AdminTokenSnake)
}

func resolveAdminToken(store *server.AdminTokenStore, envToken, flagToken, configToken string) (token string, generated bool, err error) {
	candidates := []string{
		strings.TrimSpace(envToken),
		strings.TrimSpace(flagToken),
		strings.TrimSpace(configToken),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if err := store.SaveAdminToken(candidate); err != nil {
			return "", false, err
		}
		return candidate, false, nil
	}

	token, err = store.GetAdminToken()
	if err == nil {
		return token, false, nil
	}
	if !errors.Is(err, server.ErrNotFound) {
		return "", false, err
	}

	token, err = newRandomToken(32)
	if err != nil {
		return "", false, err
	}
	if err := store.SaveAdminToken(token); err != nil {
		return "", false, err
	}
	return token, true, nil
}

func resolveJWTSecret(store *server.AdminTokenStore, envSecret string) (secret []byte, generated bool, err error) {
	trimmedEnv := strings.TrimSpace(envSecret)
	if trimmedEnv != "" {
		if err := store.SaveSetting("jwt_secret", trimmedEnv); err != nil {
			return nil, false, err
		}
		return []byte(trimmedEnv), false, nil
	}

	existing, err := store.GetSetting("jwt_secret")
	if err == nil {
		return []byte(existing), false, nil
	}
	if !errors.Is(err, server.ErrNotFound) {
		return nil, false, err
	}

	generatedSecret, err := newRandomToken(32)
	if err != nil {
		return nil, false, err
	}
	if err := store.SaveSetting("jwt_secret", generatedSecret); err != nil {
		return nil, false, err
	}
	return []byte(generatedSecret), true, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func newRandomToken(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("invalid token size")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
