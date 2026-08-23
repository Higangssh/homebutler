# Security policy

## Reporting a vulnerability

Use [Report a vulnerability](https://github.com/Higangssh/homebutler/security/advisories/new)
on the Security tab. That opens a private thread visible only to you and the
maintainer.

Please do not open a public issue for a suspected vulnerability. Public issues
are how everyone else finds out before there is a fix.

## What to expect

You will get a first response **within seven days**. That is a deliberate number:
homebutler has one maintainer in one timezone, and a promise of twenty-four hours
would be one I could not keep during a bad week.

After that, expect updates as the report is confirmed, fixed, and released. If a
fix is going to take a while, you will hear why rather than nothing.

## Supported versions

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Everything else | No |

homebutler is pre-1.0, releases every few weeks, and has no branch other than
`main`. Nothing is backported. Claiming a support window that is not honoured
would be worse than admitting there is only one version.

This is worth revisiting at 1.0. Not before.

## What counts as a vulnerability

Most of what homebutler does is run commands you could run yourself, so it is
worth being specific about where the line is.

In scope:

- **Credentials reaching somewhere they should not.** Config holds SSH
  passwords, API tokens and notification credentials. Anything that leaks them
  into command output, `--json`, the report snapshots written under
  `~/.homebutler/`, or MCP responses.
- **The web dashboard** (`homebutler serve`): authentication bypass, unsafe token
  comparison, binding wider than the user asked for.
- **The MCP server**: an agent being able to do more than the tool surface says
  it can, or reaching a target it was not pointed at.
- **Remote execution** (`internal/remote`): host key handling, anything that
  would let a connection be redirected or trusted when it should not be.
- **`homebutler install`**: anything that runs code the catalogue did not intend
  to run.
- Privilege escalation, or a lower-privileged local user influencing what
  homebutler does.

Out of scope:

- homebutler run as root can do what root can do. That is the tool, not a
  finding.
- Findings that require an already-compromised host, or an attacker who can
  already edit your config file.
- A public port being reported as public. `doctor` and `inventory scan --filter
  exposed` are supposed to tell you that.
- Missing hardening in a dependency without a demonstrated path through
  homebutler.

If you are unsure which side something falls on, report it. A short conversation
costs less than a missed vulnerability.

## Credit

Reporters are credited by default: in the published advisory, in the CHANGELOG
entry for the release that carries the fix, and in the CVE record if one is
issued. Tell me if you would rather not be named and you will not be.

There is no bug bounty. homebutler is an unfunded side project, and paying for
reports is not something it can honestly offer. Credit is what there is.
