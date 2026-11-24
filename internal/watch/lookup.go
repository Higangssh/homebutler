package watch

import (
	"fmt"
	"strings"
)

// CheckIncidentQuery is the rule for asking about one incident: an ID or a
// target, never both and never neither. It is checked wherever the question
// is asked, so a call naming a server is refused before the round trip
// rather than by the server.
func CheckIncidentQuery(id, target string) error {
	switch {
	case id != "" && target != "":
		return fmt.Errorf("give an incident ID or a target, not both")
	case id == "" && target == "":
		return fmt.Errorf("an incident ID or a target is required")
	case strings.HasPrefix(id, "-"):
		return fmt.Errorf("invalid incident ID: %s", id)
	case strings.HasPrefix(target, "-"):
		return fmt.Errorf("invalid target: %s", target)
	}
	return nil
}

// FindIncident returns the incident with this ID or, given a target instead,
// the latest incident recorded for it.
//
// A target is the name watch list shows. A docker target added with a unit
// is watched under that unit, and its incidents carry the container's name,
// so the names a target answers to are its own and its unit's. Docker names
// are case-sensitive, so the match is exact; a name that differs only in case
// is used only when it is the one such name, and refused as ambiguous
// otherwise.
func FindIncident(dir, id, target string) (*Incident, error) {
	if err := CheckIncidentQuery(id, target); err != nil {
		return nil, err
	}
	if id != "" {
		return LoadIncident(dir, id)
	}

	incidents, err := ListIncidents(dir)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{target: true}
	targets, _ := LoadTargets(dir)
	for _, t := range targets {
		if t.Container == target {
			names[t.EffectiveUnit()] = true
		}
	}
	// ListIncidents is newest first, so the first match is the latest.
	for i := range incidents {
		if names[incidents[i].Container] {
			return &incidents[i], nil
		}
	}

	var folded []string
	seen := map[string]bool{}
	for _, inc := range incidents {
		if strings.EqualFold(inc.Container, target) && !seen[inc.Container] {
			seen[inc.Container] = true
			folded = append(folded, inc.Container)
		}
	}
	switch len(folded) {
	case 0:
		return nil, fmt.Errorf("no incident recorded for %q", target)
	case 1:
		return FindIncident(dir, "", folded[0])
	}
	return nil, fmt.Errorf("no incident recorded for %q; incidents exist for %s, which differ only in case", target, strings.Join(folded, ", "))
}

// IncidentContainer says which Docker container an incident is about, when
// that is known for certain: the name to look for in a report snapshot, or
// why there is none.
//
// An incident records the kind of monitor that wrote it, from the release
// that added incident_context on. One written before does not, and the watch list is the only other witness:
// it is used when exactly one target answers to the incident's name. None,
// because the target has been removed since, or two of different kinds,
// which the list allows because it keys on name and kind, is a guess, and a
// guess is not made.
func IncidentContainer(dir string, inc Incident) (name, why string) {
	if inc.Source != "" {
		return "", "this incident is from " + inc.Source + ", not a container"
	}
	targets, _ := LoadTargets(dir)
	kind := inc.Kind
	var matched []Target
	for _, t := range targets {
		if t.Container == inc.Container || t.EffectiveUnit() == inc.Container {
			if kind == "" || t.EffectiveKind() == kind {
				matched = append(matched, t)
			}
		}
	}
	if kind == "" {
		switch {
		case len(matched) == 0:
			return "", "this incident does not record its kind and no watched target has its name, so it is not known to be a container"
		case len(matched) > 1 && !sameKind(matched):
			return "", "this incident does not record its kind and targets of more than one kind have its name, so it is not known to be a container"
		}
		kind = matched[0].EffectiveKind()
	}
	if kind != KindDocker {
		return "", "this incident is for a " + kind + " target, not a container"
	}
	// The incident carries the name Docker was asked about: the unit, for
	// a target watched under one, since that is the name docker events
	// reports.
	return inc.Container, ""
}

func sameKind(targets []Target) bool {
	for _, t := range targets[1:] {
		if t.EffectiveKind() != targets[0].EffectiveKind() {
			return false
		}
	}
	return true
}
