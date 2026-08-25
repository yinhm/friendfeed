[English](README.md) | [简体中文](README.zh-CN.md)

# ffdb

ffdb is a community project I write and maintain for a small group of people who still appreciate FriendFeed.

FriendFeed has been gone for years, but its way of bringing feeds, subscriptions, likes, comments, groups, and external content into one stream remains unusually simple and useful. ffdb is neither a frozen recreation of a 2009 website nor a general-purpose social platform. It preserves FriendFeed's core interaction model while providing a modern implementation that a small, trusted community can operate and maintain over the long term.

This repository is not an official Meta/Facebook project and is not affiliated with the original FriendFeed service.

## 2.0 status

The current `master` branch is in the **2.0 release freeze**. Version 2.0 supports only the current new-DB/Pebble v2 data model. It does not support old databases, Pebble v1, or downgrading a database after it has been written by 2.0. Existing deployments with an old database must migrate it first with the `v1.0.0` tools; see the [database migration guide](docs/db_migration.md).

The goal of 2.0 is not to add more experimental features. It consolidates the capabilities already in real use: stable identities, groups, activity-ranked Home, independent interaction data, RSS/Atom services, persistent tasks, notifications, consistent permissions, and rebuildable derived indexes.

## Comparison with the original FriendFeed

| Capability | ffdb 2.0 | Difference from the original FriendFeed |
| --- | --- | --- |
| User feeds, follow and unfollow | Complete | Preserves the core model; a new follow adds at most 100 recent items to Home |
| Home timeline | Complete | Likes and comments may bump an item within limits; bounded hot/cold caches and cursors replace historical page snapshots |
| Public feed | Complete | Independent, rebuildable activity timeline; private content is excluded on both write and read paths |
| Posts, rich text, links, images, YouTube | Complete | Uses React 19, Plate 53, and strict URL/HTML sanitization; not every historical embed provider is restored |
| Likes and comments | Complete | Canonical data is stored independently; users have private `/likes` and `/comments` history pages |
| Groups | Core features complete | Create, join, leave, administer, manage members, approve private requests, post, attach services, and discover groups; proactive invitations are not implemented |
| Private feeds and groups | Complete | Metadata is public for discovery and requests; content is authorized consistently for owners, members, followers, and superusers |
| Profile rename | Complete | Old IDs soft-redirect; an administrator can reclaim one-time rename records |
| External service aggregation | Partially complete | Supports public RSS, Atom, and JSON Feed; does not recreate the original Flickr, Delicious, and other provider ecosystem |
| Twitter/X | Login compatibility; limited fetching | Keeps legacy OAuth and archive compatibility paths; continued synchronization is not promised after X API changes |
| Notifications | Core features complete | Follow requests, interactions, group roles/membership, and failed-service notifications; no email, Web Push, or notification aggregation |
| Realtime updates | Intentionally simplified | SSE carries dirty hints only; Home offers a refresh and then reads the authoritative first page instead of pushing full entries |
| Search | Complete | Local Bleve index with the same visibility checks used by feeds and permalinks |
| Public API and client ecosystem | Incompatible | Does not recreate the original public FriendFeed API, mobile clients, or third-party application ecosystem |
| Multi-node, large-scale deployment | Not a goal | Designed for a single host and a small community; the main stack is Pebble, ffdb, ffweb, and nginx |

## Features

- Google and Twitter OAuth login, binding local identities to each provider's stable subject ID; first-login onboarding lets users choose a readable profile ID.
- User feeds, Home, Public, Search, tags, permalinks, and cursor pagination.
- Create, edit, and delete posts; likes, comments, rich text, and compatible rendering of historical `rawBody` data.
- Public and private groups, member/admin management, follow requests, discovery, and per-user group activity navigation.
- RSS, Atom, and JSON Feed imports for users and groups, including conditional requests, SSRF protection, source moves, and failure lifecycle management.
- Persistent task queue with leases, epoch fencing, retries, dead history, audits, and operational tools.
- Persistent in-site notifications and SSE dirty hints.
- Local or R2 media storage, historical media URL migration, and Twitter image rescue tools.
- Pebble online consistent backups, database audits, index/timeline rebuilds, and memory-bounded migrations.
- Single-host deployment with systemd, journald, nginx, and Fabric 3.

## Architecture

```text
browser
   │ HTTP / SSE
nginx
   │
ffweb (Gin + templates + React assets)
   │ loopback gRPC
ffdb
   ├── Pebble v2          canonical data + derived indexes
   ├── Bleve              rebuildable search index
   ├── Task workers       Service/timeline/notification maintenance
   └── local media / R2
```

ffdb's gRPC listener must remain on loopback. This trust boundary is what makes the actor/viewer UUID carried by current requests safe; the port must not be exposed directly to the public network. The project targets reliable single-host operation and does not introduce Redis, Kafka, or a distributed-consensus layer in 2.0.

## Development

Requirements: Go 1.26 (follow `go.mod`), Node.js 24 (the exact version is in `.nvmrc`), Corepack, and the repository-pinned pnpm version. `uv` is used only for Fabric deployment and Python tooling.

Build the frontend before Go. Production Go binaries embed `httpd/static/` and the templates:

```bash
cd httpd/app
corepack enable pnpm
pnpm install --frozen-lockfile
pnpm run build

cd ../..
go build -o ffdb .
go build -o httpd/httpd ./httpd
```

Prepare a configuration file:

```bash
cp conf/example.config.json conf/config.json
```

At minimum, ensure that `address` is a loopback address such as `127.0.0.1:8901`, that `db_path` and `media_path` are writable by the service account, that ffweb's `-rpc` matches `address`, and that OAuth callback URLs match the real HTTPS domain. Without R2 configuration, media is stored locally only.

```bash
./ffdb -c conf/config.json -d
./httpd/httpd -c conf/config.json -rpc 127.0.0.1:8901 -p 8080 -s '<cookie-secret>' -d
```

For frontend development, run `pnpm dev` in `httpd/app`. See the [frontend README](httpd/app/README.md) for details.

## Verification

```bash
cd httpd/app
pnpm lint
pnpm run typecheck
CI=true pnpm test
pnpm run build

cd ../..
go build ./...
go vet ./...
go test ./...
```

Run `pnpm run test:e2e` when changing complete feed or editor interactions.

## Deployment

Deployment tasks use Fabric 3:

```bash
uv venv .venv
uv pip install -r requirements.txt
uv run --no-project fab --list
uv run --no-project fab production bootstrap
uv run --no-project fab production deploy_env
uv run --no-project fab production deploy_config
uv run --no-project fab production deploy_nginx
uv run --no-project fab production deploy_db
uv run --no-project fab production deploy_web
```

`deploy_client` remains as a compatibility Fabric task, but it still points to the retired client/Upstart path and must not be used for new deployments. See [open_decisions.md](docs/open_decisions.md). Production services are managed by systemd and write logs only to stdout/stderr:

```bash
systemctl status ffdb.service ffweb.service
journalctl -f -u ffdb.service -u ffweb.service
```

nginx, TLS, SSE buffering, media-site, and systemd templates are under `conf/`. The deployment configuration contains assumptions about the real environment; review its hosts, domains, users, and paths before use, and never commit secrets.

## Backup and recovery

ffdb can create a point-in-time Pebble snapshot while the service is running:

```bash
cli run --t BackupDB
```

Use the loopback CLI to inspect the running ffdb process's memory and background state; see [runtime diagnostics](docs/runtime_diagnostics.md) for commands and safety boundaries.

Backups are atomically published under `/tmp/backup-YYYYMMDD-HHMMSS`. Because the operating system may clean `/tmp`, immediately copy the result to another disk or host. To restore, stop ffdb, preserve the original database directory, place the complete backup at the configured `db_path`, and restart. See the [database migration guide](docs/db_migration.md) for schema, migration, and audit operations.

## Documentation

- [Database design](docs/database_design.md) / [migration and operations](docs/db_migration.md)
- [Home and Public timelines](docs/timeline.md) / [feeds and interaction history](docs/feed.md)
- [Groups](docs/group.md) / [group discovery](docs/group_discovery.md) / [group navigation](docs/group_navigation.md)
- [Service aggregation](docs/service_aggregation.md) / [task queue](docs/task_queue.md)
- [Notifications](docs/notifications.md) / [realtime SSE](docs/realtime_sse.md)
- [Permissions](docs/perm.md) / [OAuth identity](docs/oauth_identity.md) / [profile rename](docs/profile_rename.md)
- [Theme and frontend styles](docs/theme.md) / [health checks](docs/healthcheck.md)
- [2.0 release notes](docs/release_2.0.md) / [open architecture decisions](docs/open_decisions.md)

Design documents describe current persistence and behavioral contracts, not a feature wish list. Work that is genuinely undecided is tracked only in `docs/open_decisions.md`.
