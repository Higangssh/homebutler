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
