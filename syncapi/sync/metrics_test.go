package sync

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestObserveSyncMetrics(t *testing.T) {
	syncDurationHistogram.Reset()
	syncLagSeconds.Set(0)

	observeSyncMetrics(150*time.Millisecond, 75*time.Millisecond)

	metrics := make(chan prometheus.Metric, 10)
	syncDurationHistogram.Collect(metrics)
	close(metrics)

	found := false
	for metric := range metrics {
		dtoMetric := &dto.Metric{}
		require.NoError(t, metric.Write(dtoMetric))
		if dtoMetric.GetHistogram() == nil {
			continue
		}
		found = true
		require.Equal(t, uint64(1), dtoMetric.GetHistogram().GetSampleCount(), "expected a single sync duration observation")
		require.InDelta(t, 0.150, dtoMetric.GetHistogram().GetSampleSum(), 0.1, "unexpected duration sum")
	}
	require.True(t, found, "expected histogram sample for sync duration")

	require.InDelta(t, 0.075, testutil.ToFloat64(syncLagSeconds), 0.0001, "expected lag gauge to be updated")
}
