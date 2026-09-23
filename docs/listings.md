# Where homebutler is described

homebutler's own sentences about itself live in several places, and only some of
them are in this repository. The ones that are not go stale silently: nothing
here can read them, no test can fail because of them, and `git grep` stops at
the edge of this tree.

What that costs, concretely. For several releases a 95k-star MCP server list
described homebutler as an all-in-one homelab manager that monitors resources,
manages containers, and scans networks — true, and also true of the twenty other
entries in the same section. `report`, `backup drill` and `doctor` did not
appear, which are the three reasons to pick this one. The entry also named a
binary size that had been wrong for some time. The text was accurate on the day
it was written and no longer described the product.

## Copies this repository owns

Change these here and every reader gets the new wording on the next release.

| Where | What it feeds |
|---|---|
| `README.md` | GitHub, and anything that scrapes the README — Glama grades against it |
| `server.json` | the MCP Registry entry |
| `npm/package.json` | the npm page |
| `skills/SKILL.md` | what an agent is told homebutler is before it runs anything |
| `glama.json` | maintainer claim only; Glama takes the description from the README |

`internal/mcp/serverjson_test.go` holds `server.json` and `npm/package.json`
together on name and version, so those two cannot drift apart without the build
failing. Nothing checks that they still describe the same product as the README
— that part is read by a person.

## Copies in someone else's tree

None of these are in this tree, and none of them tell you when they have gone
wrong.

| Where | How it changes |
|---|---|
| `punkpeye/awesome-mcp-servers` | PR against the Monitoring section |
| `charm-and-friends/charm-in-the-wild` | PR against System Management, alphabetical |
| Glama | rebuilds itself from the README — the one entry here a commit still fixes |
| GitHub repository description and topics | repository settings, not a file |
| ClawHub | `skills/SKILL.md` published as `@higangssh/homebutler`; a new version has to be pushed |
| homebutler.dev | the `homebutler-site` repository, which builds on its own push |

The list is incomplete by nature. Add a row when you find one rather than
remembering it.

Glama is the exception in the table: it regrades from the README, so a commit
here reaches it and nobody has to open anything. Every other row needs a person
who remembers. Check which kind a row is before you go and fix it.

ClawHub is the row that costs the most when it goes stale, because what is
there is not a description of the product — it is a file an agent installs and
follows. `cmd/skill_test.go` pins every command and tool name in
`skills/SKILL.md`, which is the copy in this repository; the published copy is
whatever was last pushed. In September 2026 that was 9,891 bytes against
11,297 here, two corrections behind.

## Writing an entry somewhere else

**Name what the neighbours do not do.** Every list puts homebutler next to
twenty tools that also read Docker and also watch a host. Judged change
reporting, restore drills and `doctor` are what separate it; an entry that omits
them is filed correctly and does nothing.

**No number that moves.** Binary size, tool count, app-catalogue size — each is
wrong by some later release, and you will not be there to correct it. An entry
without a number stays true.

**Check the claim against what ships, not against memory.** Which libraries the
binary actually depends on is `go.mod`; which platforms it runs on is the
release assets. Lists that tag entries by library or OS make an unchecked guess
visibly wrong.

## When to walk the list

When the one-sentence answer to "what is homebutler" changes, and when a release
adds or removes a command someone elsewhere is describing. The repository copies
move with the commit; these do not, and the gap between them is exactly the
window in which homebutler is being introduced as something it no longer is.
