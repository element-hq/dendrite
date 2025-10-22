package queue

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestObserveSendQueueDepth(t *testing.T) {
	sendQueueDepthValue.Store(0)
	sendQueueDepth.Set(0)

	observeSendQueueDepth(3)
	require.InDelta(t, 3, testutil.ToFloat64(sendQueueDepth), 0.0001)

	observeSendQueueDepth(-2)
	require.InDelta(t, 1, testutil.ToFloat64(sendQueueDepth), 0.0001)
}
