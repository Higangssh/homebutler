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
| **`failed_collectors`** | The names a collector can be reported under — `docker`, `ports`, `processes`. A count is only trustworthy when its collector is absent from this list |
| **The `kind` vocabulary** | The eight words a change can carry, and what each means |
| **The `runner` vocabulary** | `mcp`, `cli`, `shell` |
| **The `category` vocabulary** | The eleven words a `doctor` finding can carry, documented in the README |
| **`doctor` exit codes** | Including under `--strict` |
| **Documented config keys** | Everything in [configuration.md](configuration.md) keeps its name and meaning |
| **HTTP routes and their protection** | The path, the method, and whether a token is required. A route that quietly stops needing one is the change nobody sees |
| **Which decision an absence waits on** | A capability the dashboard cannot reach records why. The sentence is prose and improves; the decision it names is frozen, so a tool quietly moving from "waiting on a rule" to "never" is a diff. Additive change after 1.0 is allowed — [the registry](../internal/capability/capability.go) is what says how much is still coming |

A field whose value is a sentence — `report --json`'s `status` and `warnings`,
`doctor`'s `title`, `detail` and `action` — is frozen as a field: it keeps its
name and stays an array of strings. **The sentences inside it are not.** They
are written for a person and get better; nothing should be parsing them. Where
a caller needs a value rather than a sentence, the value is a typed field of
its own, and if one is missing that is a bug worth reporting rather than a
reason to split a string.

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

Each of these was a real defect, not a hypothetical. They are listed because
knowing what is not guaranteed is worth as much as the list of what is.

Prose is not checkable, but a claim inside prose often is. The skill says shell
commands are for the things no tool exposes, and it listed `homebutler notify
test`, which is a tool and a dashboard button — so that sentence is now read
against the registry rather than believed. Generating the prose from the code
would close the gap by deleting the promise; extracting the claim keeps the
promise and tests it. That is the route out of the first item above, when
somebody takes it.
