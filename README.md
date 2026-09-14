# viam-crestron

Go tooling and (in progress) a Viam module to control lights on a Crestron Home
processor over its `/cws/api` REST interface.

Target under development: **CP4-R at 192.168.3.2**, Crestron Home
v2.8000.00056, REST API v2.000.0004.

## Layout

- `crestron/` — REST API client
  - `client.go` — token -> session-key auth, transparent re-login, `Raw()`
  - `lights.go` — `Rooms`, `Lights`, `Light(id)`, `SetLevel/SetPercent/TurnOn/TurnOff`
  - `config.go` — offline discovery by parsing `LightingSystem.cfg` + `SystemConfig.cfg`
- `cmd/crestron-cli` — driver / explorer (`lights`, `rooms`, `discover`, `on/off/set`, `raw`)
- `cmd/crestron-console` — run console commands over SSH
- `cmd/crestron-fetch` — pull config files off the processor over SFTP
- `docs/lights-inventory.txt` — the 111 discovered loads

## Credentials

Two separate things:

- **SSH / SFTP console** — the processor's admin username + password. Used by
  `crestron-console` and `crestron-fetch`. Set `CRESTRON_USER` / `CRESTRON_PASS`.
- **Web API token** — a separate token for the REST API, generated in the
  Crestron Home Setup app under *Installer Settings > System Control Options >
  Web API*. Used by `crestron-cli` live commands. Set `CRESTRON_TOKEN`.
  The token stored in `SystemConfig.cfg` is a one-way hash and cannot be replayed.

## Discovering lights without the token

```sh
export CRESTRON_USER=... CRESTRON_PASS=...
go run ./cmd/crestron-fetch -out ./cfg \
  /user/Data/Configuration/LightingSystem.cfg \
  /user/Data/Configuration/SystemConfig.cfg
go run ./cmd/crestron-cli discover ./cfg/LightingSystem.cfg ./cfg/SystemConfig.cfg
```

## Controlling lights (needs the Web API token)

```sh
export CRESTRON_TOKEN=...
go run ./cmd/crestron-cli lights          # live list with current levels
go run ./cmd/crestron-cli set 52062 40    # Kitchen Downlights to 40%
go run ./cmd/crestron-cli off 52062
```

## Viam module

The module exposes three models:

| Model | API | Purpose |
|-------|-----|---------|
| `erh:crestron:light` | `rdk:component:switch` | One light load. On/off loads are 2-position (Off/On); dimmable loads are 6-position (0/20/40/60/80/100%), with position labels. |
| `erh:crestron:scene` | `rdk:component:button` | One scene. `Push` recalls it. |
| `erh:crestron:discovery` | `rdk:service:discovery` | Discovers every light (switch) and scene (button) as ready-to-add component configs. |

Build with the Makefile (matches the standard Viam module layout):

```sh
make module.tar.gz   # test, build bin/crestron-module, strip, tar
make test            # go test ./...
make setup           # go mod tidy
```

`meta.json` uses `build: make module.tar.gz` / `path: module.tar.gz` and builds
for `linux/amd64`, `linux/arm64`, and `darwin/arm64`, so `viam module build` /
`reload` produce the tarball automatically. The entrypoint is `bin/crestron-module`.
The Web API token is resolved from `CRESTRON_ENV` (set in the module's `env`,
pointing at a `KEY=value` file) or `CRESTRON_TOKEN`.

Credentials: every model reads `host` / `token` from its config, falling back to
`CRESTRON_HOST` / `CRESTRON_TOKEN` (env or `~/.crestron.env`). `host` defaults to
`192.168.3.2`.

### Light switch config

```json
{
  "name": "kitchen-downlights",
  "api": "rdk:component:switch",
  "model": "erh:crestron:light",
  "attributes": { "id": 52062, "dimmable": true }
}
```

`dimmable` selects the default positions (six steps vs Off/On). Optional
`steps` overrides the percentages, e.g. `"steps": [0, 25, 50, 75, 100]`;
optional `fade` sets the transition time in tenths of a second.

### Scene button config

```json
{
  "name": "stairwell-all-on",
  "api": "rdk:component:button",
  "model": "erh:crestron:scene",
  "attributes": { "id": 52197 }
}
```

### Discovery service

Add the service, then run discovery from the Viam app (or SDK) to get a config
per light and scene:

```json
{ "name": "crestron-discovery", "api": "rdk:service:discovery", "model": "erh:crestron:discovery", "attributes": {} }
```

## Bulk config generator

`crestron-genconfig` prints a `{"components": [...]}` fragment for every light and
scene, named by room, to paste into a machine config:

```sh
go run ./cmd/crestron-genconfig > components.json          # creds from machine env
go run ./cmd/crestron-genconfig -embed-token > components.json  # hard-code host+token
go run ./cmd/crestron-genconfig -no-scenes                 # lights only
```

### Push directly to a machine

With a Viam API key and the machine part ID, `-push` writes the components
straight into the machine config via the app API. It is idempotent: it replaces
any previously-pushed crestron components and leaves everything else alone.

```sh
go run ./cmd/crestron-genconfig -push -embed-token \
  -api-key-id <VIAM_API_KEY_ID> -api-key <VIAM_API_KEY> -part <MACHINE_PART_ID>
```

Creds may also come from `VIAM_API_KEY_ID` / `VIAM_API_KEY` / `VIAM_PART_ID`.
Use `-embed-token` when pushing so the components carry `host`+`token`, unless
the machine's module env already provides `CRESTRON_TOKEN`. The machine must
have the crestron module configured for the pushed models to resolve.

Folders: each resource gets a top-level `"ui_folder": {"name": "<room>"}` field,
which groups it in the app's Resources sidebar. Lights and scenes go in their
room's folder; the controller and discovery service go in a `Crestron` folder.
(viam-server ignores `ui_folder`; it is purely an app-UI grouping.)

## Releasing

`.github/workflows/deploy.yml` builds and publishes to the Viam registry on any
tag push (via [viamrobotics/build-action](https://github.com/viamrobotics/build-action),
using `meta.json`'s `build`/`setup`/`arch`). Add repo secrets `viam_key_id` and
`viam_key_value` (an org API key), then:

```sh
git tag 0.0.1 && git push origin 0.0.1
```
