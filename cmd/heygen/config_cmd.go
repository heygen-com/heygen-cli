package main

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/heygen-com/heygen-cli/internal/config"
	clierrors "github.com/heygen-com/heygen-cli/internal/errors"
	"github.com/spf13/cobra"
)

type configResponse struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source,omitempty"`
}

func newConfigCmd(ctx *cmdContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "config",
		Short:       "Manage CLI configuration",
		Annotations: map[string]string{"skipAuth": "true"},
	}
	cmd.AddCommand(newConfigSetCmd(ctx))
	cmd.AddCommand(newConfigGetCmd(ctx))
	cmd.AddCommand(newConfigListCmd(ctx))
	return cmd
}

func configProviderWithSource(ctx *cmdContext) (config.ProviderWithSource, error) {
	provider, ok := ctx.configProvider.(config.ProviderWithSource)
	if !ok {
		return nil, clierrors.New("config provider does not expose value sources")
	}
	return provider, nil
}

func writableConfigProvider(ctx *cmdContext) (config.WritableProvider, error) {
	provider, ok := ctx.configProvider.(config.WritableProvider)
	if !ok {
		return nil, clierrors.New("config provider does not support writes")
	}
	return provider, nil
}

func validateConfigKey(key string) error {
	if !slices.Contains(config.ValidKeys, key) {
		return clierrors.NewUsage(fmt.Sprintf("invalid config key %q", key))
	}
	return nil
}

func validateConfigValue(key, value string) error {
	switch key {
	case config.KeyOutput:
		if value != "json" && value != "human" {
			return clierrors.NewUsage("output must be one of: json, human")
		}
	case config.KeyAnalytics:
		if value != "true" && value != "false" {
			return clierrors.NewUsage(key + " must be one of: true, false")
		}
	}
	return nil
}

func marshalData(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, clierrors.New(fmt.Sprintf("failed to encode response: %v", err))
	}
	return data, nil
}

func newConfigSetCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a persistent config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			if err := validateConfigKey(key); err != nil {
				return err
			}
			if err := validateConfigValue(key, value); err != nil {
				return err
			}

			provider, err := writableConfigProvider(ctx)
			if err != nil {
				return err
			}
			if err := provider.Set(key, value); err != nil {
				return clierrors.New(err.Error())
			}

			data, err := marshalData(configResponse{Key: key, Value: value})
			if err != nil {
				return err
			}
			return ctx.formatter.Data(data, "", nil)
		},
	}
}

func newConfigGetCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if err := validateConfigKey(key); err != nil {
				return err
			}

			provider, err := configProviderWithSource(ctx)
			if err != nil {
				return err
			}
			source, err := provider.Resolve(key)
			if err != nil {
				return clierrors.New(err.Error())
			}

			data, err := marshalData(configResponse{
				Key:    key,
				Value:  source.Value,
				Source: source.Origin,
			})
			if err != nil {
				return err
			}
			return ctx.formatter.Data(data, "", nil)
		},
	}
}

func newConfigListCmd(ctx *cmdContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List effective config values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, err := configProviderWithSource(ctx)
			if err != nil {
				return err
			}

			items := make([]configResponse, 0, len(config.ValidKeys))
			for _, key := range config.ValidKeys {
				source, err := provider.Resolve(key)
				if err != nil {
					return clierrors.New(err.Error())
				}
				items = append(items, configResponse{
					Key:    key,
					Value:  source.Value,
					Source: source.Origin,
				})
			}

			data, err := marshalData(items)
			if err != nil {
				return err
			}
			return ctx.formatter.Data(data, "", nil)
		},
	}
}
