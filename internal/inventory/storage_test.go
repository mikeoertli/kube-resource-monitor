package inventory

import (
	"context"
	"errors"
	"github.com/mikeoertli/kube-resource-monitor/internal/metrics"
	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"testing"
)

type countedStorage struct {
	*metrics.Mock
	calls int
	err   error
}

func (p *countedStorage) VolumeMetrics(ctx context.Context, ns string) ([]metrics.VolumeSample, error) {
	p.calls++
	values, _ := p.Mock.VolumeMetrics(ctx, ns)
	return values, p.err
}

func TestCombinedStorageScrapesOnceAndPreservesKinds(t *testing.T) {
	c := container("app", "", "", "", "")
	c.Resources.Requests[corev1.ResourceEphemeralStorage] = qty("1Gi")
	c.Resources.Limits[corev1.ResourceEphemeralStorage] = qty("4Gi")
	pod := pod("prod", "web", "node", nil, nil, c)
	limit := qty("2Gi")
	pod.Spec.Volumes = []corev1.Volume{{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &limit}}}}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "data"}, Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: qty("30Gi")}}}}
	kube := fake.NewSimpleClientset(pod, pvc)
	for _, partial := range []bool{false, true} {
		provider := &countedStorage{Mock: metrics.NewMock(nil, nil, []metrics.VolumeSample{{Namespace: "prod", PodName: "web", UsedBytes: 3 << 30}, {Namespace: "prod", PodName: "web", VolumeName: "cache", UsedBytes: 1 << 30}, {Namespace: "prod", ClaimName: "data", UsedBytes: 20 << 30, CapacityBytes: 30 << 30}, {Namespace: "prod", ClaimName: "data", UsedBytes: 20 << 30, CapacityBytes: 30 << 30}})}
		provider.Jitter = 0
		// PVC mock applies a small creep even with zero jitter; use returned values
		// only to verify consistency, without relying on a wall-clock waveform.
		if partial {
			provider.err = errors.New("one kubelet unavailable")
		}
		snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupStorage})
		if err != nil {
			t.Fatal(err)
		}
		if provider.calls != 1 || len(snap.Rows) != 2 {
			t.Fatalf("calls=%d rows=%d", provider.calls, len(snap.Rows))
		}
		if snap.Rows[0].Kind != model.KindEphemeral || snap.Rows[1].Kind != model.KindPVC || snap.Rows[0].Children[0].Kind != model.KindVolume {
			t.Fatal("volume flavors lost")
		}
		want := snap.Rows[0].Usage.Used.StorageBytes + snap.Rows[1].Usage.Used.StorageBytes
		if snap.Totals.Used.StorageBytes != want {
			t.Fatal("volume children or shared PVC duplicated in totals")
		}
		if partial && len(snap.Warnings) == 0 {
			t.Fatal("partial availability hidden")
		}
	}
}

func TestGroupingCycleHasNoDuplicates(t *testing.T) {
	seen := map[GroupBy]bool{}
	for _, group := range AllGroupBy {
		if seen[group] {
			t.Fatalf("duplicate grouping %s", group)
		}
		seen[group] = true
	}
}

func TestStorageTypesSkipUnrelatedInventories(t *testing.T) {
	for _, kind := range []StorageType{StorageEphemeral, StoragePVC} {
		pod := pod("prod", "web", "node", nil, nil, container("app", "", "", "", ""))
		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "data"}, Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: qty("30Gi")}}}}
		kube := fake.NewSimpleClientset(pod, pvc)
		if kind == StorageEphemeral {
			kube.PrependReactor("list", "persistentvolumeclaims", func(action clienttesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("PVC permission denied")
			})
		}
		provider := metrics.NewMock(nil, nil, []metrics.VolumeSample{{Namespace: "prod", PodName: "web", UsedBytes: 1 << 30}, {Namespace: "prod", ClaimName: "data", UsedBytes: 2 << 30}})
		snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupStorage, StorageType: kind})
		if err != nil || len(snap.Rows) != 1 {
			t.Fatalf("type %s: snapshot=%+v err=%v", kind, snap, err)
		}
		want := model.KindEphemeral
		if kind == StoragePVC {
			want = model.KindPVC
		}
		if snap.Rows[0].Kind != want {
			t.Fatalf("unfiltered kind %s", snap.Rows[0].Kind)
		}
	}
}
