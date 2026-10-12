package alerts

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Higangssh/homebutler/internal/config"
)

// config validate checks alerts.rules through this, against the Rule type
// watch actually decodes them into. The validator's own schema has no rules
// field, so it used to call a working rules block an unknown key that "has no
// effect" (#338).
func init() {
	config.RegisterAlertRulesCheck(checkRulesNode)
}

// ruleKeys are the yaml keys a rule accepts, read from Rule's own tags so the
// list cannot fall behind the struct.
func ruleKeys() []string {
	var keys []string
	t := reflect.TypeOf(Rule{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

// checkRulesNode reports unknown keys inside each rule, with their line, and
// then the first error watch would hit loading the rules. watch starts without
// any rules when that fails, so it is an error rather than a warning.
func checkRulesNode(node *yaml.Node) []config.Finding {
	var findings []config.Finding
	known := ruleKeys()
	if node.Kind == yaml.SequenceNode {
		for i, rule := range node.Content {
			if rule.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(rule.Content); j += 2 {
				key := rule.Content[j].Value
				if slices.Contains(known, key) {
					continue
				}
				findings = append(findings, config.Finding{
					Severity: config.SeverityWarning,
					Field:    fmt.Sprintf("alerts.rules[%d].%s", i, key),
					Message:  fmt.Sprintf("Line %d: field %s not found in an alerts.rules entry", rule.Content[j].Line, key),
					Hint:     "Unrecognised keys are ignored silently, so this line currently has no effect.",
				})
			}
		}
	}

	var rules []Rule
	if err := node.Decode(&rules); err != nil {
		return append(findings, config.Finding{
			Severity: config.SeverityError,
			Field:    "alerts.rules",
			Message:  fmt.Sprintf("Line %d: alerts.rules cannot be read: %v", node.Line, err),
			Hint:     "watch starts without these rules when this fails; the CPU, memory, disk and container checks still run.",
		})
	}
	if err := validateRules(rules); err != nil {
		findings = append(findings, config.Finding{
			Severity: config.SeverityError,
			Field:    "alerts.rules",
			Message:  "alerts.rules: " + err.Error(),
			Hint:     "watch starts without these rules when this fails; the CPU, memory, disk and container checks still run.",
		})
	}
	return findings
}
