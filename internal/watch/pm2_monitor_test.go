package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPM2Monitor_ParseProcesses(t *testing.T) {
	pm := &PM2Monitor{}
	procs := []pm2Process{
		{Name: "api", PM2Env: pm2Env{RestartTime: 3, Status: "online"}},
		{Name: "worker", PM2Env: pm2Env{RestartTime: 0, Status: "online"}},
	}
	data, _ := json.Marshal(procs)
	parsed := pm.parseProcesses(string(data))
	if len(parsed) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(parsed))
	}
	if parsed[0].Name != "api" {
		t.Errorf("expected api, got %s", parsed[0].Name)
	}
	if parsed[0].PM2Env.RestartTime != 3 {
		t.Errorf("expected RestartTime=3, got %d", parsed[0].PM2Env.RestartTime)
	}
}

func TestPM2Monitor_ParseProcesses_Invalid(t *testing.T) {
	pm := &PM2Monitor{}
	parsed := pm.parseProcesses("not json")
	if parsed != nil {
		t.Errorf("expected nil for invalid JSON, got %v", parsed)
	}
}

func TestPM2Monitor_ParseProcesses_EmptyArray(t *testing.T) {
	pm := &PM2Monitor{}
	parsed := pm.parseProcesses("[]")
	if len(parsed) != 0 {
		t.Errorf("expected empty slice, got %d items", len(parsed))
	}
}

func TestPM2Monitor_ParseProcesses_MalformedJSON(t *testing.T) {
	pm := &PM2Monitor{}
	// Partial JSON
	parsed := pm.parseProcesses(`[{"name": "app"`)
	if parsed != nil {
		t.Errorf("expected nil for malformed JSON, got %v", parsed)
	}
}

func TestPM2Monitor_DetectRestart(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if name != "pm2" {
			return "", fmt.Errorf("unexpected command: %s", name)
		}
		procs := []pm2Process{
			{Name: "my-api", PM2Env: pm2Env{RestartTime: callCount, Status: "online"}},
		}
		data, _ := json.Marshal(procs)
		return string(data), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{
		Run:      runner,
		Interval: 100 * time.Millisecond,
		ReadFile: func(path string) ([]byte, error) {
			return []byte("error line 1\nerror line 2"), nil
		},
	}

	targets := []Target{
		{Container: "my-api", Kind: "pm2", Unit: "my-api"},
	}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "my-api" {
			t.Errorf("expected my-api, got %s", inc.Container)
		}
		if inc.PreLogs == "" {
			t.Error("expected non-empty PreLogs")
		}
		if inc.RestartCount < 2 {
			t.Errorf("expected RestartCount >= 2, got %d", inc.RestartCount)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for incident")
	}
}

func TestPM2Monitor_NoTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pm := &PM2Monitor{
		Run:      func(string, ...string) (string, error) { return "[]", nil },
		Interval: 50 * time.Millisecond,
	}
	incCh := make(chan Incident, 10)
	err := pm.Watch(ctx, nil, incCh)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestPM2Monitor_NilRunner(t *testing.T) {
	ctx := context.Background()
	pm := &PM2Monitor{Run: nil}
	incCh := make(chan Incident, 10)
	targets := []Target{{Container: "app", Kind: "pm2", Unit: "app"}}
	err := pm.Watch(ctx, targets, incCh)
	if err == nil || !strings.Contains(err.Error(), "requires a CommandRunner") {
		t.Errorf("expected CommandRunner error, got %v", err)
	}
}

func TestPM2Monitor_CaptureErrorLog(t *testing.T) {
	pm := &PM2Monitor{}
	mockRead := func(path string) ([]byte, error) {
		lines := make([]string, 150)
		for i := range lines {
			lines[i] = fmt.Sprintf("line %d", i+1)
		}
		return []byte(strings.Join(lines, "\n")), nil
	}
	result := pm.captureErrorLog(mockRead, "test-app")
	// Should only have last 100 lines
	var nonEmpty int
	for _, l := range strings.Split(result, "\n") {
		if l != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 100 {
		t.Errorf("expected <= 100 lines, got %d", nonEmpty)
	}
}

func TestPM2Monitor_CaptureErrorLog_FileNotFound(t *testing.T) {
	pm := &PM2Monitor{}
	mockRead := func(path string) ([]byte, error) {
		return nil, fmt.Errorf("no such file or directory")
	}
	result := pm.captureErrorLog(mockRead, "missing-app")
	if !strings.Contains(result, "cannot read error log") {
		t.Errorf("expected 'cannot read error log' message, got: %s", result)
	}
}

func TestPM2Monitor_CaptureErrorLog_SmallFile(t *testing.T) {
	pm := &PM2Monitor{}
	mockRead := func(path string) ([]byte, error) {
		return []byte("line1\nline2\nline3"), nil
	}
	result := pm.captureErrorLog(mockRead, "small-app")
	if !strings.Contains(result, "line1") {
		t.Errorf("expected all lines returned for small file, got: %s", result)
	}
}

func TestPM2Monitor_RestartTimeZeroToPositive(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		restartTime := 0
		if callCount > 1 {
			restartTime = 5 // jump from 0 to 5
		}
		procs := []pm2Process{
			{Name: "app", PM2Env: pm2Env{RestartTime: restartTime, Status: "online"}},
		}
		data, _ := json.Marshal(procs)
		return string(data), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{
		Run:      runner,
		Interval: 100 * time.Millisecond,
		ReadFile: func(path string) ([]byte, error) {
			return []byte("error log"), nil
		},
	}
	targets := []Target{{Container: "app", Kind: "pm2", Unit: "app"}}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.RestartCount != 5 {
			t.Errorf("expected RestartCount=5, got %d", inc.RestartCount)
		}
		if !strings.Contains(inc.PrevStarted, "0") {
			t.Errorf("expected prev restart_time 0, got %s", inc.PrevStarted)
		}
		if !strings.Contains(inc.CurrStarted, "5") {
			t.Errorf("expected curr restart_time 5, got %s", inc.CurrStarted)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestPM2Monitor_CommandRunnerError_OnPoll(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if callCount <= 1 {
			// Seed succeeds
			procs := []pm2Process{{Name: "app", PM2Env: pm2Env{RestartTime: 0, Status: "online"}}}
			data, _ := json.Marshal(procs)
			return string(data), nil
		}
		// All subsequent polls fail
		return "", fmt.Errorf("pm2 not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{{Container: "app", Kind: "pm2", Unit: "app"}}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	select {
	case <-incCh:
		t.Error("did not expect incident when pm2 fails")
	case <-ctx.Done():
		// Good: no crash
	}
}

func TestPM2Monitor_SeedError(t *testing.T) {
	// Seed error should be tolerated, and first poll should work
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		if callCount <= 1 {
			return "", fmt.Errorf("pm2 not available yet")
		}
		// After seed, return a process. No restart should happen on first successful poll
		// because we have no prev state (hasPrev is false).
		procs := []pm2Process{{Name: "app", PM2Env: pm2Env{RestartTime: 5, Status: "online"}}}
		data, _ := json.Marshal(procs)
		return string(data), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{Run: runner, Interval: 100 * time.Millisecond}
	targets := []Target{{Container: "app", Kind: "pm2", Unit: "app"}}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	// No incident expected: seed failed, first poll sets baseline, no prev to compare
	select {
	case <-incCh:
		t.Error("did not expect incident when seed failed and first poll sets baseline")
	case <-ctx.Done():
		// Good
	}
}

func TestPM2Monitor_MultipleApps(t *testing.T) {
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		restartA := 0
		restartB := 0
		if callCount > 1 {
			restartA = 1 // only app-a restarts
		}
		procs := []pm2Process{
			{Name: "app-a", PM2Env: pm2Env{RestartTime: restartA, Status: "online"}},
			{Name: "app-b", PM2Env: pm2Env{RestartTime: restartB, Status: "online"}},
			{Name: "unwatched", PM2Env: pm2Env{RestartTime: 99, Status: "online"}},
		}
		data, _ := json.Marshal(procs)
		return string(data), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{
		Run:      runner,
		Interval: 100 * time.Millisecond,
		ReadFile: func(path string) ([]byte, error) {
			return []byte("error log"), nil
		},
	}
	targets := []Target{
		{Container: "app-a", Kind: "pm2", Unit: "app-a"},
		{Container: "app-b", Kind: "pm2", Unit: "app-b"},
	}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		if inc.Container != "app-a" {
			t.Errorf("expected app-a incident, got %s", inc.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}

func TestTailFile_NormalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := tailFile(path, 100)
	if err != nil {
		t.Fatalf("tailFile error: %v", err)
	}
	resultLines := strings.Split(result, "\n")
	var nonEmpty int
	for _, l := range resultLines {
		if l != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 100 {
		t.Errorf("expected <= 100 non-empty lines, got %d", nonEmpty)
	}
	// Last line should be "line 200"
	if !strings.Contains(result, "line 200") {
		t.Error("expected last line to be present")
	}
}

func TestTailFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := tailFile(path, 100)
	if err != nil {
		t.Fatalf("tailFile error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string for empty file, got %q", result)
	}
}

func TestTailFile_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.log")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := tailFile(path, 100)
	if err != nil {
		t.Fatalf("tailFile error: %v", err)
	}
	if !strings.Contains(result, "line1") || !strings.Contains(result, "line3") {
		t.Errorf("expected all lines for small file, got: %s", result)
	}
}

func TestTailFile_FileNotFound(t *testing.T) {
	_, err := tailFile("/nonexistent/path/file.log", 100)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestTailFile_LargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.log")
	// Create a file with > 8192 bytes (larger than chunkSize)
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("this is a longer log line number %05d with some padding to make it big enough", i+1)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := tailFile(path, 50)
	if err != nil {
		t.Fatalf("tailFile error: %v", err)
	}
	resultLines := strings.Split(result, "\n")
	if len(resultLines) > 50 {
		t.Errorf("expected <= 50 lines, got %d", len(resultLines))
	}
	// Should contain the last line
	if !strings.Contains(result, "00500") {
		t.Error("expected last line number 500")
	}
}

func TestPM2Monitor_SaveIncident(t *testing.T) {
	dir := t.TempDir()
	callCount := 0
	runner := func(name string, args ...string) (string, error) {
		callCount++
		procs := []pm2Process{
			{Name: "app", PM2Env: pm2Env{RestartTime: callCount, Status: "online"}},
		}
		data, _ := json.Marshal(procs)
		return string(data), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	incCh := make(chan Incident, 10)
	pm := &PM2Monitor{
		Run:      runner,
		Dir:      dir,
		Interval: 100 * time.Millisecond,
		ReadFile: func(path string) ([]byte, error) {
			return []byte("error log"), nil
		},
	}
	targets := []Target{{Container: "app", Kind: "pm2", Unit: "app"}}

	go func() {
		_ = pm.Watch(ctx, targets, incCh)
	}()

	select {
	case inc := <-incCh:
		loaded, err := LoadIncident(dir, inc.ID)
		if err != nil {
			t.Errorf("failed to load saved incident: %v", err)
		}
		if loaded != nil && loaded.Container != "app" {
			t.Errorf("expected app, got %s", loaded.Container)
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}
