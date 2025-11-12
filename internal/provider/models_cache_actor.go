package provider

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/statcode-ai/statcode-ai/internal/actor"
)

const providerModelsCacheVersion uint32 = 2

// providerModelsCacheActor handles persistent storage of provider models lists.
type providerModelsCacheActor struct {
	name     string
	cacheDir string
}

func newProviderModelsCacheActor(name, cacheDir string) *providerModelsCacheActor {
	return &providerModelsCacheActor{name: name, cacheDir: cacheDir}
}

func (a *providerModelsCacheActor) ID() string { return a.name }

func (a *providerModelsCacheActor) Start(ctx context.Context) error {
	if a.cacheDir == "" {
		return fmt.Errorf("cache directory not configured")
	}
	return os.MkdirAll(a.cacheDir, 0755)
}

func (a *providerModelsCacheActor) Stop(ctx context.Context) error { return nil }

func (a *providerModelsCacheActor) Receive(ctx context.Context, msg actor.Message) error {
	switch m := msg.(type) {
	case providerModelsSaveMsg:
		err := a.saveModels(m.ProviderName, m.Models)
		m.ResponseChan <- err
		return nil
	case providerModelsLoadMsg:
		models, err := a.loadModels(m.ProviderName)
		m.ResponseChan <- providerModelsLoadResponse{Models: models, Err: err}
		return nil
	default:
		return fmt.Errorf("unknown cache actor message type: %T", msg)
	}
}

type providerModelsSaveMsg struct {
	ProviderName string
	Models       []*Model
	ResponseChan chan error
}

func (providerModelsSaveMsg) Type() string { return "providerModelsSaveMsg" }

type providerModelsLoadMsg struct {
	ProviderName string
	ResponseChan chan providerModelsLoadResponse
}

func (providerModelsLoadMsg) Type() string { return "providerModelsLoadMsg" }

type providerModelsLoadResponse struct {
	Models []*Model
	Err    error
}

type providerModelsCache struct {
	Version uint32
	Models  []cachedModel
}

// cachedModel is a storage-safe copy of Model that strips any future sensitive fields.
type cachedModel struct {
	ID              string
	Name            string
	Provider        string
	Description     string
	ContextWindow   int
	MaxOutputTokens int
}

// legacyProviderModelsCache matches the pre-v2 on-disk format so we can migrate seamlessly.
type legacyProviderModelsCache struct {
	Version uint32
	Models  []*Model
}

func (a *providerModelsCacheActor) saveModels(providerName string, models []*Model) error {
	if a.cacheDir == "" {
		return fmt.Errorf("cache directory not configured")
	}

	var buf bytes.Buffer
	wrapper := providerModelsCache{
		Version: providerModelsCacheVersion,
		Models:  sanitizeModels(models),
	}
	if err := gob.NewEncoder(&buf).Encode(&wrapper); err != nil {
		return err
	}

	path := a.modelsPath(providerName)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (a *providerModelsCacheActor) loadModels(providerName string) ([]*Model, error) {
	if a.cacheDir == "" {
		return nil, fmt.Errorf("cache directory not configured")
	}

	path := a.modelsPath(providerName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var decoded providerModelsCache
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err == nil {
		if decoded.Version != providerModelsCacheVersion {
			return nil, fmt.Errorf("unsupported provider models cache version %d", decoded.Version)
		}
		return restoreModels(decoded.Models), nil
	}

	var legacyWrapper legacyProviderModelsCache
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&legacyWrapper); err == nil {
		return legacyWrapper.Models, nil
	}

	var legacy []*Model
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&legacy); err == nil {
		return legacy, nil
	}

	return nil, fmt.Errorf("failed to decode provider models cache")
}

func (a *providerModelsCacheActor) modelsPath(providerName string) string {
	name := sanitizeProviderName(providerName)
	return filepath.Join(a.cacheDir, name+".models")
}

func providerModelsCacheDir() (string, error) {
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		return filepath.Join(cacheDir, "statcode-ai", "provider_models"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".cache", "statcode-ai", "provider_models"), nil
}

func sanitizeProviderName(name string) string {
	if name == "" {
		return "provider"
	}

	name = strings.ToLower(name)
	var builder strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteRune('_')
	}
	return builder.String()
}

func sanitizeModels(models []*Model) []cachedModel {
	if len(models) == 0 {
		return nil
	}

	sanitized := make([]cachedModel, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		sanitized = append(sanitized, cachedModel{
			ID:              model.ID,
			Name:            model.Name,
			Provider:        model.Provider,
			Description:     model.Description,
			ContextWindow:   model.ContextWindow,
			MaxOutputTokens: model.MaxOutputTokens,
		})
	}
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

func restoreModels(cached []cachedModel) []*Model {
	if len(cached) == 0 {
		return nil
	}

	models := make([]*Model, 0, len(cached))
	for _, model := range cached {
		models = append(models, &Model{
			ID:              model.ID,
			Name:            model.Name,
			Provider:        model.Provider,
			Description:     model.Description,
			ContextWindow:   model.ContextWindow,
			MaxOutputTokens: model.MaxOutputTokens,
		})
	}
	if len(models) == 0 {
		return nil
	}
	return models
}
