package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/mikeoertli/kube-resource-monitor/internal/inventory"
	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	"github.com/mikeoertli/kube-resource-monitor/internal/notify"
	"github.com/mikeoertli/kube-resource-monitor/internal/render"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	return filepath.Join(root, "krm", "krm_config.toml")
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
	if name == "color" {
		return nil
	}
	if name == "force-color" {
		name = "color"
	}
	if !configPreference(name) {
		return nil
	}
	return cmd.Root().PersistentFlags().Lookup(name)
}

// The CLI keeps --color for compatibility; settings name its forcing behavior.
func configSettingName(flag string) string {
	if flag == "color" {
		return "force_color"
	}
	return strings.ReplaceAll(flag, "-", "_")
}

func configTemplate() ([]byte, error) {
	cmd := newRootCommand()
	var out bytes.Buffer
	out.WriteString("# krm settings. Explicit CLI flags override these defaults.\n# Authentication stays in kubeconfig; never put credentials here.\n")
	appendFlags := func(flags *pflag.FlagSet, allowed func(string) bool) error {
		var firstErr error
		flags.VisitAll(func(flag *pflag.Flag) {
			if !allowed(flag.Name) || firstErr != nil {
				return
			}
			usage := flag.Usage + " (--" + flag.Name + ")"
			var value interface{} = flag.DefValue
			if slice, ok := flag.Value.(pflag.SliceValue); ok {
				values := slice.GetSlice()
				if values == nil {
					values = []string{}
				}
				value = values
			} else {
				switch flag.Value.Type() {
				case "bool":
					value, firstErr = strconv.ParseBool(flag.DefValue)
				case "float64":
					value, firstErr = strconv.ParseFloat(flag.DefValue, 64)
				}
				if flag.Name == "sort-by" {
					value = ""
					usage = "Initial sort; empty uses the first entry of the active sort order (--sort-by)"
				}
			}
			if firstErr != nil {
				return
			}
			fmt.Fprintf(&out, "\n# %s\n", strings.ReplaceAll(usage, "\n", "\n# "))
			firstErr = toml.NewEncoder(&out).Encode(map[string]interface{}{configSettingName(flag.Name): value})
		})
		return firstErr
	}
	if err := appendFlags(cmd.PersistentFlags(), configPreference); err != nil {
		return nil, err
	}
	out.WriteString("\n# Defaults used only when you run krm notify.\n[notify]\n")
	if err := appendFlags(notifyFlags(cmd), notifyPreference); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func setPreference(flag *pflag.Flag, value interface{}) error {
	if slice, ok := flag.Value.(pflag.SliceValue); ok {
		items, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("must be an array")
		}
		values := []string{}
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return fmt.Errorf("array entries must be strings")
			}
			values = append(values, text)
		}
		return slice.Replace(values)
	}
	var text string
	switch flag.Value.Type() {
	case "bool":
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("must be true or false")
		}
		text = strconv.FormatBool(v)
	case "float64":
		switch v := value.(type) {
		case float64:
			text = strconv.FormatFloat(v, 'g', -1, 64)
		case int64:
			text = strconv.FormatInt(v, 10)
		default:
			return fmt.Errorf("must be a number")
		}
	default:
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("must be a string")
		}
		text = v
	}
	return flag.Value.Set(text)
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
	var mapping map[string]interface{}
	if _, err := toml.Decode(string(data), &mapping); err != nil {
		return fmt.Errorf("decode TOML config: %w", err)
	}
	seen := map[string]bool{}
	f.configured = map[string]bool{}
	fresh := newRootCommand()
	apply := func(name string, value interface{}) error {
		flag := preferenceFlag(cmd, name)
		if flag == nil {
			if name == "color" {
				return fmt.Errorf("config setting color was renamed to force_color; update your settings file")
			}
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
	for rawName, value := range mapping {
		name := strings.ReplaceAll(rawName, "_", "-")
		if name == "notify" {
			alerts, ok := value.(map[string]interface{})
			if !ok {
				return fmt.Errorf("notify must be a TOML table")
			}
			for rawKey, item := range alerts {
				key := "notify." + strings.ReplaceAll(rawKey, "_", "-")
				if err := apply(key, item); err != nil {
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
