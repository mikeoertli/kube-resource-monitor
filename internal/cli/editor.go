package cli

import (
	"fmt"
	"os/exec"
	"strings"
	"unicode"
)

// Support EDITOR='code --wait' and quoted paths without invoking a shell.
// Shell operators, substitutions and environment expansion are never evaluated.
func editorArgs(raw string) ([]string, error) {
	if _, err := exec.LookPath(raw); err == nil {
		return []string{raw}, nil
	}
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range raw {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, started = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case unicode.IsSpace(r):
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("EDITOR contains an unfinished quote or escape")
	}
	if started {
		args = append(args, word.String())
	}
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("EDITOR must name an executable")
	}
	return args, nil
}
