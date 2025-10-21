package collector

import (
	"context"
	"fmt"
	"seyir/internal/logger"
	"sync"
)

// Manager handles multiple log collectors
type Manager struct {
	collectors map[string]LogSource
	mutex      sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewManager creates a new collector manager
// Note: Lake directory should be set via db.SetGlobalLakeDir() before using this
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Manager{
		collectors: make(map[string]LogSource),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// AddCollector adds a new log collector to the manager
func (m *Manager) AddCollector(name string, collector LogSource) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if _, exists := m.collectors[name]; exists {
		logger.Warn("Collector %s already exists, replacing it", name)
		m.collectors[name].Stop()
	}
	
	m.collectors[name] = collector
	
	// Start the collector
	go func() {
		if err := collector.Start(m.ctx); err != nil {
			logger.Error("Failed to start collector %s: %v", name, err)
			m.removeCollector(name)
		}
	}()
	
	logger.Info("Added and started collector: %s", name)
	return nil
}

// RemoveCollector removes and stops a collector
func (m *Manager) RemoveCollector(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	return m.removeCollector(name)
}

// removeCollector internal method to remove collector (caller must hold mutex)
func (m *Manager) removeCollector(name string) error {
	if collector, exists := m.collectors[name]; exists {
		if err := collector.Stop(); err != nil {
			logger.Error("Error stopping collector %s: %v", name, err)
		}
		if err := collector.Close(); err != nil {
			logger.Error("Error closing collector %s: %v", name, err)
		}
		delete(m.collectors, name)
		logger.Info("Removed collector: %s", name)
	}
	return nil
}

// StartAll starts all registered collectors
func (m *Manager) StartAll() error {
	m.mutex.RLock()
	collectors := make([]LogSource, 0, len(m.collectors))
	names := make([]string, 0, len(m.collectors))
	
	for name, collector := range m.collectors {
		collectors = append(collectors, collector)
		names = append(names, name)
	}
	m.mutex.RUnlock()
	
	for i, collector := range collectors {
		go func(name string, coll LogSource) {
			if err := coll.Start(m.ctx); err != nil {
				logger.Error("Failed to start collector %s: %v", name, err)
			}
		}(names[i], collector)
	}
	
	return nil
}

// StopAll stops all collectors
func (m *Manager) StopAll() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Cancel context to stop all collectors
	m.cancel()
	
	for name, collector := range m.collectors {
		if err := collector.Stop(); err != nil {
			logger.Error("Error stopping collector %s: %v", name, err)
		}
		if err := collector.Close(); err != nil {
			logger.Error("Error closing collector %s: %v", name, err)
		}
	}
	
	m.collectors = make(map[string]LogSource)
	logger.Info("Stopped all collectors")
	return nil
}

// GetCollectors returns a list of all registered collectors
func (m *Manager) GetCollectors() map[string]LogSource {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	result := make(map[string]LogSource, len(m.collectors))
	for name, collector := range m.collectors {
		result[name] = collector
	}
	return result
}

// GetHealthStatus returns the health status of all collectors
func (m *Manager) GetHealthStatus() map[string]bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	status := make(map[string]bool, len(m.collectors))
	for name, collector := range m.collectors {
		status[name] = collector.IsHealthy()
	}
	return status
}

// GetCollector returns a specific collector by name
func (m *Manager) GetCollector(name string) (LogSource, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	collector, exists := m.collectors[name]
	return collector, exists
}

// EnableStdin enables stdin log collection
func (m *Manager) EnableStdin(sourceName string) error {
	if sourceName == "" {
		sourceName = "stdin"
	}
	
	collector := NewStdinCollector(sourceName)
	if collector == nil {
		return fmt.Errorf("failed to create stdin collector")
	}
	return m.AddCollector("stdin", collector)
}

// EnableDocker enables Docker container discovery and log collection
func (m *Manager) EnableDocker() error {
	collector := NewDockerCollector()
	if collector == nil {
		return fmt.Errorf("failed to create docker collector")
	}
	return m.AddCollector("docker", collector)
}

// DisableStdin disables stdin log collection
func (m *Manager) DisableStdin() error {
	return m.RemoveCollector("stdin")
}

// DisableDocker disables Docker log collection
func (m *Manager) DisableDocker() error {
	return m.RemoveCollector("docker")
}