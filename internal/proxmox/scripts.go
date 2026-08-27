package proxmox

import "fmt"

// scriptCatalogRef pins every generated command to one commit of
// community-scripts/ProxmoxVE instead of tracking main, so a script fetched
// today is the same bytes a week from now. Bump it deliberately when the
// catalog below needs a newer script.
//
// See #62: homebutler stops at generating this command for a human to review
// and run themselves. It never fetches or executes the script.
const scriptCatalogRef = "fadcb0c375547861f991c09ff8dec196c380d428"

// Script is one curated entry from the Proxmox VE Community Scripts catalog
// (https://github.com/community-scripts/ProxmoxVE).
type Script struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var scriptCatalog = []Script{
	{Slug: "docker", Name: "Docker", Description: "Docker CE and Docker Compose in an LXC"},
	{Slug: "homeassistant", Name: "Home Assistant", Description: "Home Assistant Core in an LXC"},
	{Slug: "nginxproxymanager", Name: "Nginx Proxy Manager", Description: "Nginx Proxy Manager in an LXC"},
	{Slug: "pihole", Name: "Pi-hole", Description: "Pi-hole DNS ad-blocker in an LXC"},
	{Slug: "postgresql", Name: "PostgreSQL", Description: "PostgreSQL database server in an LXC"},
}

// Scripts returns the curated Proxmox VE Community Scripts catalog.
func Scripts() []Script {
	return scriptCatalog
}

// ScriptCommand renders the pinned shell command for a cataloged script slug.
// It only builds text: homebutler never fetches or runs the script itself,
// and the caller is responsible for reviewing the command before running it
// on the Proxmox host.
func ScriptCommand(slug string) (string, error) {
	for _, script := range scriptCatalog {
		if script.Slug == slug {
			return fmt.Sprintf(`bash -c "$(curl -fsSL https://raw.githubusercontent.com/community-scripts/ProxmoxVE/%s/ct/%s.sh)"`, scriptCatalogRef, slug), nil
		}
	}
	return "", fmt.Errorf("unknown Proxmox Community Script %q; use the script catalog to see available slugs", slug)
}
