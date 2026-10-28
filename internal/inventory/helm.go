package inventory

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/version"
)

// Collect enriches only opt-in views. Ordinary monitoring needs no extra Helm
// metadata reads, Helm binary, release Secrets, or release storage permissions.
func (c *Collector) Collect(ctx context.Context, opts Options) (*Snapshot, error) {
	query := opts
	if opts.GroupBy == GroupHelm {
		// Filter release names after grouping, rather than dropping workloads first.
		query.NamePattern = ""
		query.OnlyProblems = false
	}
	snap, err := c.collect(ctx, query)
	if err != nil {
		return nil, err
	}
	if opts.IncludeHelm || opts.GroupBy == GroupHelm {
		snap.Warnings = append(snap.Warnings, c.annotateHelm(ctx, opts.Namespace, snap.Rows)...)
	}
	if opts.GroupBy == GroupHelm {
		snap.Rows = groupByHelm(snap.Rows)
		snap.Rows = filterByName(snap.Rows, opts.NamePattern)
		if opts.OnlyProblems {
			snap.Rows = filterProblems(snap.Rows, opts.ProblemThreshold)
		}
		snap.Totals = model.TotalOf(snap.Rows)
	}
	return snap, nil
}

func helmObjectKey(kind, ns, name string) string { return kind + "/" + ns + "/" + name }

func helmFromMetadata(object metav1.Object) model.HelmMetadata {
	labels, annotations := object.GetLabels(), object.GetAnnotations()
	release := annotations["meta.helm.sh/release-name"]
	managed := strings.EqualFold(labels["app.kubernetes.io/managed-by"], "Helm") || release != ""
	if release == "" {
		release = labels["app.kubernetes.io/instance"]
	}
	ns := annotations["meta.helm.sh/release-namespace"]
	if ns == "" {
		ns = object.GetNamespace()
	}
	chart, chartVersion := splitHelmChart(labels["helm.sh/chart"])
	return model.HelmMetadata{Managed: managed, Release: release, Namespace: ns, Chart: chart, ChartVersion: chartVersion}
}

func splitHelmChart(label string) (string, string) {
	// Scan possible boundaries: chart names and prerelease versions both contain
	// hyphens. Helm replaces '+' with '_' to make build metadata label-safe.
	for i := 0; i < len(label); i++ {
		if label[i] != '-' || i == 0 {
			continue
		}
		suffix := strings.ReplaceAll(label[i+1:], "_", "+")
		if _, err := version.ParseSemantic(suffix); err == nil {
			return label[:i], suffix
		}
	}
	return label, ""
}

func (c *Collector) annotateHelm(ctx context.Context, namespace string, rows []*model.Row) []string {
	objects := map[string]metav1.Object{}
	warnings := []string{}
	needsPVC := false
	var inspect func([]*model.Row)
	inspect = func(rows []*model.Row) {
		for _, row := range rows {
			needsPVC = needsPVC || row.Kind == model.KindPVC
			inspect(row.Children)
		}
	}
	inspect(rows)
	lists := []struct {
		kind string
		load func() (runtime.Object, error)
	}{
		{"Pod", func() (runtime.Object, error) { return c.kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{}) }},
		{"Deployment", func() (runtime.Object, error) {
			return c.kube.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		}},
		{"ReplicaSet", func() (runtime.Object, error) {
			return c.kube.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
		}},
		{"StatefulSet", func() (runtime.Object, error) {
			return c.kube.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
		}},
		{"DaemonSet", func() (runtime.Object, error) {
			return c.kube.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
		}},
		{"Job", func() (runtime.Object, error) {
			return c.kube.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		}},
		{"CronJob", func() (runtime.Object, error) {
			return c.kube.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
		}},
	}
	if needsPVC {
		lists = append(lists, struct {
			kind string
			load func() (runtime.Object, error)
		}{"PersistentVolumeClaim", func() (runtime.Object, error) {
			return c.kube.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
		}})
	}
	for _, list := range lists {
		result, err := list.load()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Helm metadata incomplete: cannot list %s: %v", list.kind, err))
			continue
		}
		entries, err := meta.ExtractList(result)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			metadata, err := meta.Accessor(entry)
			if err == nil {
				objects[helmObjectKey(list.kind, metadata.GetNamespace(), metadata.GetName())] = metadata
			}
		}
	}
	cache := map[string]model.HelmMetadata{}
	var resolve func(string, map[string]bool) model.HelmMetadata
	resolve = func(key string, visiting map[string]bool) model.HelmMetadata {
		if value, ok := cache[key]; ok {
			return value
		}
		if visiting[key] {
			return model.HelmMetadata{Unknown: true}
		}
		object, ok := objects[key]
		if !ok {
			return model.HelmMetadata{Unknown: true}
		}
		visiting[key] = true
		defer delete(visiting, key)
		value := helmFromMetadata(object)
		if owner := metav1.GetControllerOf(object); owner != nil {
			parent := resolve(helmObjectKey(owner.Kind, object.GetNamespace(), owner.Name), visiting)
			if parent.Managed {
				// Ownership annotations on the controller take precedence over pod labels.
				// Fill missing chart details only from the same release, not another one.
				if !value.Managed || value.Release == "" || (value.Release == parent.Release && value.Namespace == parent.Namespace) {
					if parent.Chart == "" {
						parent.Chart = value.Chart
						parent.ChartVersion = value.ChartVersion
					}
				}
				if parent.Release == "" && value.Release != "" {
					parent.Release = value.Release
					parent.Namespace = value.Namespace
				}
				value = parent
			} else if !value.Managed && parent.Unknown {
				value.Unknown = true
			}
		}
		if !value.Managed {
			value = model.HelmMetadata{Unknown: value.Unknown}
		}
		cache[key] = value
		return value
	}
	var attach func([]*model.Row, *model.HelmMetadata)
	attach = func(rows []*model.Row, inherited *model.HelmMetadata) {
		for _, row := range rows {
			kind, name := string(row.Kind), row.Name
			switch row.Kind {
			case model.KindNode, model.KindNamespace:
				attach(row.Children, nil)
				continue
			case model.KindStandalone, model.KindEphemeral:
				kind = "Pod"
			case model.KindContainer, model.KindVolume:
				if inherited != nil {
					if inherited.Managed || inherited.Unknown {
						row.Helm = inherited
					}
					attach(row.Children, inherited)
					continue
				}
				kind = "Pod"
				name, _, _ = strings.Cut(name, "/")
			}
			value := resolve(helmObjectKey(kind, row.Namespace, name), map[string]bool{})
			if value.Managed || value.Unknown {
				row.Helm = &value
			}
			attach(row.Children, &value)
			// Pod-template metadata can fill gaps, but only when children agree.
			if common := commonHelmMetadata(row.Children); common != nil {
				if !value.Managed {
					row.Helm = common
				} else if value.Release == "" || (value.Release == common.Release && value.Namespace == common.Namespace) {
					if value.Release == "" {
						value.Release = common.Release
						value.Namespace = common.Namespace
					}
					if value.Chart == "" {
						value.Chart = common.Chart
						value.ChartVersion = common.ChartVersion
					}
					row.Helm = &value
				}
			}
		}
	}
	attach(rows, nil)
	return warnings
}

// Return an association only if all children identify the same release.
func commonHelmMetadata(rows []*model.Row) *model.HelmMetadata {
	var common *model.HelmMetadata
	for _, row := range rows {
		h := row.Helm
		if h == nil || !h.Managed {
			return nil
		}
		if common == nil {
			copy := *h
			common = &copy
			continue
		}
		if common.Release != h.Release || common.Namespace != h.Namespace {
			return nil
		}
		if common.Chart != h.Chart {
			common.Chart = "multiple"
		}
		if common.ChartVersion != h.ChartVersion {
			common.ChartVersion = "multiple"
		}
	}
	return common
}

func groupByHelm(workloads []*model.Row) []*model.Row {
	groups := map[string]*model.Row{}
	for _, workload := range workloads {
		h := workload.Helm
		key, name, ns := "unmanaged", "Not managed by Helm", ""
		if h != nil && h.Managed {
			ns = h.Namespace
			if h.Release == "" {
				key = "unknown/" + ns
				name = "Unknown Helm release"
			} else {
				key = "release/" + ns + "/" + h.Release
				name = h.Release
			}
			name += " · " + ns
		} else if h != nil && h.Unknown {
			key, name = "unavailable", "Helm ownership unavailable"
		}
		group := groups[key]
		if group == nil {
			group = &model.Row{Kind: model.KindHelmRelease, Name: name, Namespace: ns}
			if h != nil {
				copy := *h
				group.Helm = &copy
			}
			groups[key] = group
		} else if group.Helm != nil && h != nil {
			if group.Helm.Chart != h.Chart {
				group.Helm.Chart = "multiple"
			}
			if group.Helm.ChartVersion != h.ChartVersion {
				group.Helm.ChartVersion = "multiple"
			}
		}
		group.Children = append(group.Children, workload)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*model.Row, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		group.Rollup()
		group.Ready = fmt.Sprintf("%d pods", group.PodCount())
		if group.PodCount() == 1 {
			group.Ready = "1 pod"
		}
		out = append(out, group)
	}
	return out
}
