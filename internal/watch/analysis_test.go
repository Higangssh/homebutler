package watch

import (
	"strings"
	"testing"
)

func TestAnalyze(t *testing.T) {
	tests := []struct {
		name           string
		info           CrashInfo
		wantCategory   string
		wantConfidence string
		wantPatterns   []string
		checkReason    string
	}{
		{
			name:           "OOMKilled true",
			info:           CrashInfo{ExitCode: 0, OOMKilled: true, Backend: "docker"},
			wantCategory:   "oom",
			wantConfidence: "high",
		},
		{
			name:           "exit code 137 without OOMKilled",
			info:           CrashInfo{ExitCode: 137, Backend: "systemd"},
			wantCategory:   "oom",
			wantConfidence: "high",
		},
		{
			name:           "exit code 139 segfault",
			info:           CrashInfo{ExitCode: 139, Backend: "systemd"},
			wantCategory:   "segfault",
			wantConfidence: "high",
		},
		{
			name:           "exit code 143 SIGTERM",
			info:           CrashInfo{ExitCode: 143, Backend: "pm2"},
			wantCategory:   "sigterm",
			wantConfidence: "medium",
		},
		{
			name:           "exit code 1 no log",
			info:           CrashInfo{ExitCode: 1, Backend: "docker"},
			wantCategory:   "error",
			wantConfidence: "medium",
		},
		{
			name:           "exit code 0 clean restart",
			info:           CrashInfo{ExitCode: 0, Backend: "systemd"},
			wantCategory:   "clean_restart",
			wantConfidence: "low",
		},
		{
			name:           "log contains panic",
			info:           CrashInfo{ExitCode: 2, ErrorLog: "goroutine 1 [running]:\npanic: runtime error", Backend: "docker"},
			wantCategory:   "panic",
			wantConfidence: "high",
			wantPatterns:   []string{"panic:", "goroutine \\d+"},
		},
		{
			name:           "log contains connection refused",
			info:           CrashInfo{ExitCode: 1, ErrorLog: "dial tcp 127.0.0.1:5432: connection refused", Backend: "docker"},
			wantCategory:   "dependency",
			wantConfidence: "medium",
			wantPatterns:   []string{"connection refused"},
		},
		{
			name:           "log contains timeout",
			info:           CrashInfo{ExitCode: 1, ErrorLog: "context deadline exceeded; timeout waiting for response", Backend: "systemd"},
			wantCategory:   "timeout",
			wantConfidence: "medium",
			wantPatterns:   []string{"timeout", "deadline exceeded"},
		},
		{
			name:           "log contains FATAL uppercase",
			info:           CrashInfo{ExitCode: 1, ErrorLog: "FATAL: could not connect to database", Backend: "pm2"},
			wantCategory:   "fatal_error",
			wantConfidence: "medium",
			wantPatterns:   []string{"fatal"},
		},
		{
			name:           "exit code 137 with panic log - exit code wins",
			info:           CrashInfo{ExitCode: 137, ErrorLog: "panic: something went wrong", Backend: "docker"},
			wantCategory:   "oom",
			wantConfidence: "high",
		},
		{
			name:           "multiple patterns matched",
			info:           CrashInfo{ExitCode: 1, ErrorLog: "FATAL: connection refused to database\ntimeout on retry", Backend: "docker"},
			wantCategory:   "dependency",
			wantConfidence: "medium",
			wantPatterns:   []string{"connection refused", "fatal", "timeout"},
		},
		{
			name:           "unknown exit code empty log",
			info:           CrashInfo{ExitCode: 42, Backend: "systemd"},
			wantCategory:   "unknown",
			wantConfidence: "low",
		},
		{
			name:           "non-ASCII log Korean",
			info:           CrashInfo{ExitCode: 1, ErrorLog: "에러 발생: connection refused 데이터베이스 연결 실패", Backend: "docker"},
			wantCategory:   "dependency",
			wantConfidence: "medium",
			wantPatterns:   []string{"connection refused"},
		},
		{
			name:           "very long log 10000 chars",
			info:           CrashInfo{ExitCode: 1, ErrorLog: strings.Repeat("x", 9990) + "panic: oh", Backend: "docker"},
			wantCategory:   "panic",
			wantConfidence: "high",
			wantPatterns:   []string{"panic:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Analyze(tt.info)

			if got.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", got.Category, tt.wantCategory)
			}
			if got.Confidence != tt.wantConfidence {
				t.Errorf("Confidence = %q, want %q", got.Confidence, tt.wantConfidence)
			}
			if got.ExitCode != tt.info.ExitCode {
				t.Errorf("ExitCode = %d, want %d", got.ExitCode, tt.info.ExitCode)
			}
			if tt.wantPatterns != nil {
				for _, p := range tt.wantPatterns {
					found := false
					for _, gp := range got.Patterns {
						if gp == p {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Patterns %v missing expected %q", got.Patterns, p)
					}
				}
			}
			if tt.checkReason != "" && got.Reason != tt.checkReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.checkReason)
			}
			if got.Reason == "" {
				t.Error("Reason should not be empty")
			}
		})
	}
}
