package db

import (
	"sync"
	"time"
)

// RingBuffer provides a fixed-size circular buffer with O(1) operations
// This prevents memory growth and provides predictable performance
type RingBuffer[T any] struct {
	buffer   []T
	head     int
	tail     int
	size     int
	maxSize  int
	minTime  time.Time
	maxTime  time.Time
	mutex    sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with fixed maximum size
func NewRingBuffer[T any](maxSize int) *RingBuffer[T] {
	return &RingBuffer[T]{
		buffer:  make([]T, maxSize),
		maxSize: maxSize,
		minTime: time.Now(),
		maxTime: time.Now(),
	}
}

// Push adds an item to the ring buffer (O(1) operation)
func (rb *RingBuffer[T]) Push(item T) {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	// Add item at head position
	rb.buffer[rb.head] = item
	rb.head = (rb.head + 1) % rb.maxSize

	// Update size (only grows until full)
	if rb.size < rb.maxSize {
		rb.size++
	} else {
		// Buffer is full, move tail forward (overwrite oldest)
		rb.tail = (rb.tail + 1) % rb.maxSize
	}

	// Update time range if item has timestamp
	if entry, ok := any(item).(*LogEntry); ok {
		if rb.size == 1 || entry.Ts.Before(rb.minTime) {
			rb.minTime = entry.Ts
		}
		if entry.Ts.After(rb.maxTime) {
			rb.maxTime = entry.Ts
		}
	}
}

// Get retrieves an item at the specified index (0 = oldest, size-1 = newest)
func (rb *RingBuffer[T]) Get(index int) T {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	if index < 0 || index >= rb.size {
		var zero T
		return zero
	}

	// Calculate actual buffer position
	pos := (rb.tail + index) % rb.maxSize
	return rb.buffer[pos]
}

// Size returns the current number of items in the buffer
func (rb *RingBuffer[T]) Size() int {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return rb.size
}

// MaxSize returns the maximum capacity of the buffer
func (rb *RingBuffer[T]) MaxSize() int {
	return rb.maxSize
}

// IsFull returns true if the buffer is at maximum capacity
func (rb *RingBuffer[T]) IsFull() bool {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return rb.size == rb.maxSize
}

// Clear removes all items from the buffer
func (rb *RingBuffer[T]) Clear() {
	rb.mutex.Lock()
	defer rb.mutex.Unlock()

	rb.head = 0
	rb.tail = 0
	rb.size = 0
	rb.minTime = time.Now()
	rb.maxTime = time.Now()
}

// GetTimeRange returns the time range of entries in the buffer
func (rb *RingBuffer[T]) GetTimeRange() (minTime, maxTime time.Time) {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()
	return rb.minTime, rb.maxTime
}

// ToSlice returns a copy of all items as a slice (oldest to newest)
func (rb *RingBuffer[T]) ToSlice() []T {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	if rb.size == 0 {
		return []T{}
	}

	result := make([]T, rb.size)
	for i := 0; i < rb.size; i++ {
		pos := (rb.tail + i) % rb.maxSize
		result[i] = rb.buffer[pos]
	}

	return result
}

// ForEach executes a function for each item in the buffer (oldest to newest)
func (rb *RingBuffer[T]) ForEach(fn func(T)) {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	for i := 0; i < rb.size; i++ {
		pos := (rb.tail + i) % rb.maxSize
		fn(rb.buffer[pos])
	}
}

// Stats returns buffer statistics
func (rb *RingBuffer[T]) Stats() RingBufferStats {
	rb.mutex.RLock()
	defer rb.mutex.RUnlock()

	utilization := 0.0
	if rb.maxSize > 0 {
		utilization = float64(rb.size) / float64(rb.maxSize)
	}

	return RingBufferStats{
		Size:        rb.size,
		MaxSize:     rb.maxSize,
		Utilization: utilization,
		MinTime:     rb.minTime,
		MaxTime:     rb.maxTime,
	}
}

// RingBufferStats contains statistics about the ring buffer
type RingBufferStats struct {
	Size        int       `json:"size"`
	MaxSize     int       `json:"max_size"`
	Utilization float64   `json:"utilization"`
	MinTime     time.Time `json:"min_time"`
	MaxTime     time.Time `json:"max_time"`
}
