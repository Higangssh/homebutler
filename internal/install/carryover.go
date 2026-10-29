package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Carryover is what Install or Uninstall did with an earlier install's state,
// for the caller to say. The zero value means there was nothing to do.
type Carryover struct {
	// Copied holds the state directories filled from the old container.
	Copied []string
	// Stranded holds the container paths an earlier install kept in
	// anonymous volumes when no container is left to copy them from.
	Stranded []string
}

// recoveryURL is where the steps to bring stranded state back are written.
const recoveryURL = "https://github.com/Higangssh/homebutler/blob/main/CHANGELOG.md"

// InstallWarning is what an install says about state it could not carry
// over, in one paragraph, or "" when there is none. It is the warning the
// MCP tool and the HTTP route answer with.
func (c Carryover) InstallWarning(appName string) string {
	if len(c.Stranded) == 0 {
		return ""
	}
	return fmt.Sprintf("The %s installed here before kept %s in anonymous Docker volumes, and the container that used them is gone, so this install started without them. "+
		"They are still on this machine unless they were pruned; how to find them and copy them back (#327): %s",
		appName, strings.Join(c.Stranded, " and "), recoveryURL)
}

// UninstallWarning is the same for an uninstall, which keeps what it finds
// and only has to say that this part of the data is not where the rest is.
func (c Carryover) UninstallWarning(appName, appDir string) string {
	if len(c.Stranded) == 0 {
		return ""
	}
	return fmt.Sprintf("%s's %s are not in %s: an earlier install kept them in anonymous Docker volumes, and the container that used them was already gone. "+
		"This uninstall did not touch them, and a reinstall will not find them by itself; how to copy them back (#327): %s",
		appName, strings.Join(c.Stranded, " and "), appDir, recoveryURL)
}

// Lines is what the CLI prints: what was copied, then the warning, with the
// command that lists the candidate volumes.
func (c Carryover) Lines(appName, warning string) []string {
	var lines []string
	for _, dir := range c.Copied {
		lines = append(lines, fmt.Sprintf("📦 Copied %s's state out of its Docker volume into %s", appName, dir))
	}
	if warning != "" {
		lines = append(lines, "⚠️  "+warning,
			"   The volumes no container uses: docker volume ls --filter dangling=true")
	}
	return lines
}

// carriedVolumesFile lists, one per line, the anonymous volumes state was
// copied out of. Nothing mounts them afterwards, so the containers no longer
// name them, and Purge reads this to remove them as well.
const carriedVolumesFile = ".carried-volumes"

// neverStarted is what Docker reports as StartedAt for a container that was
// created and never ran.
const neverStarted = "0001-01-01T00:00:00Z"

// stateMountsFormat prints when a container started, then each of its mounts
// as "type destination name".
const stateMountsFormat = `{{.State.StartedAt}}{{"\n"}}{{range .Mounts}}{{.Type}} {{.Destination}} {{.Name}}{{"\n"}}{{end}}`

// carryOverState moves an earlier install's state into the app's state
// directories, where the compose file Install writes now binds it.
//
// Until #327 filebrowser's compose file mapped only /srv, and the image's
// /config and /database went into anonymous volumes. Those survive `compose
// down`, but nothing points at them afterwards, and the next container gets
// new empty ones. So while the old container still exists, stop it and copy
// each such volume out with `docker cp` into its directory, unless that
// directory already holds something, which is never overwritten. A copy
// goes through a temporary directory, so a failure leaves the target empty
// and the next attempt tries again. Any failure is returned, and the caller
// must not run down or up after one: the container is the only thing that
// still names the volumes. The volumes are recorded in carriedVolumesFile
// before anything is copied, for Purge.
//
// A container that never started was created by a failed `up` and holds
// nothing of the old install, so it does not count as the old container.
// When the old compose file left a state path unmapped and no container that
// ran is left, the paths are returned as Stranded, for the caller to warn
// about.
func carryOverState(app App, appDir string) (Carryover, error) {
	var c Carryover
	if len(app.StateDirs) == 0 {
		return c, nil
	}
	composeFile := filepath.Join(appDir, "docker-compose.yml")
	old, err := os.ReadFile(composeFile)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}

	// The state dirs the old compose file did not bind, and that are still
	// empty here. A dir that holds something was carried over already, or
	// put there by hand.
	var pending []StateDir
	for _, d := range app.StateDirs {
		if strings.Contains(string(old), ":"+d.Path+`"`) {
			continue
		}
		if empty, err := emptyDir(filepath.Join(appDir, d.Name)); err != nil {
			return c, err
		} else if empty {
			pending = append(pending, d)
		}
	}
	if len(pending) == 0 {
		return c, nil
	}

	out, err := dockerCmd("compose", "-f", composeFile, "ps", "-a", "-q")
	if err != nil {
		return c, fmt.Errorf("could not ask Docker whether the old container still holds %s: %w", strings.Join(pathsOf(pending), " and "), err)
	}

	// Which container that ran holds each pending path, and in which volume.
	type source struct{ id, volume string }
	from := map[string]source{}
	ran := false
	for _, id := range strings.Fields(out) {
		info, err := dockerCmd("inspect", "--format", stateMountsFormat, id)
		if err != nil {
			return c, fmt.Errorf("could not read the old container's mounts: %w", err)
		}
		lines := strings.Split(info, "\n")
		if started := strings.TrimSpace(lines[0]); started == "" || started == neverStarted {
			continue
		}
		ran = true
		for _, line := range lines[1:] {
			if f := strings.Fields(line); len(f) == 3 && f[0] == "volume" {
				if _, seen := from[f[1]]; !seen {
					from[f[1]] = source{id, f[2]}
				}
			}
		}
	}
	if !ran {
		c.Stranded = pathsOf(pending)
		return c, nil
	}
	var todo []StateDir
	var volumes []string
	for _, d := range pending {
		if s, ok := from[d.Path]; ok {
			todo = append(todo, d)
			volumes = append(volumes, s.volume)
		}
	}
	if len(todo) == 0 {
		return c, nil
	}
	if err := recordCarriedVolumes(appDir, volumes); err != nil {
		return c, err
	}

	// A running app is still writing to its database.
	if _, err := dockerCmd("compose", "-f", composeFile, "stop"); err != nil {
		return c, fmt.Errorf("could not stop the old container to copy %s out of it: %w", strings.Join(pathsOf(todo), " and "), err)
	}
	for _, d := range todo {
		dst := filepath.Join(appDir, d.Name)
		tmp := filepath.Join(appDir, "."+d.Name+".copying")
		if err := os.RemoveAll(tmp); err != nil {
			return c, err
		}
		if err := os.MkdirAll(tmp, 0755); err != nil {
			return c, err
		}
		if out, err := dockerCmd("cp", from[d.Path].id+":"+d.Path+"/.", tmp); err != nil {
			_ = os.RemoveAll(tmp)
			msg := err.Error()
			if out != "" {
				msg = out
			}
			return c, fmt.Errorf("copying %s out of the old container failed, and it is still in its volume: %s", d.Path, msg)
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_ = os.RemoveAll(tmp)
			return c, err
		}
		if err := os.Rename(tmp, dst); err != nil {
			return c, err
		}
		c.Copied = append(c.Copied, dst)
	}
	return c, nil
}

// recordCarriedVolumes adds names to carriedVolumesFile, each once.
func recordCarriedVolumes(appDir string, names []string) error {
	all := append(carriedVolumes(appDir), names...)
	seen := map[string]bool{}
	var b strings.Builder
	for _, v := range all {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		b.WriteString(v + "\n")
	}
	return os.WriteFile(filepath.Join(appDir, carriedVolumesFile), []byte(b.String()), 0644)
}

// carriedVolumes reads carriedVolumesFile; a missing file is no volumes.
func carriedVolumes(appDir string) []string {
	data, err := os.ReadFile(filepath.Join(appDir, carriedVolumesFile))
	if err != nil {
		return nil
	}
	return parseVolumeNames(string(data))
}

// emptyDir reports whether dir is missing or has nothing in it.
func emptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func pathsOf(dirs []StateDir) []string {
	var out []string
	for _, d := range dirs {
		out = append(out, d.Path)
	}
	return out
}
