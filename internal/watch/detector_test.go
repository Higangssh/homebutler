package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectRestart_NilPrev(t *testing.T) {
	curr := &InspectResult{RestartCount: 0, StartedAt: "2025-01-01T00:00:00Z", Running: true}
	ev := DetectRestart(nil, curr)
	if ev != nil {
		t.Error("expected nil for nil prev state")
	}
}

func TestDetectRestart_NoChange(t *testing.T) {
	prev := &ContainerState{
		Container:    "nginx",
		RestartCount: 3,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 3, StartedAt: "2025-01-01T00:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	if ev != nil {
		t.Error("expected nil when no change")
	}
}

func TestDetectRestart_RestartCountIncrease(t *testing.T) {
	prev := &ContainerState{
		Container:    "nginx",
		RestartCount: 3,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 5, StartedAt: "2025-01-01T01:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	if ev == nil {
		t.Fatal("expected restart event")
	}
	if ev.RestartCount != 5 {
		t.Errorf("expected RestartCount=5, got %d", ev.RestartCount)
	}
	if ev.PrevStarted != "2025-01-01T00:00:00Z" {
		t.Errorf("unexpected PrevStarted: %s", ev.PrevStarted)
	}
	if ev.CurrStarted != "2025-01-01T01:00:00Z" {
		t.Errorf("unexpected CurrStarted: %s", ev.CurrStarted)
	}
}

func TestDetectRestart_StartedAtChange(t *testing.T) {
	prev := &ContainerState{
		Container:    "redis",
		RestartCount: 0,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 0, StartedAt: "2025-01-01T05:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	if ev == nil {
		t.Fatal("expected restart event from StartedAt change")
	}
	if ev.Container != "redis" {
		t.Errorf("expected redis, got %s", ev.Container)
	}
}

func TestDetectRestart_EmptyPrevStartedAt(t *testing.T) {
	prev := &ContainerState{
		Container:    "app",
		RestartCount: 0,
		StartedAt:    "",
	}
	curr := &InspectResult{RestartCount: 0, StartedAt: "2025-01-01T00:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	if ev != nil {
		t.Error("expected nil when prev StartedAt is empty (first observation)")
	}
}

func TestDetectRestart_RestartCountTakesPriority(t *testing.T) {
	prev := &ContainerState{
		Container:    "app",
		RestartCount: 1,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 2, StartedAt: "2025-01-01T00:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	if ev == nil {
		t.Fatal("expected restart event from RestartCount increase")
	}
	if ev.RestartCount != 2 {
		t.Errorf("expected RestartCount=2, got %d", ev.RestartCount)
	}
}

func TestDetectRestart_RestartCountDecrease(t *testing.T) {
	// RestartCount decreasing (e.g., container recreated) should not trigger via RestartCount branch
	prev := &ContainerState{
		Container:    "app",
		RestartCount: 5,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 0, StartedAt: "2025-01-01T05:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	// Should still detect via StartedAt change
	if ev == nil {
		t.Fatal("expected restart event from StartedAt change when count decreased")
	}
	if ev.RestartCount != 0 {
		t.Errorf("expected RestartCount=0, got %d", ev.RestartCount)
	}
}

func TestDetectRestart_BothSameStartedAt_SameCount(t *testing.T) {
	prev := &ContainerState{
		Container:    "app",
		RestartCount: 0,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 0, StartedAt: "2025-01-01T00:00:00Z", Running: false}
	ev := DetectRestart(prev, curr)
	if ev != nil {
		t.Error("expected nil when nothing changed (even if not running)")
	}
}

func TestDetectRestart_RestartCountIncrease_SameStartedAt(t *testing.T) {
	// RestartCount increased but StartedAt same -- still detect via count
	prev := &ContainerState{
		Container:    "app",
		RestartCount: 1,
		StartedAt:    "2025-01-01T00:00:00Z",
	}
	curr := &InspectResult{RestartCount: 3, StartedAt: "2025-01-01T00:00:00Z", Running: true}
	ev := DetectRestart(prev, curr)
	if ev == nil {
		t.Fatal("expected event from RestartCount increase")
	}
	if ev.Container != "app" {
		t.Errorf("expected app, got %s", ev.Container)
	}
}

func TestInspectResultStruct(t *testing.T) {
	r := InspectResult{
		RestartCount: 3,
		StartedAt:    "2025-01-01T00:00:00Z",
		Running:      true,
	}
	if r.RestartCount != 3 {
		t.Errorf("expected 3, got %d", r.RestartCount)
	}
	if !r.Running {
		t.Error("expected Running=true")
	}
}

func TestRunResultStruct(t *testing.T) {
	r := RunResult{Checked: 5}
	if r.Checked != 5 {
		t.Errorf("expected 5, got %d", r.Checked)
	}
	if r.Incidents != nil {
		t.Error("expected nil incidents")
	}
}

func TestCheckTargets_NoTargets(t *testing.T) {
	dir := t.TempDir()
	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if incidents != nil {
		t.Errorf("expected nil incidents, got %v", incidents)
	}
}

func TestCheckTargets_SkipsNonDocker(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{
		{Container: "nginx", Kind: "systemd", Unit: "nginx.service"},
		{Container: "app", Kind: "pm2", Unit: "app"},
	}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}

	// Mock InspectContainer - should NOT be called for non-docker targets
	origInspect := inspectContainerFunc
	defer func() { inspectContainerFunc = origInspect }()
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		t.Errorf("InspectContainer should not be called for non-docker target, got %s", name)
		return nil, fmt.Errorf("should not be called")
	}

	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 0 {
		t.Errorf("expected 0 incidents, got %d", len(incidents))
	}
}

func TestCheckTargets_DetectsRestart(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{
		{Container: "nginx", Kind: "docker"},
	}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}

	// Set up previous state with restart_count=3
	states := map[string]*ContainerState{
		"nginx": {
			Container:    "nginx",
			RestartCount: 3,
			StartedAt:    "2025-01-01T00:00:00Z",
			LastChecked:  time.Now().Add(-1 * time.Hour),
		},
	}
	if err := SaveState(dir, states); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectContainerFunc
	origCapture := captureLogsFunc
	defer func() {
		inspectContainerFunc = origInspect
		captureLogsFunc = origCapture
	}()

	inspectContainerFunc = func(name string) (*InspectResult, error) {
		return &InspectResult{
			RestartCount: 5,
			StartedAt:    "2025-01-01T05:00:00Z",
			Running:      true,
		}, nil
	}
	captureLogsFunc = func(container string, lines string) string {
		return "captured log lines"
	}

	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	if incidents[0].Container != "nginx" {
		t.Errorf("expected nginx, got %s", incidents[0].Container)
	}
	if incidents[0].RestartCount != 5 {
		t.Errorf("expected RestartCount=5, got %d", incidents[0].RestartCount)
	}
}

func TestCheckTargets_NoRestart(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{{Container: "nginx", Kind: "docker"}}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}

	states := map[string]*ContainerState{
		"nginx": {
			Container:    "nginx",
			RestartCount: 3,
			StartedAt:    "2025-01-01T00:00:00Z",
		},
	}
	if err := SaveState(dir, states); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectContainerFunc
	defer func() { inspectContainerFunc = origInspect }()
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		return &InspectResult{
			RestartCount: 3,
			StartedAt:    "2025-01-01T00:00:00Z",
			Running:      true,
		}, nil
	}

	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 0 {
		t.Errorf("expected 0 incidents, got %d", len(incidents))
	}
}

func TestCheckTargets_InspectError(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{{Container: "nginx", Kind: "docker"}}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectContainerFunc
	origStderr := defaultStderr
	defer func() {
		inspectContainerFunc = origInspect
		defaultStderr = origStderr
	}()
	// Redirect stderr to discard warnings
	defaultStderr = os.NewFile(0, os.DevNull)

	inspectContainerFunc = func(name string) (*InspectResult, error) {
		return nil, fmt.Errorf("container not found")
	}

	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 0 {
		t.Errorf("expected 0 incidents when inspect fails, got %d", len(incidents))
	}
}

func TestCheckTargets_NoPrevState(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{{Container: "nginx", Kind: "docker"}}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectContainerFunc
	defer func() { inspectContainerFunc = origInspect }()
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		return &InspectResult{
			RestartCount: 0,
			StartedAt:    "2025-01-01T00:00:00Z",
			Running:      true,
		}, nil
	}

	// No previous state exists
	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First observation, no restart detected
	if len(incidents) != 0 {
		t.Errorf("expected 0 incidents for first observation, got %d", len(incidents))
	}

	// Verify state was saved
	states, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := states["nginx"]
	if !ok {
		t.Fatal("expected nginx state to be saved")
	}
	if s.RestartCount != 0 {
		t.Errorf("expected RestartCount=0, got %d", s.RestartCount)
	}
}

func TestCheckTargets_MixedDockerAndOther(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{
		{Container: "nginx", Kind: "docker"},
		{Container: "svc", Kind: "systemd", Unit: "svc.service"},
		{Container: "api", Kind: ""}, // empty kind defaults to docker
	}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectContainerFunc
	defer func() { inspectContainerFunc = origInspect }()

	inspectedContainers := []string{}
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		inspectedContainers = append(inspectedContainers, name)
		return &InspectResult{RestartCount: 0, StartedAt: "ts1", Running: true}, nil
	}

	_, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only inspect docker targets (nginx and api, not svc)
	if len(inspectedContainers) != 2 {
		t.Errorf("expected 2 inspected containers, got %d: %v", len(inspectedContainers), inspectedContainers)
	}
}

func TestCheckTargets_CorruptTargets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "targets.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CheckTargets(dir)
	if err == nil {
		t.Fatal("expected error for corrupt targets")
	}
}

func TestCheckTargets_CorruptState(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{{Container: "nginx", Kind: "docker"}}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CheckTargets(dir)
	if err == nil {
		t.Fatal("expected error for corrupt state")
	}
}

func TestCheckTargets_SavesIncident(t *testing.T) {
	dir := t.TempDir()
	targets := []Target{{Container: "nginx", Kind: "docker"}}
	if err := SaveTargets(dir, targets); err != nil {
		t.Fatal(err)
	}
	states := map[string]*ContainerState{
		"nginx": {Container: "nginx", RestartCount: 1, StartedAt: "ts1"},
	}
	if err := SaveState(dir, states); err != nil {
		t.Fatal(err)
	}

	origInspect := inspectContainerFunc
	origCapture := captureLogsFunc
	defer func() {
		inspectContainerFunc = origInspect
		captureLogsFunc = origCapture
	}()
	inspectContainerFunc = func(name string) (*InspectResult, error) {
		return &InspectResult{RestartCount: 2, StartedAt: "ts2", Running: true}, nil
	}
	captureLogsFunc = func(container string, lines string) string {
		return "post logs"
	}

	incidents, err := CheckTargets(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}

	// Verify incident was saved to disk
	loaded, lerr := LoadIncident(dir, incidents[0].ID)
	if lerr != nil {
		t.Errorf("failed to load saved incident: %v", lerr)
	}
	if loaded != nil && loaded.PostLogs != "post logs" {
		t.Errorf("expected post logs, got %s", loaded.PostLogs)
	}
}

func TestInspectResult_JSON(t *testing.T) {
	raw := `{"restart_count":3,"started_at":"2025-01-01T00:00:00Z","running":true}`
	var r InspectResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.RestartCount != 3 {
		t.Errorf("expected 3, got %d", r.RestartCount)
	}
	if r.StartedAt != "2025-01-01T00:00:00Z" {
		t.Errorf("expected 2025-01-01T00:00:00Z, got %s", r.StartedAt)
	}
	if !r.Running {
		t.Error("expected Running=true")
	}
}

func TestRunResult_JSON(t *testing.T) {
	r := RunResult{Checked: 2, Incidents: []Incident{{Container: "x"}}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "checked") {
		t.Error("expected 'checked' in JSON")
	}
}

// Ensure unused imports are used
var _ = fmt.Sprint
var _ = os.DevNull
var _ = filepath.Join
var _ = time.Now
var _ = strings.Contains

func TestRestartEventStruct(t *testing.T) {
	ev := RestartEvent{
		Container:    "redis",
		RestartCount: 10,
		PrevStarted:  "ts1",
		CurrStarted:  "ts2",
	}
	if ev.Container != "redis" {
		t.Errorf("expected redis, got %s", ev.Container)
	}
	if ev.RestartCount != 10 {
		t.Errorf("expected 10, got %d", ev.RestartCount)
	}
}
