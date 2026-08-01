package llm

import "sync/atomic"

// Metrics holds process-local estimate/actual mismatch counters.
type Metrics struct {
	EstimateTotal atomic.Int64
	ActualTotal   atomic.Int64
	OverEstimate  atomic.Int64 // estimate > actual (count of events)
	UnderEstimate atomic.Int64 // estimate < actual
	Exact         atomic.Int64
}

// DefaultMetrics is the package-level counter set.
var DefaultMetrics Metrics

// RecordSettle updates mismatch counters for one reservation settle.
func RecordSettle(estimated, actual int64) {
	DefaultMetrics.Record(estimated, actual)
}

// Record updates this Metrics instance.
func (m *Metrics) Record(estimated, actual int64) {
	m.EstimateTotal.Add(estimated)
	m.ActualTotal.Add(actual)
	switch {
	case estimated > actual:
		m.OverEstimate.Add(1)
	case estimated < actual:
		m.UnderEstimate.Add(1)
	default:
		m.Exact.Add(1)
	}
}

// Snapshot is a point-in-time view.
type Snapshot struct {
	EstimateTotal int64
	ActualTotal   int64
	OverEstimate  int64
	UnderEstimate int64
	Exact         int64
}

// Snapshot returns current counters.
func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{
		EstimateTotal: m.EstimateTotal.Load(),
		ActualTotal:   m.ActualTotal.Load(),
		OverEstimate:  m.OverEstimate.Load(),
		UnderEstimate: m.UnderEstimate.Load(),
		Exact:         m.Exact.Load(),
	}
}
