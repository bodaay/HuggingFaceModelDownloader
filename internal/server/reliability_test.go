// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWritePump_OneMessagePerFrame: the browser parses each frame as one JSON
// document. Messages queued together used to be sent as a single
// newline-joined frame, which failed to parse and dropped every update in it
// (including final job states), leaving jobs looking stuck (github #87/#88).
func TestWritePump_OneMessagePerFrame(t *testing.T) {
	msgs := []string{`{"type":"a"}`, `{"type":"b"}`, `{"type":"c"}`}
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &WSClient{conn: conn, send: make(chan []byte, len(msgs))}
		for _, m := range msgs { // queue all before the pump starts
			c.send <- []byte(m)
		}
		close(c.send)
		c.writePump()
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	for i, want := range msgs {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("frame %d is not a single JSON document: %q", i, data)
		}
		if string(data) != want {
			t.Errorf("frame %d = %s, want %s", i, data, want)
		}
	}
}

// TestFinishRunStatus_ResumeWhileWindingDown: if Resume re-queues a paused job
// before the paused run has returned, that run must not mark the job
// cancelled — the scheduler would then skip it and the resume would silently
// never run.
func TestFinishRunStatus_ResumeWhileWindingDown(t *testing.T) {
	job := &Job{Status: JobStatusQueued, generation: 1}
	if finishRunStatusLocked(job, 1, errors.New("context canceled"), nil) {
		t.Fatal("stale paused run claimed ownership of a re-queued job")
	}
	if job.Status != JobStatusQueued {
		t.Errorf("status = %s, want queued", job.Status)
	}
}

func TestFinishRunStatus_Outcomes(t *testing.T) {
	cases := []struct {
		name   string
		ctxErr error
		runErr error
		want   JobStatus
	}{
		{"completed", nil, nil, JobStatusCompleted},
		{"failed", nil, errors.New("boom"), JobStatusFailed},
		{"cancelled", errors.New("context canceled"), errors.New("boom"), JobStatusCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := &Job{Status: JobStatusRunning, generation: 3, Progress: JobProgress{Activity: "Verifying x"}}
			if !finishRunStatusLocked(job, 3, tc.ctxErr, tc.runErr) {
				t.Fatal("current run should record its outcome")
			}
			if job.Status != tc.want {
				t.Errorf("status = %s, want %s", job.Status, tc.want)
			}
			if job.Progress.Activity != "" {
				t.Errorf("activity not cleared: %q", job.Progress.Activity)
			}
		})
	}

	t.Run("stale generation", func(t *testing.T) {
		job := &Job{Status: JobStatusRunning, generation: 4}
		if finishRunStatusLocked(job, 3, nil, nil) {
			t.Error("stale run must not record an outcome")
		}
	})
	t.Run("paused", func(t *testing.T) {
		job := &Job{Status: JobStatusPaused, generation: 3}
		if finishRunStatusLocked(job, 3, errors.New("context canceled"), nil) || job.Status != JobStatusPaused {
			t.Error("paused job must stay paused")
		}
	})
}
