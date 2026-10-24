// Package language_server / telemetry_types.go — BAIC telemetry client (recovered).
//
// Source: google3/third_party/jetski/language_server/language_server.
package language_server

import (
	"sync"
)

// ---------------------------------------------------------------------------
// BAIC telemetry client (recovered).
// ---------------------------------------------------------------------------

// BAICTelemetryClient is the recovered telemetry client.
type BAICTelemetryClient struct {
	mu       sync.Mutex
	endpoint string
}

// NewBAICTelemetryClient is the recovered constructor.
func NewBAICTelemetryClient(endpoint string) *BAICTelemetryClient {
	return &BAICTelemetryClient{endpoint: endpoint}
}

// ProvideFeedback is the recovered method.
func (c *BAICTelemetryClient) ProvideFeedback(feedback string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordAnalyticsEvent is the recovered method.
func (c *BAICTelemetryClient) RecordAnalyticsEvent(name string, props map[string]any) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordAnalyticsEventString is the recovered method.
func (c *BAICTelemetryClient) RecordAnalyticsEventString(name, value string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordBattleModeMetadata is the recovered method.
func (c *BAICTelemetryClient) RecordBattleModeMetadata(meta string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCommandUsage is the recovered method.
func (c *BAICTelemetryClient) RecordCommandUsage(cmd string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCompletions is the recovered method.
func (c *BAICTelemetryClient) RecordCompletions(model string, count int) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCortexExecutionMetadata is the recovered method.
func (c *BAICTelemetryClient) RecordCortexExecutionMetadata(meta string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCortexTrajectory is the recovered method.
func (c *BAICTelemetryClient) RecordCortexTrajectory(trajID string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordError is the recovered method.
func (c *BAICTelemetryClient) RecordError(err error) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordFullTrajectoryAnalytics is the recovered method.
func (c *BAICTelemetryClient) RecordFullTrajectoryAnalytics(trajID string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordHookExecution is the recovered method.
func (c *BAICTelemetryClient) RecordHookExecution(hook string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordInputCompletion is the recovered method.
func (c *BAICTelemetryClient) RecordInputCompletion(prompt string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordInputCompletionFeedback is the recovered method.
func (c *BAICTelemetryClient) RecordInputCompletionFeedback(feedback string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordMonitorSignal is the recovered method.
func (c *BAICTelemetryClient) RecordMonitorSignal(signal string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordObservabilityData is the recovered method.
func (c *BAICTelemetryClient) RecordObservabilityData(data string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordRPCMetadata is the recovered method.
func (c *BAICTelemetryClient) RecordRPCMetadata(rpc, status string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordReadUrlContent is the recovered method.
func (c *BAICTelemetryClient) RecordReadUrlContent(url string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordRemoteControlEvent is the recovered method.
func (c *BAICTelemetryClient) RecordRemoteControlEvent(event string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordStateInitializationData is the recovered method.
func (c *BAICTelemetryClient) RecordStateInitializationData(state string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordTrajectorySegmentAnalytics is the recovered method.
func (c *BAICTelemetryClient) RecordTrajectorySegmentAnalytics(segID string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordTrajectorySegmentEvents is the recovered method.
func (c *BAICTelemetryClient) RecordTrajectorySegmentEvents(segID, evt string) error {
	if c == nil {
		return nil
	}
	return nil
}

// Shutdown is the recovered method.
func (c *BAICTelemetryClient) Shutdown() error {
	if c == nil {
		return nil
	}
	return nil
}
