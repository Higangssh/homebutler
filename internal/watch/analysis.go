package watch

import (
	"fmt"
	"regexp"
	"strings"
)

type CrashInfo struct {
	ExitCode  int
	OOMKilled bool
	Signal    string
	ErrorLog  string
	Backend   string
}

type CrashSummary struct {
	Category   string   `json:"category"`
	Reason     string   `json:"reason"`
	ExitCode   int      `json:"exit_code"`
	Signal     string   `json:"signal,omitempty"`
	Patterns   []string `json:"patterns,omitempty"`
	Confidence string   `json:"confidence"`
}

type logPattern struct {
	re       *regexp.Regexp
	name     string
	category string
	priority int
}

var logPatterns = []logPattern{
	{regexp.MustCompile(`(?i)out of memory`), "out of memory", "oom", 0},
	{regexp.MustCompile(`(?i)cannot allocate`), "cannot allocate", "oom", 0},
	{regexp.MustCompile(`(?i)\boom\b`), "oom", "oom", 0},
	{regexp.MustCompile(`(?i)segmentation fault`), "segmentation fault", "segfault", 1},
	{regexp.MustCompile(`(?i)sigsegv`), "sigsegv", "segfault", 1},
	{regexp.MustCompile(`panic:`), "panic:", "panic", 2},
	{regexp.MustCompile(`goroutine \d+`), `goroutine \d+`, "panic", 2},
	{regexp.MustCompile(`(?i)connection refused`), "connection refused", "dependency", 3},
	{regexp.MustCompile(`(?i)no such host`), "no such host", "dependency", 3},
	{regexp.MustCompile(`(?i)connection reset`), "connection reset", "dependency", 3},
	{regexp.MustCompile(`(?i)fatal`), "fatal", "fatal_error", 4},
	{regexp.MustCompile(`(?i)critical`), "critical", "fatal_error", 4},
	{regexp.MustCompile(`(?i)timeout`), "timeout", "timeout", 5},
	{regexp.MustCompile(`(?i)deadline exceeded`), "deadline exceeded", "timeout", 5},
}

func Analyze(info CrashInfo) CrashSummary {
	s := CrashSummary{
		ExitCode: info.ExitCode,
		Signal:   info.Signal,
	}

	if info.OOMKilled {
		s.Category = "oom"
		s.Reason = "Process killed by OOM killer"
		s.Confidence = "high"
		return s
	}

	switch info.ExitCode {
	case 137:
		s.Category = "oom"
		s.Reason = "Process received SIGKILL (likely OOM)"
		s.Confidence = "high"
		s.Signal = "SIGKILL"
		return s
	case 139:
		s.Category = "segfault"
		s.Reason = "Segmentation fault (SIGSEGV)"
		s.Confidence = "high"
		s.Signal = "SIGSEGV"
		return s
	case 143:
		s.Category = "sigterm"
		s.Reason = "Graceful termination requested (SIGTERM)"
		s.Confidence = "medium"
		s.Signal = "SIGTERM"
		return s
	}

	if info.ErrorLog != "" {
		bestPriority := len(logPatterns)
		bestCategory := ""
		var matched []string

		for _, p := range logPatterns {
			if p.re.MatchString(info.ErrorLog) {
				matched = append(matched, p.name)
				if p.priority < bestPriority {
					bestPriority = p.priority
					bestCategory = p.category
				}
			}
		}

		if len(matched) > 0 {
			s.Category = bestCategory
			s.Patterns = matched
			s.Confidence = confidenceForCategory(bestCategory)
			s.Reason = reasonForCategory(bestCategory, matched)
			return s
		}
	}

	switch info.ExitCode {
	case 1:
		s.Category = "error"
		s.Reason = "Application exited with error"
		s.Confidence = "medium"
	case 0:
		s.Category = "clean_restart"
		s.Reason = "Process exited cleanly"
		s.Confidence = "low"
	default:
		s.Category = "unknown"
		s.Reason = fmt.Sprintf("Unknown exit code %d", info.ExitCode)
		s.Confidence = "low"
	}

	return s
}

func confidenceForCategory(cat string) string {
	switch cat {
	case "oom", "segfault", "panic":
		return "high"
	default:
		return "medium"
	}
}

func reasonForCategory(cat string, patterns []string) string {
	switch cat {
	case "oom":
		return "Out of memory detected in logs"
	case "segfault":
		return "Segmentation fault detected in logs"
	case "panic":
		return "Go panic detected in logs"
	case "dependency":
		return "Dependency connection failure"
	case "fatal_error":
		return "Fatal error detected in logs"
	case "timeout":
		return "Timeout detected in logs"
	default:
		return fmt.Sprintf("Log patterns matched: %s", strings.Join(patterns, ", "))
	}
}
