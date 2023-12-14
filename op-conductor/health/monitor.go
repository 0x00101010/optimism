package health

// HealthMonitor defines the interface for monitoring the health of the sequencer.
type HealthMonitor interface {
	// Subscribe returns a channel that will be notified for every health check.
	Subscribe() <-chan bool
	// Stop stops the health check.
	Stop()
}

// NewSequencerHealthMonitor creates a new sequencer health monitor.
func NewSequencerHealthMonitor() HealthMonitor {
	return &SequencerHealthMonitor{}
}

// SequencerHealthMonitor monitors sequencer health
type SequencerHealthMonitor struct{}

var _ HealthMonitor = (*SequencerHealthMonitor)(nil)

// Stop implements HealthMonitor.
func (*SequencerHealthMonitor) Stop() {
	// TODO: implement
}

// Subscribe implements HealthMonitor.
func (*SequencerHealthMonitor) Subscribe() <-chan bool {
	// TODO: implement
	ch := make(chan bool)
	return ch
}
