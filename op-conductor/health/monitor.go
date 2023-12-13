package health

// HealthMonitor defines the interface for monitoring the health of the sequencer.
type HealthMonitor interface {
	// Subscribe returns a channel that will be notified for every health check.
	Subscribe() <-chan bool
	// Stop stops the health check.
	Stop()
}
