# ReST Bed dashboard UI

The Angular frontend for the dashboard. It talks only to the API's relative
`/api/*` paths, so whatever serves it has to route `/api` to `cmd/server`.

## Develop

```bash
npm ci
npm start          # ng serve on :4200; proxy.conf.json sends /api to localhost:8000
```

Run the API alongside it (`make run` from the repo root).

## Scripts

| Script | What it does |
|---|---|
| `npm run build` | Production build into `dist/ui/browser/` |
| `npm run test:ci` | Unit tests once, no watch |
| `npm run lint` | Development build (type and template checks) |
| `npm run clean` | Remove `dist/` |

## Image

`Dockerfile` builds the app and serves it on `nginx:1.27-alpine`.
`nginx.conf.template` serves the app and proxies `/api/` to `API_UPSTREAM`
(default `api:8000`, the compose service name). It falls back to
`index.html` for app routes, never caches `index.html`, and caches hashed
bundles for good. `compose.yaml` is this service's fragment, included from
the root `compose.yaml`.

Coding rules for this workspace are in `.claude/CLAUDE.md`.
