package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CheckIncidentQuery is the rule for asking about one incident: an ID or a
// target, never both and never neither, and neither starting with '-'. It is
// checked wherever the question is asked, so a call naming a server is
// refused before the round trip rather than by the server.
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

// readIncidents returns every incident that could be read, newest first, and
// a line for each file that could not. The lines name the file and the
// reason, never the directory: they go into answers that may be relayed
// anywhere, and the directory is the operator's home.
func readIncidents(dir string) ([]Incident, []string, error) {
	idir := incidentsDir(dir)
	entries, err := os.ReadDir(idir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading the incident directory: %w", withoutPath(err))
	}

	var incidents []Incident
	var unreadable []string
	for _, e := range entries {
		// An incident still being written, or left by a crash mid-write, is
		// under a name ending in tempSuffix, and is not one to warn about.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(idir, e.Name()))
		if err != nil {
			unreadable = append(unreadable, e.Name()+": "+withoutPath(err).Error())
			continue
		}
		var inc Incident
		if err := json.Unmarshal(data, &inc); err != nil {
			unreadable = append(unreadable, e.Name()+": "+err.Error())
			continue
		}
		incidents = append(incidents, inc)
	}

	sort.Slice(incidents, func(i, j int) bool {
		return incidents[i].DetectedAt.After(incidents[j].DetectedAt)
	})
	return incidents, unreadable, nil
}

// withoutPath is an error with the path of a failed file operation taken
// out, keeping what went wrong.
func withoutPath(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// FindIncident returns the incident with this ID or, given a target instead,
// the latest incident recorded for it, and a warning for every incident file
// that could not be read, since under a target one of those may be the
// newer incident.
//
// A target is the name watch list shows. A docker target added with a unit
// is watched under that unit, and its incidents carry the container's name,
// so a docker target also answers to its unit. Systemd and pm2 incidents are
// recorded under the target's own name, so theirs is not an alias. Docker
// names are case-sensitive, so the match is exact; a name that differs only
// in case is resolved the same way when it is the one such name, and refused
// as ambiguous otherwise.
func FindIncident(dir, id, target string) (*Incident, []string, error) {
	if err := CheckIncidentQuery(id, target); err != nil {
		return nil, nil, err
	}
	if id != "" {
		inc, err := LoadIncident(dir, id)
		return inc, nil, err
	}

	incidents, unreadable, err := readIncidents(dir)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	if len(unreadable) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d incident file(s) could not be read, so a newer incident for %s may be missing: %s",
			len(unreadable), target, strings.Join(unreadable, "; ")))
	}
	targets, terr := LoadTargets(dir)
	if terr != nil {
		warnings = append(warnings, targetsWarning(terr))
	}

	if inc := latestFor(incidents, targets, target); inc != nil {
		return inc, warnings, nil
	}
	// A watched target of exactly this name is the container meant. Another
	// whose name differs only in case is a different container, so its
	// incidents are not this one's.
	for _, t := range targets {
		if t.Container == target {
			return nil, warnings, noIncident(target, unreadable, warnings)
		}
	}

	var folded []string
	seen := map[string]bool{}
	fold := func(name string) {
		if name != target && strings.EqualFold(name, target) && !seen[name] {
			seen[name] = true
			folded = append(folded, name)
		}
	}
	for _, t := range targets {
		fold(t.Container)
	}
	for _, inc := range incidents {
		fold(inc.Container)
	}
	sort.Strings(folded)
	switch len(folded) {
	case 0:
	case 1:
		if inc := latestFor(incidents, targets, folded[0]); inc != nil {
			warnings = append(warnings, fmt.Sprintf("no incident is recorded for %q; this is the latest for %q, which differs from it only in case", target, folded[0]))
			return inc, warnings, nil
		}
	default:
		return nil, warnings, fmt.Errorf("no incident recorded for %q; %s differ from it only in case, so give the exact name", target, strings.Join(folded, ", "))
	}
	return nil, warnings, noIncident(target, unreadable, warnings)
}

func noIncident(target string, unreadable, warnings []string) error {
	if len(unreadable) > 0 {
		return fmt.Errorf("no readable incident recorded for %q; %s", target, warnings[0])
	}
	return fmt.Errorf("no incident recorded for %q", target)
}

// targetsWarning says the watch list could not be read, without its path.
func targetsWarning(err error) string {
	return "the watch list could not be read, so targets watched under a unit or by kind were not considered: " + withoutPath(err).Error()
}

// latestFor is the newest incident recorded under name, or under the unit of
// a docker target of that name. Under the unit only a docker incident, or
// one that does not record its kind, counts: a systemd or pm2 target can be
// named what the unit is.
func latestFor(incidents []Incident, targets []Target, name string) *Incident {
	aliases := unitAliases(targets, func(n string) bool { return n == name })
	// readIncidents is newest first, so the first match is the latest.
	for i := range incidents {
		inc := &incidents[i]
		if inc.Container == name || aliases.has(*inc) {
			return inc
		}
	}
	return nil
}

// unitAlias is the units a name stands for, and which of them are also the
// name of a target of another kind.
type unitAlias struct {
	units     map[string]bool
	otherKind map[string]bool
}

// unitAliases is the units of the docker targets whose name named accepts
// and that are watched under a unit, which is where the monitors record
// their incidents. It runs one way only. An incident recorded under a
// target's own name is about the container of that name: before `watch
// check` inspected the unit, it inspected the target's name, so such an
// incident is not the unit's.
func unitAliases(targets []Target, named func(string) bool) unitAlias {
	a := unitAlias{units: map[string]bool{}, otherKind: map[string]bool{}}
	for _, t := range targets {
		if named(t.Container) && t.EffectiveKind() == KindDocker && t.EffectiveUnit() != t.Container {
			a.units[t.EffectiveUnit()] = true
		}
	}
	for _, t := range targets {
		if t.EffectiveKind() != KindDocker && a.units[t.Container] {
			a.otherKind[t.Container] = true
		}
	}
	return a
}

// has is whether inc is one of the alias's: recorded under one of its units
// by a docker monitor. A Proxmox incident is not, whatever it is named. One
// that does not record its kind is taken to be docker's only while no
// systemd or pm2 target has the unit's name, since that target's incidents
// are recorded under it too; IncidentContainer refuses the same guess.
func (a unitAlias) has(inc Incident) bool {
	if inc.Source != "" || !a.units[inc.Container] {
		return false
	}
	switch inc.Kind {
	case KindDocker:
		return true
	case "":
		return !a.otherKind[inc.Container]
	}
	return false
}

// Attribution is what is known about whose an incident is.
type Attribution struct {
	// Container is the Docker name to look for in a report snapshot, or
	// empty when the incident is not known to be a container's.
	Container string
	// Why says why Container is empty.
	Why string
	// Inferred says how the kind was decided when the incident does not
	// record it, which is only ever from the watch list as it is now.
	Inferred string
	// Warning says the watch list could not be read.
	Warning string
}

// IncidentContainer says which Docker container an incident is about, when
// that is known: from the incident's own kind, or, for an incident recorded
// before kinds were, from the one watched target that has its name. None,
// because the target has been removed since, or two of different kinds,
// which the list allows because it keys on name and kind, is a guess, and a
// guess is not made. A kind read from the list says so in Inferred, since
// the list may have changed since the incident.
func IncidentContainer(dir string, inc Incident) Attribution {
	if inc.Source != "" {
		return Attribution{Why: "this incident is from " + inc.Source + ", not a container"}
	}
	if inc.Kind != "" {
		if inc.Kind != KindDocker {
			return Attribution{Why: "this incident was recorded by the " + inc.Kind + " monitor, not for a container"}
		}
		return Attribution{Container: inc.Container}
	}

	targets, err := LoadTargets(dir)
	if err != nil {
		return Attribution{
			Why:     "this incident does not record its kind and the watch list could not be read, so it is not known to be a container",
			Warning: targetsWarning(err),
		}
	}
	var matched []Target
	for _, t := range targets {
		if t.Container == inc.Container || (t.EffectiveKind() == KindDocker && t.EffectiveUnit() == inc.Container) {
			matched = append(matched, t)
		}
	}
	switch {
	case len(matched) == 0:
		return Attribution{Why: "this incident does not record its kind and no watched target has its name, so it is not known to be a container"}
	case !sameKind(matched):
		return Attribution{Why: "this incident does not record its kind and targets of more than one kind have its name, so it is not known to be a container"}
	}
	kind := matched[0].EffectiveKind()
	if kind != KindDocker {
		return Attribution{Why: "this incident does not record its kind, and the watch list now has a " + kind + " target of that name, so it is taken not to be a container"}
	}
	return Attribution{
		Container: inc.Container,
		Inferred:  "This incident does not record its kind; it is taken to be the container's because the watch list now has a docker target of that name.",
	}
}

func sameKind(targets []Target) bool {
	for _, t := range targets[1:] {
		if t.EffectiveKind() != targets[0].EffectiveKind() {
			return false
		}
	}
	return true
}
