package metrics

import (
	"encoding/json"
	"testing"
)

func TestVolumeSummarySeparatesPodTotalsAndVolumes(t *testing.T) {
	var summary summaryResponse
	err := json.Unmarshal([]byte(`{"pods":[{"podRef":{"namespace":"prod","name":"web"},"ephemeral-storage":{"usedBytes":0},"volume":[{"name":"cache","usedBytes":123},{"name":"disk","usedBytes":456,"capacityBytes":1000,"pvcRef":{"namespace":"prod","name":"data"}},{"name":"missing"}]},{"podRef":{"namespace":"dev","name":"other"},"ephemeral-storage":{"usedBytes":999}}]}`), &summary)
	if err != nil {
		t.Fatal(err)
	}
	got := volumeSamples(summary, "prod")
	if len(got) != 3 || got[0].PodName != "web" || got[0].VolumeName != "" || got[0].UsedBytes != 0 || got[1].VolumeName != "cache" || got[2].ClaimName != "data" {
		t.Fatalf("bad summary join: %+v", got)
	}
	if len(volumeSamples(summary, "")) != 4 {
		t.Fatal("all namespace collection dropped sample")
	}
}
