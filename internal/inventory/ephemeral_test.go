package inventory

import (
	"context"
	"github.com/mikeoertli/kube-resource-monitor/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestEphemeralBudgetUsageAndEmptyDir(t *testing.T) {
	c := container("app", "", "", "", "")
	c.Resources.Requests[corev1.ResourceEphemeralStorage] = qty("1Gi")
	c.Resources.Limits[corev1.ResourceEphemeralStorage] = qty("4Gi")
	p := pod("prod", "web", "node", nil, nil, c)
	limit := qty("2Gi")
	p.Spec.Volumes = []corev1.Volume{
		{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &limit}}},
		{Name: "ram", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: &limit}}},
	}
	kube := fake.NewSimpleClientset(p)
	for _, available := range []bool{true, false} {
		var samples []metrics.VolumeSample
		if available {
			samples = []metrics.VolumeSample{{Namespace: "prod", PodName: "web", UsedBytes: 3 << 30}, {Namespace: "prod", PodName: "web", VolumeName: "cache", UsedBytes: 1 << 30}}
		}
		coll := New(kube, metrics.NewMock(nil, nil, samples))
		snap, err := coll.Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupVolume})
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Rows) != 1 {
			t.Fatalf("budget missing: %+v", snap)
		}
		r := snap.Rows[0]
		r.Rollup()
		if r.Usage.Requests.StorageBytes != 1<<30 || r.Usage.Limits.StorageBytes != 4<<30 || len(r.Children) != 1 || r.Children[0].Usage.Limits.StorageBytes != 2<<30 {
			t.Fatalf("bad declaration: %+v", r)
		}
		if r.MetricsMissing == available {
			t.Fatal("missing usage confused with zero")
		}
		if available && (r.Usage.Used.StorageBytes != 3<<30 || snap.Totals.Used.StorageBytes != 3<<30) {
			t.Fatal("child usage double counted")
		}
		if !available && len(snap.Warnings) == 0 {
			t.Fatal("missing usage needs explanation")
		}
	}
}
