package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikeoertli/kube-resource-monitor/internal/inventory"
	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	"github.com/mikeoertli/kube-resource-monitor/internal/notify"
	"github.com/mikeoertli/kube-resource-monitor/internal/render"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

func configPath() string {
	if path := os.Getenv("KRM_CONFIG"); path != "" {
		return path
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "krm", "krm_config.yaml")
}

// These are one-time actions rather than saved runtime preferences. Authentication
// remains entirely in kubeconfig; printing this template never reads credentials.
func configPreference(name string) bool {
	switch name {
	case "config", "edit-config", "init-config", "print-config", "version", "help", "demo":
		return false
	}
	return true
}

func notifyFlags(cmd *cobra.Command) *pflag.FlagSet {
	for _, child := range cmd.Root().Commands() {
		if child.Name() == "notify" {
			return child.Flags()
		}
	}
	return nil
}
func notifyPreference(name string) bool {
	switch name {
	case "on", "repeat", "for", "hysteresis", "no-resolved", "stdout", "also-stdout":
		return true
	}
	return false
}
func preferenceFlag(cmd *cobra.Command, name string) *pflag.Flag {
	if strings.HasPrefix(name, "notify.") {
		child := strings.TrimPrefix(name, "notify.")
		if !notifyPreference(child) {
			return nil
		}
		if flags := notifyFlags(cmd); flags != nil {
			return flags.Lookup(child)
		}
		return nil
	}
	if !configPreference(name) {
		return nil
	}
	return cmd.Root().PersistentFlags().Lookup(name)
}
func configTemplate() ([]byte, error) {
	cmd := newRootCommand()
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: "krm settings. Explicit CLI flags override these defaults.\nAuthentication stays in kubeconfig; never put credentials here."}
	appendFlags := func(mapping *yaml.Node, flags *pflag.FlagSet, allowed func(string) bool) error {
		var firstErr error
		flags.VisitAll(func(flag *pflag.Flag) {
			if !allowed(flag.Name) || firstErr != nil {
				return
			}
			key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.ReplaceAll(flag.Name, "-", "_"), HeadComment: flag.Usage + " (--" + flag.Name + ")"}
			value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: flag.DefValue}
			if slice, ok := flag.Value.(pflag.SliceValue); ok {
				values := slice.GetSlice()
				if values == nil {
					values = []string{}
				}
				firstErr = value.Encode(values)
			} else {
				switch flag.Value.Type() {
				case "bool":
					value.Tag = "!!bool"
				case "float64":
					value.Tag = "!!float"
				}
				if flag.Name == "sort-by" {
					value.Value = ""
					key.HeadComment = "Initial sort; empty uses the first entry of the active sort order (--sort-by)"
				}
			}
			mapping.Content = append(mapping.Content, key, value)
		})
		return firstErr
	}
	if err := appendFlags(root, cmd.PersistentFlags(), configPreference); err != nil {
		return nil, err
	}
	alerts := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if err := appendFlags(alerts, notifyFlags(cmd), notifyPreference); err != nil {
		return nil, err
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "notify", HeadComment: "Defaults used only when you run krm notify."}, alerts)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return out.Bytes(), enc.Close()
}

func setPreference(flag *pflag.Flag, value *yaml.Node) error {
	if slice, ok := flag.Value.(pflag.SliceValue); ok {
		if value.Kind != yaml.SequenceNode {
			return fmt.Errorf("must be a list")
		}
		values := []string{}
		for _, item := range value.Content {
			if item.Tag != "!!str" {
				return fmt.Errorf("list entries must be strings")
			}
			values = append(values, item.Value)
		}
		return slice.Replace(values)
	}
	switch flag.Value.Type() {
	case "bool":
		if value.Tag != "!!bool" {
			return fmt.Errorf("must be true or false")
		}
	case "float64":
		if value.Tag != "!!float" && value.Tag != "!!int" {
			return fmt.Errorf("must be a number")
		}
	default:
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return fmt.Errorf("must be a string")
		}
	}
	return flag.Value.Set(value.Value)
}

func (f *globalFlags) loadConfig(cmd *cobra.Command) error {
	data, err := os.ReadFile(f.configPath)
	if os.IsNotExist(err) {
		if cmd.Flags().Changed("config") {
			return fmt.Errorf("config %s does not exist; use --init-config", f.configPath)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&doc); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("config must be a YAML mapping of settings")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("config must contain exactly one YAML document")
	}
	seen := map[string]bool{}
	f.configured = map[string]bool{}
	fresh := newRootCommand()
	apply := func(name string, value *yaml.Node) error {
		flag := preferenceFlag(cmd, name)
		if flag == nil {
			return fmt.Errorf("unknown config setting %q", name)
		}
		if seen[name] {
			return fmt.Errorf("duplicate config setting %q", name)
		}
		seen[name] = true
		if err := setPreference(preferenceFlag(fresh, name), value); err != nil {
			return fmt.Errorf("invalid config setting %s: %w", name, err)
		}
		if !flag.Changed {
			if err := setPreference(flag, value); err != nil {
				return err
			}
			f.configured[name] = true
		}
		return nil
	}
	mapping := doc.Content[0]
	seenNotify := false
	for i := 0; i < len(mapping.Content); i += 2 {
		name := strings.ReplaceAll(mapping.Content[i].Value, "_", "-")
		value := mapping.Content[i+1]
		if name == "notify" {
			if seenNotify {
				return fmt.Errorf("duplicate notify section")
			}
			seenNotify = true
			if value.Kind != yaml.MappingNode {
				return fmt.Errorf("notify must be a mapping")
			}
			for j := 0; j < len(value.Content); j += 2 {
				key := "notify." + strings.ReplaceAll(value.Content[j].Value, "_", "-")
				if err := apply(key, value.Content[j+1]); err != nil {
					return err
				}
			}
		} else if err := apply(name, value); err != nil {
			return err
		}
	}
	if err := validateNotifyPreferences(cmd.Root()); err != nil {
		return err
	}
	return f.validatePreferences()
}

func validateNotifyPreferences(cmd *cobra.Command) error {
	flags := notifyFlags(cmd)
	if flags == nil {
		return nil
	}
	rules, _ := flags.GetStringArray("on")
	if _, err := notify.ParseRules(rules); err != nil {
		return fmt.Errorf("notify.on: %w", err)
	}
	repeat, _ := flags.GetDuration("repeat")
	hold, _ := flags.GetDuration("for")
	hysteresis, _ := flags.GetFloat64("hysteresis")
	if repeat < 0 || hold < 0 {
		return fmt.Errorf("notify repeat and for must be nonnegative")
	}
	if hysteresis < 0 || hysteresis >= 1 {
		return fmt.Errorf("notify hysteresis must be between 0 (inclusive) and 1 (exclusive)")
	}
	return nil
}

func parseSortOrder(values []string, storage bool) ([]model.SortKey, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("sort order must not be empty")
	}
	result := []model.SortKey{}
	seen := map[model.SortKey]bool{}
	for _, value := range values {
		key, err := parseSortKey(value)
		if err != nil {
			return nil, err
		}
		if key == model.SortStorage {
			key = model.SortStorageUsed
		}
		if storage && !model.IsStorageSortKey(key) {
			return nil, fmt.Errorf("storage sort order cannot include %s", key)
		}
		if !storage && (key == model.SortStoragePercent || key == model.SortStorageRequest || key == model.SortStorageUsed) {
			return nil, fmt.Errorf("workload sort order cannot include %s", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate sort key %s", key)
		}
		seen[key] = true
		result = append(result, key)
	}
	return result, nil
}

func (f *globalFlags) validatePreferences() error {
	if _, err := inventory.ParseGroupBy(f.groupBy); err != nil {
		return err
	}
	if _, err := inventory.ParseStorageType(f.storageType); err != nil {
		return err
	}
	if _, err := parseSortOrder(f.sortOrder, false); err != nil {
		return fmt.Errorf("sort_order: %w", err)
	}
	if _, err := parseSortOrder(f.storageSortOrder, true); err != nil {
		return fmt.Errorf("storage_sort_order: %w", err)
	}
	if f.sortBy != "" {
		if _, err := parseSortKey(f.sortBy); err != nil {
			return err
		}
	}
	if f.interval < time.Second {
		return fmt.Errorf("--interval must be at least 1s; metrics-server scrapes every 15s by default")
	}
	if f.storageInterval < time.Second {
		return fmt.Errorf("storage_interval must be at least 1s")
	}
	if f.requestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive")
	}
	if f.threshold <= 0 || f.threshold > 10 {
		return fmt.Errorf("threshold must be greater than zero and at most 10")
	}
	if _, err := render.ParseFormat(f.output); err != nil {
		return err
	}
	if _, err := parseBarStyle(f.barStyle); err != nil {
		return err
	}
	return nil
}
