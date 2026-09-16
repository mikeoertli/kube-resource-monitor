package render

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikeoertli/kube-resource-monitor/internal/model"
)

func TestResourceCSVAppendAndIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "samples")
	rows := []*model.Row{
		{Kind: model.KindPod, Namespace: "prod", Name: "web", Children: []*model.Row{{Kind: model.KindContainer, Namespace: "prod", Name: "app"}}},
		{Kind: model.KindPod, Namespace: "dev", Name: "web"},
		{Kind: model.KindDeployment, Namespace: "prod", Name: "web"},
		{Kind: model.KindPod, Namespace: "prod", Name: "other", Children: []*model.Row{{Kind: model.KindContainer, Namespace: "prod", Name: "app"}}},
	}
	for i := 0; i < 2; i++ {
		if err := AppendResourceCSV(dir, time.Unix(int64(i), 0), rows); err != nil {
			t.Fatal(err)
		}
	}
	// Container grouping qualifies names with the pod; it must reuse the same file.
	if err := AppendResourceCSV(dir, time.Unix(2, 0), []*model.Row{{Kind: model.KindContainer, Namespace: "prod", Name: "web/app"}}); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Fatalf("got %d files, want 6", len(files))
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if file.Name() == "prod__container__web_app.csv" {
			want = 4
		}
		if len(records) != want || records[0][0] != "timestamp" || records[1][0] == records[2][0] {
			t.Fatalf("bad history: %v", records)
		}
	}
}

func TestResourceCSVErrorsAndSafeNames(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AppendResourceCSV(blocked, time.Now(), nil); err == nil {
		t.Fatal("expected directory error")
	}
	if err := AppendResourceCSV(dir, time.Now(), []*model.Row{{Kind: model.KindPod, Name: "../../escape"}}); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 2 {
		t.Fatalf("unsafe name: %v", files)
	}
}

type failingCSVWriter struct{}

func (failingCSVWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
func TestCSVFlushError(t *testing.T) {
	if err := WriteCSV(failingCSVWriter{}, Export{}); err == nil {
		t.Fatal("lost buffered write error")
	}
}

func TestResourceCSVColumnOrderAndValues(t *testing.T) {
	dir := t.TempDir()
	taken := time.Date(2026, 9, 10, 12, 34, 56, 0, time.FixedZone("local", -6*60*60))
	row := &model.Row{
		Kind: model.KindDeployment, Namespace: "prod", Name: "web app/test",
		Node: "node-1", Ready: "1/1", Phase: "Running", Restarts: 2,
		Usage: model.Usage{
			Used:          model.Amounts{CPUMilli: 250, MemBytes: 1024, StorageBytes: 2048},
			Requests:      model.Amounts{CPUMilli: 500, MemBytes: 2048},
			Limits:        model.Amounts{CPUMilli: 1000, MemBytes: 4096},
			Capacity:      model.Amounts{StorageBytes: 8192},
			HasCPURequest: true, HasCPULimit: true, HasMemRequest: true, HasMemLimit: true, UsedKnown: true,
		},
	}
	if err := AppendResourceCSV(dir, taken, []*model.Row{row}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "prod__deployment__web_app_test.csv"))
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := "timestamp,kind,namespace,name,node,ready,phase,restarts,cpu_milli,cpu_request_milli,cpu_limit_milli,cpu_percent_of_limit,mem_bytes,mem_request_bytes,mem_limit_bytes,mem_percent_of_limit,storage_used_bytes,storage_capacity_bytes,storage_percent,metrics_missing"
	wantRow := "2026-09-10T18:34:56Z,Deployment,prod,web app/test,node-1,1/1,Running,2,250,500,1000,25.0,1024,2048,4096,25.0,2048,8192,25.0,false"
	if len(records) != 2 {
		t.Fatalf("records: %v", records)
	}
	if got := strings.Join(records[0], ","); got != wantHeader {
		t.Fatalf("header: %s", got)
	}
	if got := strings.Join(records[1], ","); got != wantRow {
		t.Fatalf("sample: %s", got)
	}
}
