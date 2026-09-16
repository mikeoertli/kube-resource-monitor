package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikeoertli/kube-resource-monitor/internal/model"
)

// AppendResourceCSV records every resource, including children, in a separate
// file. Files are closed after each sample so long watches do not exhaust FDs.
func AppendResourceCSV(dir string, taken time.Time, rows []*model.Row) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("CSV directory: %w", err)
	}
	var walk func([]*model.Row, string) error
	walk = func(rows []*model.Row, pod string) error {
		for _, row := range rows {
			name := row.Name
			if row.Kind == model.KindContainer && pod != "" {
				name = pod + "/" + name
			}
			parts := []string{row.Namespace, strings.ToLower(string(row.Kind)), name}
			for i := range parts {
				parts[i] = csvFilePart(parts[i])
			}
			path := filepath.Join(dir, strings.Join(parts, "__")+".csv")
			if err := appendResourceSample(path, taken, row); err != nil {
				return fmt.Errorf("CSV %s: %w", path, err)
			}
			parent := pod
			if row.Kind == model.KindPod || row.Kind == model.KindStandalone {
				parent = row.Name
			}
			if err := walk(row.Children, parent); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(rows, "")
}

// Kubernetes names do not contain spaces or underscores. Replace filename
// separators and other special characters with underscores for readable names.
func csvFilePart(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func appendResourceSample(path string, taken time.Time, row *model.Row) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil {
		r := *row
		r.Children = nil
		err = writeCSV(f, Export{Timestamp: taken, Rows: []ExportRow{ToExportRow(&r)}}, info.Size() == 0)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
