// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"
	"time"
)

func TestWSHub_Broadcast(t *testing.T) {
	hub := NewWSHub()
	go hub.Run()

	// Give hub time to start
	time.Sleep(10 * time.Millisecond)

	// Test broadcast doesn't panic with no clients
	hub.Broadcast("test", map[string]string{"key": "value"})

	// Test BroadcastJob
	job := &Job{
		ID:     "test123",
		Repo:   "test/repo",
		Status: JobStatusRunning,
	}
	hub.BroadcastJob(job)

	// Test BroadcastEvent
	hub.BroadcastEvent(map[string]string{"event": "test"})
}

func TestWSHub_ClientCount(t *testing.T) {
	hub := NewWSHub()
	go hub.Run()

	time.Sleep(10 * time.Millisecond)

	count := hub.ClientCount()
	if count != 0 {
		t.Errorf("Expected 0 clients, got %d", count)
	}
}

