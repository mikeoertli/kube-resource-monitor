package inventory

import (
	"context"
	"fmt"
	"github.com/mikeoertli/kube-resource-monitor/internal/metrics"
	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	"strings"
)

// StorageType filters the single storage view by resource flavor.
type StorageType string

const (
	StorageAll       StorageType = "all"
	StorageEphemeral StorageType = "ephemeral"
	StoragePVC       StorageType = "pvc"
)

func ParseStorageType(value string) (StorageType, error) {
	switch t := StorageType(strings.ToLower(strings.TrimSpace(value))); t {
	case "", StorageAll:
		return StorageAll, nil
	case StorageEphemeral, StoragePVC:
		return t, nil
	default:
		return "", fmt.Errorf("unknown --type %q (want all, ephemeral, or pvc)", value)
	}
}

// storageSamples lets both inventories consume the same kubelet scrape.
// The combined view should not double its API load or mix two sample times.
type storageSamples struct {
	metrics.Provider
	samples []metrics.VolumeSample
}

func (s storageSamples) VolumeMetrics(context.Context, string) ([]metrics.VolumeSample, error) {
	return s.samples, nil
}

func (c *Collector) collectStorage(ctx context.Context, opts Options, snap *Snapshot) (*Snapshot, error) {
	kind, err := ParseStorageType(string(opts.StorageType))
	if err != nil {
		return nil, err
	}
	opts.StorageType = kind
	var values []metrics.VolumeSample
	if provider, ok := c.provider.(metrics.VolumeProvider); ok {
		var err error
		values, err = provider.VolumeMetrics(ctx, opts.Namespace)
		if err != nil {
			snap.Warnings = append(snap.Warnings, "storage usage partially or wholly unavailable: "+err.Error())
		}
	} else {
		snap.Warnings = append(snap.Warnings, "this metrics source cannot report storage usage (needs nodes/proxy)")
	}
	collector := New(c.kube, storageSamples{Provider: c.provider, samples: values})
	if opts.StorageType != StoragePVC {
		ephemeral, err := collector.collectEphemeral(ctx, opts, &Snapshot{Taken: snap.Taken, GroupBy: GroupVolume})
		if err != nil {
			return nil, err
		}
		snap.Rows = append(snap.Rows, ephemeral.Rows...)
		snap.Warnings = append(snap.Warnings, ephemeral.Warnings...)
	}
	if opts.StorageType != StorageEphemeral {
		claims, err := collector.collectPVC(ctx, opts, &Snapshot{Taken: snap.Taken, GroupBy: GroupPVC})
		if err != nil {
			return nil, fmt.Errorf("collecting storage claims: %w", err)
		}
		snap.Rows = append(snap.Rows, claims.Rows...)
		snap.Warnings = append(snap.Warnings, claims.Warnings...)
	}
	snap.Totals = model.TotalOf(snap.Rows)
	return snap, nil
}
