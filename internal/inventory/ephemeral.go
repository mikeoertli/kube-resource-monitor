package inventory

import (
	"context"
	"fmt"
	"github.com/mikeoertli/kube-resource-monitor/internal/metrics"
	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

// collectEphemeral joins pod storage budgets with the kubelet's disk accounting.
// Pod totals include writable layers, logs and disk emptyDirs. Children are
// detail only: they must never be summed back into the authoritative pod total.
func (c *Collector) collectEphemeral(ctx context.Context, opts Options, snap *Snapshot) (*Snapshot, error) {
	pods, err := c.kube.CoreV1().Pods(opts.Namespace).List(ctx, metav1.ListOptions{LabelSelector: opts.LabelSelector, FieldSelector: opts.FieldSelector})
	if err != nil {
		return nil, fmt.Errorf("listing pods for ephemeral storage: %w", err)
	}
	samples := map[string]metrics.VolumeSample{}
	if vp, ok := c.provider.(metrics.VolumeProvider); ok {
		values, err := vp.VolumeMetrics(ctx, opts.Namespace)
		if err != nil {
			snap.Warnings = append(snap.Warnings, "ephemeral usage unavailable: "+err.Error())
		}
		for _, v := range values {
			if v.PodName != "" {
				samples[v.Namespace+"/"+v.PodName+"/"+v.VolumeName] = v
			}
		}
	} else {
		snap.Warnings = append(snap.Warnings, "this metrics source cannot report ephemeral usage (needs nodes/proxy)")
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		row := &model.Row{Kind: model.KindEphemeral, Name: pod.Name, Namespace: pod.Namespace, Node: pod.Spec.NodeName, Labels: pod.Labels, Phase: string(pod.Status.Phase), Authoritative: true, MetricsMissing: true}
		for _, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses} {
			for _, status := range statuses {
				row.Restarts += status.RestartCount
			}
		}
		if !pod.CreationTimestamp.IsZero() {
			row.Age = time.Since(pod.CreationTimestamp.Time)
		}
		d := effectivePodResources(&pod.Spec)
		row.Usage.Requests.StorageBytes = d.Requests.StorageBytes
		row.Usage.Limits.StorageBytes = d.Limits.StorageBytes
		row.Usage.HasStorageRequest = d.HasStorageReq
		row.Usage.HasStorageLimit = d.HasStorageLimit
		key := pod.Namespace + "/" + pod.Name + "/"
		if v, ok := samples[key]; ok {
			row.Usage.Used.StorageBytes = v.UsedBytes
			row.Usage.UsedKnown = true
			row.MetricsMissing = false
		}
		for _, v := range pod.Spec.Volumes {
			if v.EmptyDir == nil || v.EmptyDir.Medium == corev1.StorageMediumMemory {
				continue
			}
			child := &model.Row{Kind: model.KindVolume, Name: pod.Name + "/" + v.Name, Namespace: pod.Namespace, Node: pod.Spec.NodeName, MetricsMissing: true}
			child.Age = row.Age
			if v.EmptyDir.SizeLimit != nil {
				child.Usage.HasStorageLimit = true
				child.Usage.Limits.StorageBytes = v.EmptyDir.SizeLimit.Value()
			}
			if s, ok := samples[key+v.Name]; ok {
				child.Usage.Used.StorageBytes = s.UsedBytes
				child.Usage.UsedKnown = true
				child.MetricsMissing = false
			}
			row.Children = append(row.Children, child)
		}
		// Include declared budgets even when usage is unavailable, without requiring
		// --include-missing. This view remains useful when summary access is denied.
		if row.Usage.HasStorageRequest || row.Usage.HasStorageLimit || len(row.Children) > 0 || !row.MetricsMissing {
			snap.Rows = append(snap.Rows, row)
		}
	}
	missing := 0
	for _, row := range snap.Rows {
		if row.MetricsMissing {
			missing++
		}
	}
	if missing > 0 {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("%d pod(s) have no ephemeral-storage usage sample; declared budgets are still shown", missing))
	}
	snap.Rows = filterByName(snap.Rows, opts.NamePattern)
	if opts.OnlyProblems {
		snap.Rows = filterProblems(snap.Rows, opts.ProblemThreshold)
	}
	snap.Totals = model.TotalOf(snap.Rows)
	return snap, nil
}
