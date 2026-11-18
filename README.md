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
  Crestron Home Setup app (see [Getting a Web API token](#getting-a-web-api-token)). Used by `crestron-cli` live commands. Set `CRESTRON_TOKEN`.
  The token stored in `SystemConfig.cfg` is a one-way hash and cannot be replayed.

## Getting a Web API token

The REST API (and so the Viam module) authenticates with a Web API token that
you generate on the processor. You need installer access to Crestron Home.

1. Open the **Crestron Home Setup** app and connect to the processor.
2. Go to **Installer Settings > System Control Options > Web API Settings**.
3. Turn **Web API** on. The processor generates an authorization token and shows it there.
4. Copy the token. (**Update Token** issues a new one and invalidates the old one.)
5. Put it in the controller's `token` attribute, or set `CRESTRON_TOKEN`.

Check it works:

```sh
CRESTRON_TOKEN=<token> go run ./cmd/crestron-cli lights
```

The token stored in `SystemConfig.cfg` is a one-way hash, so pulling it off the
processor over SFTP won't work; it has to come from the Setup app.

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

The module exposes four models:

| Model | API | Purpose |
|-------|-----|---------|
| `erh:crestron:controller` | `rdk:component:generic` | Shared connection to the processor; every light/scene links to one. |
| `erh:crestron:light` | `rdk:component:switch` | One light load. On/off loads are 2-position (Off/On); dimmable loads are 6-position (0/20/40/60/80/100%), with position labels. |
| `erh:crestron:scene` | `rdk:component:button` | One scene. `Push` recalls it. |
| `erh:crestron:discovery` | `rdk:service:discovery` | Discovers every light (switch) and scene (button) as ready-to-add component configs. |

Each model's config is documented below; the samples are the `attributes`
block. Name the controller `crestron-main` and the other models' `controller`
attribute can be left out.

### Controller config

```json
{
  "host": "192.168.3.2",
  "token": "<crestron web api token>"
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `host` | string | no | Processor address. Defaults to `CRESTRON_HOST`, then `192.168.3.2`. |
| `token` | string | yes* | Web API token; see [Getting a Web API token](https://github.com/erh/viam-crestron#getting-a-web-api-token). *May instead come from `CRESTRON_TOKEN` (env, `CRESTRON_ENV` file, or `~/.crestron.env`). |
| `user` | string | no | SSH/console username (not used for REST control). |
| `password` | string | no | SSH/console password (not used for REST control). |

The controller polls all light levels every 4 seconds, so lights share a single
request no matter how many there are.

### Light switch config

```json
{
  "controller": "crestron-main",
  "id": 52062,
  "dimmable": true
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | int | yes | Crestron load id (see discovery, `crestron-cli lights`, or `docs/lights-inventory.txt`). |
| `controller` | string | no | Name of the `erh:crestron:controller` component. Default `crestron-main`. |
| `dimmable` | bool | no | `true` gives six positions (0/20/40/60/80/100%); `false` gives Off/On. |
| `steps` | int[] | no | Override the percentage for each position, e.g. `[0, 25, 50, 75, 100]`. |
| `fade` | int | no | Transition time in tenths of a second (0 = instant). |

### Scene button config

```json
{
  "controller": "crestron-main",
  "id": 52197
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | int | yes | Crestron scene id. `Push` recalls the scene. |
| `controller` | string | no | Name of the `erh:crestron:controller` component. Default `crestron-main`. |

### Discovery service

```json
{
  "controller": "crestron-main",
  "include_scenes": true
}
```

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| `controller` | string | no | Name of the `erh:crestron:controller` component. Default `crestron-main`; stamped into every discovered config. |
| `include_scenes` | bool | no | Also discover scene buttons. Default `true`. |

Run discovery from the Viam app (or SDK) to get a ready-to-add config for every
light and scene.

### Building

Build with the Makefile (matches the standard Viam module layout):

```sh
make module.tar.gz   # test, build bin/crestron-module, strip, tar
make test            # go test ./...
make setup           # go mod tidy
```

`meta.json` uses `build: make module.tar.gz` / `path: module.tar.gz` and builds
for `linux/amd64`, `linux/arm64`, and `darwin/arm64`, so `viam module build` /
`reload` produce the tarball automatically. The entrypoint is `bin/crestron-module`.

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
