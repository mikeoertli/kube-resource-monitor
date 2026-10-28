package model

import "testing"

func TestHelmSortingMissingLastAndVersionsSemantic(t *testing.T) {
	for _, key := range []SortKey{SortHelmChart, SortHelmNamespace, SortHelmVersion} {
		for _, descending := range []bool{false, true} {
			older := &Row{Name: "older", Helm: &HelmMetadata{Managed: true, Chart: "a", Namespace: "a", ChartVersion: "1.9.0"}}
			newer := &Row{Name: "newer", Helm: &HelmMetadata{Managed: true, Chart: "b", Namespace: "b", ChartVersion: "1.10.0"}}
			missing := &Row{Name: "missing"}
			rows := []*Row{missing, newer, older}
			parent := &Row{Children: rows}
			Sort([]*Row{parent}, key, descending)
			want := older
			if descending {
				want = newer
			}
			if rows[0] != want || rows[2] != missing {
				t.Fatalf("%s descending=%v: incorrect ordering", key, descending)
			}
		}
	}
}
