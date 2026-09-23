# What 1.0 freezes

homebutler is pre-1.0, and until the tag lands anything may change. 1.0 is the
point where that stops being true for the parts other people build on: an agent
branching on a change's `kind`, a cron job reading `doctor`'s exit code, a
widget polling `/api/report`, a compose file naming config keys.

This file says which of those are frozen. `internal/contract` holds the same
answer in a form a test can check, because a promise nobody can verify is not a
promise — the golden file there fails the build when any of it moves.

## Frozen at 1.0

| | |
| --- | --- |
| **MCP tool names** | A tool is not renamed or removed outside a major release |
| **Tool input schemas** | Argument names, their types, and which are required |
| **Tool risk classes** | `read`, `write`, `destructive`. An agent decides what it may do unattended from these, so a reclassification is a breaking change even though nothing about the call changes |
| **JSON field names and types** | What `report --json`, `doctor --json` and `status --json` return. A field that changes from a string to an object breaks a caller as thoroughly as one that disappears |
| **`failed_collectors`** | A closed set per surface, not one list. `report` and `inventory_scan` report `docker`, `ports`, `processes`; `proxmox_status` reports `version`, `cluster`, `resources`. A count is only trustworthy when its collector is absent from the list on that surface. The dashboard's own refresh reports under a different key, `refresh_failed_collectors`, and is not this field |
| **Proxmox reads** | `proxmox_status`, `proxmox_guests`, `proxmox_node`, `proxmox_tasks` and `proxmox_task_status` return Proxmox's own shape, and it is not ours to freeze — `pveversion`, `loadavg` and `cpuinfo` are their names. What is frozen is the envelope: `proxmox_status` keeps `warnings` and `failed_collectors`, because those are ours and mean the same thing they mean everywhere else |
| **The `kind` vocabulary** | The eight words a change can carry, and what each means |
| **The `runner` vocabulary** | `mcp`, `cli`, `shell` |
| **The `category` vocabulary** | The eleven words a `doctor` finding can carry, documented in the README |
| **`doctor` exit codes** | Including under `--strict` |
| **Documented config keys** | Everything in [configuration.md](configuration.md) keeps its name and meaning |
| **HTTP routes and their protection** | The path, the method, and what the tier below asks the caller to bring. A route that quietly stops needing one is the change nobody sees |
| **Which decision an absence waits on** | A capability the dashboard cannot reach records why. The sentence is prose and improves; the decision it names is frozen, so a tool quietly moving from "waiting on a rule" to "never" is a diff. Additive change after 1.0 is allowed — [the registry](../internal/capability/capability.go) is what says how much is still coming |

Four absences carried the wrong reason until they became routes.
`backup_list`, `install_list`, `install_status` and `proxmox_guests` said
`no view built for it yet`, which was true and was not what was keeping them
out: the actions beside them could not be called without them. The reason a
thing is absent is the part this file freezes, so a wrong one is not a typo —
it is a decision recorded as waiting on something it was not waiting on, and
it is what let three uncallable actions look finished.

A field whose value is a sentence — `report --json`'s `status` and `warnings`,
`doctor`'s `title`, `detail` and `action` — is frozen as a field: it keeps its
name and stays an array of strings. **The sentences inside it are not.** They
are written for a person and get better; nothing should be parsing them. Where
a caller needs a value rather than a sentence, the value is a typed field of
its own, and if one is missing that is a bug worth reporting rather than a
reason to split a string.

### What a browser has to bring

Three tiers, decided by one question — can this be undone by doing something
else? All three are frozen: a capability moving between them changes what a
dashboard has to ask for before it acts.

| Tier | The caller sends | Who is in it |
| --- | --- | --- |
| **action** | a token | the click that asks for it is the whole ceremony. `docker_restart`, `backup_create`, `backup_drill`, `install_app`, `install_uninstall`, `watch_add`, `watch_remove`, `watch_check`, `proxmox_guest_start`, `proxmox_guest_reboot` |
| **destructive, reversible** | a token and `confirm: true` | the service comes back when it is started again. `docker_stop`, `proxmox_guest_shutdown` |
| **destructive, not reversible** | a token and `confirm_name` equal to the target | the data is gone, and a click cannot say which thing the operator meant to lose. `backup_restore`, `install_purge` |

The ten in the first tier are there on purpose. Asking twice for something that
is undone by doing something else buys no safety and teaches people to click
through confirmations — and the two tiers under it depend on a confirmation
still meaning something when one appears.

A route whose tier requires anything is not registered at all without a token,
so reaching it unauthenticated gets a 404 rather than a 401: a surface that
answers is a surface to be reached the moment the check is got wrong.

### A route that takes a target can find one

**An endpoint that takes an identifier exists alongside an endpoint that
produces one.** Otherwise a caller has to get that value from somewhere other
than this API, and for anyone who is not our own dashboard there is nowhere
else — which makes it half an API rather than a small gap.

This was found by building the screens. `POST /api/backup/restore` took an
archive name and there was no route that returned one, and `POST
/api/install/{app}/purge` took an app name and there was no route that listed
apps. Both were reachable, correct and uncallable by anyone who had not
already been told the answer, and they were about to be frozen that way.

`backup_list`, `install_list`, `install_status` and `proxmox_guests` have
routes now. Each route that acts on a named thing declares where that name
comes from — `capability.HTTP.Target` — and a test fails when the source is
not a read this API exposes.

**`proxmox_guests` is in that list and was not one of the blocked ones.** It
was added saying the guest actions had no source for their `vmid`, `node` and
`type`, and that was wrong: `proxmox_status` returns `resources.guests`, with
all three on every entry, and the dashboard's Proxmox card had been rendering
that table since before the actions existed. By the rule above it belonged
with `proxmox_node` and `proxmox_tasks` — an extra view, not a prerequisite.
The route stays, because it is the filtered one the guest screen reads and
undoing a published route costs more than it saves, but the reason recorded
for it was not true and the correction belongs where the claim was made.

The other thirteen absences stay absent, and the line is not "does it have a
sibling" but **can the write next to it be used without this read**.
`docker_list` already names containers, so `docker_logs` and `docker_inspect`
are extra views rather than prerequisites; `proxmox_status` already answers for
an endpoint; `network_scan`, `inventory_scan`, `inventory_export` and
`config_validate` have no write beside them at all.

The usual argument that adding a route later is additive, and so allowed after
1.0, does not apply here. That argument is about a capability nobody is
depending on yet. These four had writes shipped against them.

## Not frozen

| | |
| --- | --- |
| **Tool and flag descriptions** | Prose is prose. It gets better |
| **Terminal output** | Layout, colour, wording, emoji. `--json` is the interface for anything that parses |
| **Log lines** | Including what `serve` prints |
| **Order within a list** | Unless a field says otherwise. Sort what you depend on |
| **The dashboard's HTML, CSS and bundle** | The routes it calls are frozen; what it renders with them is not |
| **Unknown config keys** | They are ignored with a warning today. How that warning reads may change |
| **Anything undocumented** | A field nobody wrote down is not a contract. If you depend on one, open an issue and it can become one |

## Still allowed after 1.0

Additive change. A new tool, a new optional field, a new config key, a new
route, a new `kind` — with a changelog line, and provided nothing listed above
changes meaning. A caller that ignores fields it does not recognise keeps
working, which is what "additive" has to mean to be worth anything.

## When something frozen has to change

Short enough to follow:

1. **Add the replacement first.** Both work at once.
2. **Say the old one is deprecated** — in the changelog under ⚠️ Behavior
   changes, and in the description of the thing itself where there is one.
3. **Keep both for at least one minor release.**
4. **Remove in the next major.** Not before.

A rename is a removal and an addition, so it takes the same path.

## The golden file

```bash
go test ./internal/contract              # fails if the surface moved
go test ./internal/contract -update      # regenerate, then commit the diff
```

The diff is the point. A renamed tool or a retyped field shows up in a pull
request next to the changelog line explaining it, rather than in somebody's
integration four months later.

A line reads `name:type`, with two marks for the two ways a value can fail to
be there: `name?` is a key that may be absent, `?type` is a value that may be
`null`. They are different things to a caller, so they are different things
here.

## What the golden file does not check

A mechanism running is not the same as a mechanism being enough, and the ways
this one has been wrong are worth writing down rather than discovering twice.

- **Descriptions.** The `category` test reads the README table for its
  *words*, and nothing reads the column next to them. That column claimed
  `doctor` reported backups "never drilled" — a finding that does not exist —
  for as long as it took someone to check. Generating the prose from the code
  would close it and also make the document a copy of the code, which is the
  opposite of checking a promise against it, so these are caught one at a time.
- **Values.** The golden records that `kind` is a string, not that it is one of
  eight words. The vocabularies are frozen in the table above and tested
  separately; the golden is about shape.
- **Meaning.** A field that keeps its name and type and starts meaning
  something else passes. `running_count` returning `0` for a machine whose
  containers were never counted was a change of meaning inside an unchanged
  `int`, and only a person reading the output found it.
- **Behaviour at the edges.** Exit codes are in the table above for `doctor`
  only. `backup drill` exited `0` on a failed drill until somebody ran one.
- **Classified, not classified correctly.** The test that walks every command
  `doctor` prints checks that each one resolves to a runner and a tool. It
  cannot check that the tool is the right one: `homebutler backup drill --all`
  resolved to `backup_create` for as long as nothing printed it, because
  telling `backup drill` from `backup` with an argument needs the command tree
  and that is not in the package doing the classifying.

- **An identifier the route takes in its body.** `capability.HTTP.Target` says
  where a route's target comes from, and a test fails when that source is not
  a read this API exposes. Only half of it is mechanical: a `{param}` in a
  path is visible, so forgetting to declare one is caught, but `backup_restore`
  takes its archive name in the body and no walk over the registry can see
  that. It was the worst instance of the gap — there was no route at all that
  returned an archive name — and a rule that only read paths would have
  stepped over it. The declaration is what catches it, and a declaration is
  something a person remembers to write.
- **Whatever was true when it was regenerated.** `-update` writes the current
  surface, not the correct one. A defect present at that moment becomes the
  golden, and every run afterwards defends it — the check stops being a
  question and becomes a record of an answer nobody read. The same shape
  outside this file, on the same day: the screenshot on the landing page and
  the copy in `assets/` were byte-identical and the comparison passed, and
  both showed a `doctor` state the product cannot produce. Identical is not
  correct. A published asset gets read all the way through after it is made,
  not only at the one number it was remade for.

Each of these was a real defect, not a hypothetical, and that is the rule this
list keeps: an entry names an accident that happened and what it got past. A
list of things that could go wrong would be longer, and nobody would read a
long one. They are here because knowing what is not guaranteed is worth as
much as the list of what is.

Prose is not checkable, but a claim inside prose often is. The skill says shell
commands are for the things no tool exposes, and it listed `homebutler notify
test`, which is a tool and a dashboard button — so that sentence is now read
against the registry rather than believed. Generating the prose from the code
would close the gap by deleting the promise; extracting the claim keeps the
promise and tests it. That is the route out of the first item above, when
somebody takes it.
