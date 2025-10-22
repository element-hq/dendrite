package routing

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestRecordThumbnailCacheResult(t *testing.T) {
	resetThumbnailCacheMetrics()

	recordThumbnailCacheResult(true)
	require.InDelta(t, 1.0, testutil.ToFloat64(thumbnailCacheHitRatio), 0.0001)

	recordThumbnailCacheResult(false)
	require.InDelta(t, 0.5, testutil.ToFloat64(thumbnailCacheHitRatio), 0.0001)
}
