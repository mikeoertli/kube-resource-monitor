package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func writeConfigTemplate(path string) error {
	data, err := configTemplate()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func runEditor(command string, paths []string, cmd *cobra.Command) error {
	args, err := editorArgs(command)
	if err != nil {
		return err
	}
	editor := exec.CommandContext(cmd.Context(), args[0], append(args[1:], paths...)...)
	editor.Stdin = os.Stdin
	editor.Stdout = cmd.OutOrStdout()
	editor.Stderr = cmd.ErrOrStderr()
	if err := editor.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	return nil
}

func (f *globalFlags) configAction(cmd *cobra.Command) error {
	if f.printConfig {
		data, err := configTemplate()
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}
	path, err := filepath.Abs(f.configPath)
	if err != nil {
		return err
	}
	if f.initConfig {
		if err := writeConfigTemplate(path); err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), "Created "+path)
			return nil
		} else if !os.IsExist(err) {
			return err
		}
		template, err := configTemplate()
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp("", "krm-config-diff-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		source := filepath.Join(dir, "template.toml")
		if err := os.WriteFile(source, template, 0400); err != nil {
			return err
		}
		diff := strings.TrimSpace(os.Getenv("KRM_DIFF_EDITOR"))
		if diff == "" {
			if _, err := exec.LookPath("code"); err == nil {
				diff = "code --wait --diff"
			} else {
				diff = "vim -d"
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Config exists; opening diff. Template is on the left; your editable config is on the right: "+path)
		if err := runEditor(diff, []string{source, path}, cmd); err != nil {
			return err
		}
	} else {
		if err := writeConfigTemplate(path); err != nil && !os.IsExist(err) {
			return err
		}
		editor := strings.TrimSpace(os.Getenv("EDITOR"))
		if editor == "" {
			editor = "vi"
		}
		if err := runEditor(editor, []string{path}, cmd); err != nil {
			return err
		}
	}
	// Validate after editing, without contacting Kubernetes. A broken config
	// never prevents --edit-config or --init-config from opening it for repair.
	fresh, settings := newRootWithFlags()
	settings.configPath = path
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("config file missing after editing: %w", err)
	}
	// Validate using fresh defaults rather than the current command's flags.
	if err := settings.loadConfig(fresh); err != nil {
		return fmt.Errorf("config saved at %s but invalid: %w", path, err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Config ready: "+path+" (applies on next launch)")
	return nil
}
