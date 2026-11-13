package watch

// ValidName reports whether name may be put on the watch list, as a target's
// name or as its unit. Both are handed to docker, systemctl, journalctl and
// pm2 as an argument, so the rule is what keeps one from being read as
// something else: letters, digits, '-', '_' and '.', at most 128 of them, and
// never starting with '-', which every one of those programs takes as an
// option. `systemctl restart --help` exits 0, so a unit named --help was
// counted as a restart.
//
// It is checked when a target is added and nowhere else. A list written
// before the leading '-' was refused keeps working as it did; remove such a
// target and add it again under its real name.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 128 || name[0] == '-' {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
