package inventory

import (
	"context"
	"strings"
	"testing"

	"github.com/mikeoertli/kube-resource-monitor/internal/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
)

func helmLabels(release, chart string) map[string]string {
	return map[string]string{"app.kubernetes.io/managed-by": "Helm", "app.kubernetes.io/instance": release, "helm.sh/chart": chart}
}

func TestSplitHelmChart(t *testing.T) {
	for _, tc := range []struct{ label, chart, version string }{
		{"my-chart-1.2.3", "my-chart", "1.2.3"},
		{"my-chart-2-1.2.3-beta.1_build.42", "my-chart-2", "1.2.3-beta.1+build.42"},
		{"postgresql-16.2.1", "postgresql", "16.2.1"},
		{"unknown-chart", "unknown-chart", ""}, {"", "", ""},
	} {
		chart, version := splitHelmChart(tc.label)
		if chart != tc.chart || version != tc.version {
			t.Fatalf("%q: got %q %q", tc.label, chart, version)
		}
	}
}

func TestHelmOwnerMetadataEnrichesWorkloadsPodsAndContainers(t *testing.T) {
	kube, provider := testCluster()
	dep, _ := kube.AppsV1().Deployments("prod").Get(context.Background(), "web", metav1.GetOptions{})
	dep.Labels = helmLabels("label-release", "web-chart-1.10.0")
	dep.Annotations = map[string]string{"meta.helm.sh/release-name": "actual-release", "meta.helm.sh/release-namespace": "releases"}
	if _, err := kube.AppsV1().Deployments("prod").Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, group := range []GroupBy{GroupWorkload, GroupPod, GroupContainer, GroupNode, GroupNamespace} {
		snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: group, IncludeHelm: true, IncludeContainers: true, IncludeMissing: true})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		var walk func([]*model.Row)
		walk = func(rows []*model.Row) {
			for _, row := range rows {
				if row.Kind == model.KindDeployment || row.Kind == model.KindPod && strings.HasPrefix(row.Name, "web-") || row.Kind == model.KindContainer && (strings.HasPrefix(row.Name, "web-") || row.Name == "nginx" || row.Name == "sidecar") {
					found++
					if row.Helm == nil || row.Helm.Release != "actual-release" || row.Helm.Namespace != "releases" || row.Helm.Chart != "web-chart" || row.Helm.ChartVersion != "1.10.0" {
						t.Fatalf("group=%s: missing inherited association for %s: %+v", group, row.Name, row.Helm)
					}
				}
				if (row.Kind == model.KindNode || row.Kind == model.KindNamespace) && row.Helm != nil {
					t.Fatal("mixed aggregate acquired an arbitrary release")
				}
				walk(row.Children)
			}
		}
		walk(snap.Rows)
		if found == 0 {
			t.Fatalf("group %s did not exercise association", group)
		}
	}
}

func TestHelmGroupsKeepUnmanagedAndUnknownDistinctAndPreserveTotals(t *testing.T) {
	kube, provider := testCluster()
	dep, _ := kube.AppsV1().Deployments("prod").Get(context.Background(), "web", metav1.GetOptions{})
	dep.Labels = helmLabels("suite", "web-1.2.3")
	_, _ = kube.AppsV1().Deployments("prod").Update(context.Background(), dep, metav1.UpdateOptions{})
	// The database is known to be Helm-managed, but its release name is missing.
	_, err := kube.AppsV1().StatefulSets("prod").Create(context.Background(), &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "db", Labels: helmLabels("", "database-2.0.0")}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupHelm, IncludeMissing: true, IncludeContainers: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 3 {
		t.Fatalf("groups=%d, want release/unknown/unmanaged", len(snap.Rows))
	}
	names := map[string]*model.Row{}
	for _, group := range snap.Rows {
		names[group.Name] = group
	}
	for _, name := range []string{"suite · prod", "Unknown Helm release · prod", "Not managed by Helm"} {
		if names[name] == nil {
			t.Fatalf("missing %s: %+v", name, names)
		}
	}
	release := names["suite · prod"]
	if len(release.Children) != 1 || release.Children[0].Kind != model.KindDeployment || len(release.Children[0].Children) != 2 {
		t.Fatal("release lost workload/pod hierarchy")
	}
	// Rollup may run repeatedly while sorting/rendering without multiplying usage.
	before := snap.Totals.Used
	for _, group := range snap.Rows {
		group.Rollup()
	}
	if after := model.TotalOf(snap.Rows).Used; before != after {
		t.Fatalf("totals changed: %+v -> %+v", before, after)
	}
	filtered, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupHelm, NamePattern: "^suite", IncludeMissing: true})
	if err != nil || len(filtered.Rows) != 1 || len(filtered.Rows[0].Children[0].Children) != 2 {
		t.Fatalf("release filter: %v %+v", err, filtered)
	}
}

func TestHelmReleaseGroupingSeparatesNamespacesAndMixedCharts(t *testing.T) {
	rows := []*model.Row{
		{Name: "one", Helm: &model.HelmMetadata{Managed: true, Release: "suite", Namespace: "a", Chart: "web", ChartVersion: "1.0.0"}},
		{Name: "two", Helm: &model.HelmMetadata{Managed: true, Release: "suite", Namespace: "b", Chart: "web", ChartVersion: "1.0.0"}},
		{Name: "three", Helm: &model.HelmMetadata{Managed: true, Release: "suite", Namespace: "a", Chart: "db", ChartVersion: "2.0.0"}},
	}
	groups := groupByHelm(rows)
	if len(groups) != 2 {
		t.Fatal("same-name releases in different namespaces merged")
	}
	for _, group := range groups {
		if group.Namespace == "a" && (len(group.Children) != 2 || group.Helm.Chart != "multiple" || group.Helm.ChartVersion != "multiple") {
			t.Fatal("mixed charts were reported as one chart")
		}
	}
}

func TestHelmPodLabelsFallbackAndInstanceAloneIsNotHelm(t *testing.T) {
	kube, provider := testCluster()
	pods, _ := kube.CoreV1().Pods("prod").List(context.Background(), metav1.ListOptions{})
	for i := range pods.Items {
		pod := &pods.Items[i]
		if strings.HasPrefix(pod.Name, "web-") {
			pod.Labels = helmLabels("pod-labeled", "web-1.2.3")
		} else {
			pod.Labels = map[string]string{"app.kubernetes.io/instance": "not-helm"}
		}
		_, _ = kube.CoreV1().Pods("prod").Update(context.Background(), pod, metav1.UpdateOptions{})
	}
	snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupHelm, IncludeMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, group := range snap.Rows {
		found = found || group.Name == "pod-labeled · prod"
		if strings.Contains(group.Name, "not-helm") {
			t.Fatal("generic app instance label treated as Helm")
		}
	}
	if !found {
		t.Fatal("pod-template-only Helm labels were not used")
	}
}

func TestHelmMetadataOptInAndRBACDegradation(t *testing.T) {
	kube, provider := testCluster()
	collector := New(kube, provider)
	_, err := collector.Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupPod})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range kube.Actions() {
		if action.GetResource().Resource == "cronjobs" {
			t.Fatal("ordinary view fetched optional Helm metadata")
		}
	}
	kube.ClearActions()
	kube.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "", nil)
	})
	snap, err := collector.Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupHelm})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Warnings) == 0 || !strings.Contains(strings.Join(snap.Warnings, " "), "Helm metadata incomplete") {
		t.Fatal("missing RBAC warning")
	}
	found := false
	for _, group := range snap.Rows {
		found = found || group.Name == "Helm ownership unavailable"
	}
	if !found {
		t.Fatal("unreadable ownership incorrectly reported as unmanaged")
	}
	for _, action := range kube.Actions() {
		if action.GetResource().Resource == "secrets" {
			t.Fatal("Helm metadata attempted to read release secrets")
		}
	}
}

func TestHelmStorageAssociations(t *testing.T) {
	// A PVC's own release association is used; shared claims are never assigned
	// to an arbitrary mounting workload's release.
	kube, provider := testCluster()
	_, err := kube.CoreV1().PersistentVolumeClaims("prod").Create(context.Background(), &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "claim", Labels: helmLabels("database", "db-1.2.3")}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupPVC, IncludeHelm: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 1 || snap.Rows[0].Helm == nil || snap.Rows[0].Helm.Release != "database" {
		t.Fatal("PVC metadata association missing")
	}
}

func TestHelmControllerAndPodTemplateMetadataFillGaps(t *testing.T) {
	for _, controllerHasRelease := range []bool{false, true} {
		kube, provider := testCluster()
		dep, _ := kube.AppsV1().Deployments("prod").Get(context.Background(), "web", metav1.GetOptions{})
		dep.Labels = map[string]string{"app.kubernetes.io/managed-by": "Helm"}
		if controllerHasRelease {
			dep.Annotations = map[string]string{"meta.helm.sh/release-name": "suite"}
		}
		_, _ = kube.AppsV1().Deployments("prod").Update(context.Background(), dep, metav1.UpdateOptions{})
		pods, _ := kube.CoreV1().Pods("prod").List(context.Background(), metav1.ListOptions{})
		for i := range pods.Items {
			pod := &pods.Items[i]
			if !strings.HasPrefix(pod.Name, "web-") {
				continue
			}
			// No managed-by label here: the owning controller establishes management.
			pod.Labels = map[string]string{"app.kubernetes.io/instance": "suite", "helm.sh/chart": "web-1.2.3"}
			_, _ = kube.CoreV1().Pods("prod").Update(context.Background(), pod, metav1.UpdateOptions{})
		}
		snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupWorkload, IncludeHelm: true, IncludeContainers: true})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range snap.Rows {
			if row.Name != "web" {
				continue
			}
			found = true
			if row.Helm == nil || row.Helm.Release != "suite" || row.Helm.Chart != "web" {
				t.Fatalf("controller release=%v did not fill template metadata: %+v", controllerHasRelease, row.Helm)
			}
		}
		if !found {
			t.Fatal("workload missing")
		}
	}
}

func TestUnmanagedContainerDoesNotBecomeUnknown(t *testing.T) {
	kube, provider := testCluster()
	snap, err := New(kube, provider).Collect(context.Background(), Options{Namespace: "prod", GroupBy: GroupPod, IncludeHelm: true, IncludeContainers: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range snap.Rows {
		if row.Helm != nil {
			continue
		}
		for _, child := range row.Children {
			if child.Helm != nil {
				t.Fatalf("unmanaged pod's child became unknown: %+v", child.Helm)
			}
		}
	}
}
