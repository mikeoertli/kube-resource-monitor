package model

import "k8s.io/apimachinery/pkg/util/version"

// HelmMetadata describes a release association inferred from Kubernetes metadata.
// ChartVersion is the chart package version, not app.kubernetes.io/version.
type HelmMetadata struct {
	Managed      bool   `json:"managed"`
	Unknown      bool   `json:"unknown,omitempty"`
	Release      string `json:"release,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Chart        string `json:"chart,omitempty"`
	ChartVersion string `json:"chartVersion,omitempty"`
}

func IsHelmSortKey(key SortKey) bool {
	return key == SortHelmChart || key == SortHelmNamespace || key == SortHelmVersion
}

func helmSortValue(row *Row, key SortKey) string {
	if row.Helm == nil || !row.Helm.Managed {
		return ""
	}
	switch key {
	case SortHelmChart:
		return row.Helm.Chart
	case SortHelmNamespace:
		return row.Helm.Namespace
	case SortHelmVersion:
		return row.Helm.ChartVersion
	}
	return ""
}

func compareHelmVersions(a, b string) int {
	av, ae := version.ParseSemantic(a)
	bv, be := version.ParseSemantic(b)
	if ae == nil && be == nil {
		comparison, _ := av.Compare(bv.String())
		return comparison
	}
	if ae == nil && be != nil {
		return -1
	}
	if ae != nil && be == nil {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
