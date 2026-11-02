# saringan

**A config-driven data censoring proxy for AI agents.**

`saringan` (Malay: *filter*) sits between an AI agent — or any developer
tooling — and your data. It pulls data from integrations you define (files,
HTTP APIs, SQL databases, environment variables, stdin) and censors it
according to declarative rules **before it ever reaches the agent**.

```
                 ┌──────────────────────────────┐
  data sources → │  saringan: fetch + censor    │ → safe output → AI agent
  (file, http,   │  (rules from saringan.yaml)  │
   sql, env)     └──────────────────────────────┘
```

## Why

LLMs are great at reasoning over data — but your data contains SSNs, credit
cards, passwords and API keys. Rather than trusting a prompt ("please don't
leak secrets"), enforce redaction in infrastructure:

- **Fail-closed by design** — policies can `deny` on any fetch error.
- **Deterministic** — the same config always produces the same output.
- **Auditable** — `--stats` prints exactly which rules fired and how often.

## Install

```sh
go install github.com/bilhasry/saringan/cmd/saringan@latest
# or build from source
make build      # → bin/saringan
```

## Quick start

```sh
saringan init                  # create a starter saringan.yaml
saringan check                 # validate the config
saringan run --policy agent-safe --stats
```

Try the bundled example:

```sh
cd examples
../bin/saringan run --policy agent-safe --source customers-file --stats
```

```json
{
  "customers": [
    {
      "card": "***************1111",
      "credentials": { "password": null },
      "email": "sha256:b5fc85e55755f9e0",
      "ssn": "*hidden*",
      "notes": "[note hidden]"
    }
  ]
}
--- censor report (policy: agent-safe) ---
  drop-api-keys   2 hit(s)
  hash-emails     2 hit(s)
  hide-notes      2 hit(s)
  ...
```

## Configuration

Everything lives in one human-friendly YAML file. `${ENV_VAR}` placeholders
are expanded at load time; unset variables stay literal so the config remains
valid before the environment is provisioned.

```yaml
version: 1

sources:                       # data integrations
  customers-file:
    type: file
    path: ./data/customers.json
    format: json               # json | yaml | csv | text
  customers-api:
    type: http
    url: https://api.example.com/v1/customers
    headers: { Accept: application/json }
    auth: { type: bearer, token: ${API_TOKEN} }
  crm:
    type: postgres             # or mysql
    dsn: ${DB_DSN}
    query: "select * from customers limit 100"
  secrets:
    type: env
    keys: [API_TOKEN, DB_DSN]
  raw:
    type: stdin

rules:                         # censor rules
  - name: hide-ssn
    field: "*ssn*"             # glob on field names, case-insensitive
    action: replace
    with: "*hidden*"           # any replacement string you like

  - name: mask-cards
    pattern: '\b(?:\d[ -]?){13,19}\b'   # regex over string values
    action: mask
    keep: 4                    # keep last 4 digits: ****1111

  - name: hide-passwords
    field: password
    action: redact             # → null

  - name: hash-emails
    pattern: '[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}'
    action: hash               # → sha256:… (joinable, not reversible)

  - name: drop-api-keys
    field: "*api_key*"
    action: drop               # key removed entirely

  - name: hide-notes
    path: "*.notes"            # dot path with * wildcards
    action: replace
    with: "[note hidden]"

policies:                      # bundles handed to consumers
  agent-safe:
    sources: [customers-file, customers-api]
    rules: [hide-ssn, mask-cards, hide-passwords, hash-emails]
    on_error: deny             # deny = fail-closed, allow = fail-open
    format: json
```

### Rule selectors

| Selector  | Meaning                                                      |
| --------- | ------------------------------------------------------------ |
| `field`   | Glob on the field/key name: `"ssn"`, `"*token*"`, `"card_*"` |
| `path`    | Dot path with wildcards: `"user.credentials.password"`, `"users.*.token"` |
| `pattern` | Regex on string values. Alone → applies everywhere; with `field`/`path` → refines that scope |

A rule needs at least one selector. `field`/`path` matching is
case-insensitive unless `case_sensitive: true`.

### Actions

| Action     | Effect                                                      | Example                     |
| ---------- | ----------------------------------------------------------- | --------------------------- |
| `replace`  | Swap value (or matched spans) for `with`, default `*hidden*` | `*hidden*`, `[redacted]`    |
| `mask`     | Mask characters, optionally keeping edges                   | `************1111` (`keep: 4`, `keep_left: 2`) |
| `redact`   | Set the value to `null`                                     | `"password": null`          |
| `hash`     | SHA-256 prefix — stable but not reversible                  | `sha256:b5fc85e55755f9e0`   |
| `truncate` | Keep the first `keep` characters                            | `very long…`                |
| `drop`     | Remove the key from the output structure                    | key gone                    |

`replace` also supports capture groups: `with: "SSN-***-**-$3"`.

## CLI

Designed for both humans and AI agents — stable flags, `--json` everywhere.

```
saringan init                            create a starter config
saringan check [--json]                  validate config, report warnings
saringan list [rules|sources|policies]   inspect the configuration

saringan rule add --name hide-ssn --field "*ssn*" --action replace --with "*hidden*"
saringan rule rm <name>                  delete a rule (and its policy refs)
saringan rule show|enable|disable <name> inspect / toggle
saringan rule test <name> <text>         dry-run one rule against sample text

saringan source add --name orders --type http --url https://… --bearer ${TOKEN}
saringan source rm|show <name>
saringan source test <name>              fetch and preview (uncensored)

saringan policy add --name pci --sources db --rules mask-cards --on-error deny
saringan policy rm|show <name>

saringan run   --policy agent-safe [--source X] [--stats] [--out f]
saringan filter --policy agent-safe [--in f] [--stats] < input
saringan scan  --policy agent-safe [--in f]        dry-run: show matches only
```

### Piping into an agent

```sh
curl -s https://api.example.com/report \
  | saringan filter --policy agent-safe \
  | your-agent
```

### Per-rule statistics

`saringan run --stats` (or `filter --stats`) prints a censor report to
stderr so you can verify coverage:

```
--- censor report (policy: agent-safe) ---
  hide-ssn               2 hit(s)
  mask-cards             2 hit(s)
```

## Data integrations

| Type       | Key fields                              | Notes                                   |
| ---------- | --------------------------------------- | --------------------------------------- |
| `file`     | `path`, `format`                        | json, yaml, csv (header row → maps), text |
| `http`     | `url`, `method`, `headers`, `body`, `auth`, `timeout` | `auth.type`: basic, bearer, header |
| `postgres` | `dsn`, `query`, `timeout`               | any `database/sql` DSN                  |
| `mysql`    | `dsn`, `query`, `timeout`               |                                         |
| `env`      | `keys` (list, `"*"` = all)              | unset keys → `null`                     |
| `stdin`    | —                                       | auto-detects JSON                       |

CSV input becomes a list of row objects, so `field` rules work on column
names. SQL results become row objects keyed by column name.

## Exit codes & failure modes

- Config invalid or source unavailable with `on_error: deny` → exit 1,
  nothing printed (fail-closed).
- `on_error: allow` → failing source is skipped, warning on stderr,
  remaining sources still censored (fail-open).

## Development

```sh
make test      # go test ./...
make build     # bin/saringan
make demo      # run the example config end to end
```

## License

MIT
