---
name: api-recon
description: Discover website API endpoints, frontend routes, parameters, and UI request triggers during an authorized assessment.
---

# API Recon

Discover backend API paths/methods/parameters/responses, frontend routes, and UI
triggers such as tabs, dialogs, and table actions. Stay inside the independently
approved assessment scope. This skill performs reconnaissance, not vulnerability
exploitation.

## Boundaries

| Area | Permitted | Prohibited |
| --- | --- | --- |
| Goal | Enumerate paths, methods, parameters, routes, and UI triggers | SQLi/XSS/access-control testing, brute force, vulnerability fuzzing, attack request modification, destructive operations |
| Authentication | Hook and stub/mock client-side login gates | Requesting or guessing credentials, submitting actual login forms, OTP/OAuth authentication |
| Runtime | Mock bootstrap responses to render the authenticated SPA shell without backend credentials | Workflows that require a real backend session |

Default runtime analysis is credential-free:

1. Intercept/stub login, permission, menu, and other bootstrap APIs through
   `preload.js` / `runtime_harvest.js`.
2. Return correctly shaped success bodies for business queries; empty data is fine.
3. Render the shell despite unavailable backends or 401 responses, observing which
   XHR/fetch/WebSocket requests the frontend would issue.
4. Empty tables, placeholder UI, and empty data are expected. Do not switch to
   real login or vulnerability testing to fill them.

Use mocks to mount routes/components and record outbound requests. Mock success
must never be presented as a real authenticated backend response or vulnerability.
Browser hooks are observation aids, not a network sandbox; independently prevent
unapproved or state-changing operations from reaching real systems.

| Do not | Instead |
| --- | --- |
| Extract API paths from the main `index-*.js` with grep/curl/Read before Phase 1 finishes | Run the reviewed `OUTDIR/harvest_static.py` copy |
| Write a replacement `extract_apis.py` instead of the harvest workflow | Adapt the OUTDIR harvest script and rerun |
| Repeat the same failing command twice or more | Inspect tool logs/reference guidance and change approach |
| Skip gates A/B and run bundled templates unchanged | Copy into OUTDIR and adapt to the target |
| Use real passwords, OTP, OAuth, or login submissions | Stub/mock client-side state |
| Perform access-control/injection tests to obtain real data | Record outbound requests only |
| Delete data, export sensitive data, or perform bulk writes | Skip irreversible actions, including coverage clicks |
| Claim complete coverage without runtime/dynamic enumeration | Complete the checklist or report limitations |
| Infer all parameters without the trigger matrix and sample diff | Complete Phase 3b and Phase 5 |
| Infer required/optional fields from one runtime sample | Use multiple samples or explicit validation/error evidence |

## Static and runtime models

| Layer | Output | Limitations |
| --- | --- | --- |
| Static bundles | Endpoint paths, route candidates, payload-field candidates | Methods may be unknown; fields need Phase 1b; dynamically assembled URLs can be missed |
| Runtime | Methods, bodies, responses, dynamic URLs, WS/SSE, sample differences | Pages must render to issue requests; one sample cannot establish required fields |

| Mode | Engine | Use |
| --- | --- | --- |
| `depth` | Puppeteer `runtime_harvest.js` | Reproducible API/method/body/response and WS/SSE capture |
| `coverage` | Browser with `preload.js` | Deeper tabs, dialogs, tables, and feature triggers |
| `both` | Depth, then coverage | Broadest observation at higher time cost |

Parameter discovery combines anchored context windows, UI binding chains, sample
differences, and validation errors. There is no universal parameter extraction
script. See [reference.md](reference.md), section J.

## Completion criteria

- [ ] Static harvest produced `api_static.txt`, `routes.txt`, and `js/`.
- [ ] At least one runtime mode completed; coverage/both include working hooks and
      a dynamic enumeration loop.
- [ ] Business paths actually render the shell without returning to `/login`,
      accounting for hash routing.
- [ ] Coverage/both include a parameter trigger matrix and `param_samples.json`;
      Phase 5 produces `params_merged.json`.
- [ ] If modules are blank, Phase 4 reconstructs permissions and runtime is rerun
      until module APIs appear, beyond locale/bootstrap calls.
- [ ] Phase 5 artifacts are complete and all discovered in-scope services/endpoints
      are registered with `insert_assets`.

## Script gates and output limits

Bundled `scripts/` are reference templates. Read them, copy/adapt into OUTDIR
(such as `recon/`), and record changes in `CHANGES.md`. If a template does not fit,
rewrite the relevant logic using its structure rather than claiming its output is
complete.

| Gate | When | OUTDIR copies | Check/adapt |
| --- | --- | --- | --- |
| A: static | After Phase 0, before first harvest/spider | `harvest_static.py`, `spider_mpa.py` | Defaults fit many sites; change endpoint regex, manifest dialect, webpack/Vite publicPath, MPA exclusions/cookies only when necessary |
| B: runtime | After Phase 2, before depth/coverage | `runtime_harvest.js`, `runtime_policy.js`, `preload.js`, `config.json`, package/lock files | Cookie/storage keys, success values, stubs, login patterns, API prefixes, hash/history routing |

For SPAs, after classification the next extraction command is the OUTDIR harvest,
not manual main-bundle parsing. Copy, adapt if needed, run, and check counts.
Retry 404/manifest problems through the harvest script. From Phase 1b onward,
search only downloaded `OUTDIR/js/*.js`. For MPAs, use the copied spider instead.

- Do not read/grep `index-*.js` larger than 100 KB into model context; batch it
  through OUTDIR scripts.
- Bound grep output with `| head -20` or `-m 5`; keep path summaries, not bundle dumps.
- Verify with `wc -l` and directory file counts, not full-directory reads.
- At most one optional initial regex probe, only on HTML or a chunk at most 50 KB.
- Consult the reference for recipes/configuration/troubleshooting without copying
  the entire reference into a conversation.

## Execution checklist

Follow phases in order; do not advance past unfinished prerequisites.

1. [Phase 0: Classification](#phase-0-classification): identify SPA/MPA; create OUTDIR.
2. Gate A and [Phase 1: Static](#phase-1-static): copy, run harvest, check counts.
3. [Phase 1b: Parameters](#phase-1b-parameters): anchored windows and UI bindings;
   write `param_candidates.json`.
4. [Phase 2: Client gates](#phase-2-client-gates): document all three gates in config.
5. Gate B and [Phase 3: Runtime](#phase-3-runtime): adapt scripts; run depth/coverage.
6. Complete the parameter trigger matrix and sample capture.
7. [Phase 4: Permission tree](#phase-4-permission-tree), if needed; patch stubs and rerun.
8. [Phase 5: Merge and report](#phase-5-merge-and-report): merge evidence and register assets.

## Phase 0: Classification

Fetch entry HTML and create OUTDIR; do not edit the bundled template directory.
A shell with `<div id=app>` and lazy chunks indicates an SPA. SSR forms without
endpoint bundles suggest an MPA:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Use only approved synthetic test state; do not obtain real credentials for this
workflow. The spider produces `forms.txt`, `links.txt`, and `api_inline.txt`.
If forms are nearly absent and the site is an SPA, switch to static harvest.

## Phase 1: Static

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

Harvest reads HTML scripts and webpack/Vite manifests, downloads lazy chunks, and
produces `js/`, `api_static.txt`, `routes.txt`, and `chunkmap.txt`. Compare counts
against the manifest. Fix 404s in the adapted harvest rather than manually fetching
every chunk. Too few endpoints may require a broader endpoint regex and rerun.

### Phase 1b: Parameters

For important endpoints, establish field name, transport location, inferred type,
required/optional status, sample value, and confidence.

| Transport | Location | Static evidence |
| --- | --- | --- |
| REST JSON | Body/query | Payload object near path anchor |
| GraphQL | `variables` | gql templates and variable declarations |
| Forms | URL-encoded | `<form>`, `FormData` |
| Uploads | Multipart | `FormData.append` |
| Path parameters | `/user/:id` | Routes, `useParams`, `$route.params` |
| Signed/encrypted | Wrapped `sign`/`data` | Encryption-function inputs; reference D |

Label transport `query|json|form|graphql|encrypted`. Expand context around known
paths to locate request construction:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

Inspect axios data/params, shared request interceptors, generated OpenAPI method
signatures, React Query/SWR arguments, and Vue composable inputs. Residual yup/zod
rules, `Form.Item name=`, or embedded Swagger can expose field constraints.
Record `{path, fields[], source:"static-callsite", confidence}` in
`param_candidates.json`.

Follow `form field -> submit handler -> transform -> API payload`. Table search
can use `getFieldsValue()`, routes supply IDs/query tabs, interceptors inject tenant
or pagination/signature fields, and select options expose enum values. Trace the
DevTools stack upward from fetch/XHR.send.

Answer three payload questions: where is it assembled/transformed, how is it
validated (required/pattern/enum), and where is it sent (path/query/body/multipart/
headers)? These differ from the authentication gates below. Required, optional,
and conditional status still needs the runtime matrix, sample diff, and Phase 5
validation-error evidence.

## Phase 2: Client gates

Search downloaded chunks with bounded output and record in `config.json`:

| Gate | Question | Clues |
| --- | --- | --- |
| Render | How is client login state checked? | isLogin, getToken, cookie/storage |
| Interceptor | What redirects to `/login`? | response_code, errno, axios interceptors |
| Content | Where do menu/permission entries come from? | menu, permission, role, acl, routes |

Storage-key names are not credentials. Confirm their role through code and request
chains. Document global Authorization, `X-Tenant-Id`, and signing fields without
using actual secrets. Gate B applies these conclusions to the OUTDIR runtime scripts.

Optional observation settings: `recordDetail:true` exposes `__API_RECON_DETAIL__`;
`observe.xhrHeaders:true` records header use; `extractUrlsFromResponse:true` finds
nested API URLs; `observe.storageReads/cookieReads:true` informs configuration;
`neutralizeVueRouter:true` exposes `__API_RECON_ROUTES__`. Export
`__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, and
`__API_RECON_OBSERVE__` after each coverage round.

## Runtime security defaults

The depth helper requires Node 22.12 or later and its pinned package lock. Copy
`runtime_policy.js` together with the runtime script and package metadata.
API forwarding defaults to false. Explicit `forward:true` permits only same-origin
GET/HEAD, refuses redirects, and bounds response bytes/time. Other methods remain
mocked. Additional `allowedAssetOrigins` may load static resources, never APIs.
Captures have request/frame/route limits and private file permissions. If a proxy
is configured, Node API forwarding refuses to run rather than silently bypassing
it; keep mock mode enabled. Browser/static traffic uses Chromium's configured proxy.

TLS verification and the Chromium sandbox are enabled by default. Install a
trusted assessment CA for private targets. `insecureTLS:true` affects Chromium
only; Node read-forwarding still verifies TLS. `noSandbox:true` is an explicit
exception only inside a disposable assessment VM, never a shared workstation.
The browser must have a writable private HOME/cache directory.

Browser instrumentation is not a full network sandbox: WebSockets, native page
features, or modified preload hooks still require external egress restrictions.
The optional forwarding mode is for independently approved requests; a GET can
have application-specific side effects despite its HTTP method. Never use real
credentials with the mock reconnaissance workflow.

## Phase 3: Runtime

Complete gate B first and keep the credential-free mock boundary.
Set `runtimeMode` to `depth`, `coverage`, or `both`.

| Layer | Coverage | Purpose |
| --- | --- | --- |
| L1 exact stubs | Auth/permission/bootstrap endpoints | Render the initial shell |
| L2 negative-response rewrite | JSON responses | Map unauthorized business codes to mock success |
| L3 fallback | Unmatched configured API paths | Empty success bodies to mount UI |

Depth uses fake client state, configured forwarding/response rewriting, and stubs
while visiting hash/history routes, producing `runtime_api.json`. Forwarding is
not harmless by itself: do not forward prohibited operations to a real backend.
Coverage injects preload at document start through CDP
`addScriptToEvaluateOnNewDocument` or a userscript. Verify
`window.__API_RECON_PRELOAD__` exists and business paths do not return to `/login`.
Copy the required package metadata into OUTDIR and review dependencies before
running the adapted runtime script:

```bash
cd recon
npm ci --ignore-scripts
node runtime_harvest.js config.json
```

### Phase 3b: Dynamic coverage and parameter matrix

Visit each main/side navigation entry and wait 1-3 seconds for requests. Exercise
tabs, safe first-row view/edit/detail dialogs, filters, and new-item forms without
committing changes. Do not perform deletion, sensitive exports, or irreversible
writes. Merge APIs/routes per module. For SPAs only, use controlled pushState for
unvisited routes; do not apply that technique to MPAs.

Record samples for initial lists (pagination/default filters), search
(keywords/filters), advanced filters (optional fields), new/edit forms (entity
shape), and safe batch/export/sort controls (ids/exportType/sortField). Observed
outbound bodies and headers retain real structure even with mock responses; use
them as evidence rather than mock body contents. Produce `scan_raw.json`,
`param_samples.json`, and `api_detail.json`, then compare multiple samples.

Vue requires router neutralization and document-start preload. React combines
routes, side navigation and pushState. `both` runs depth before coverage.

## Phase 4: Permission tree

Blank module pages or identical sparse bootstrap-only requests indicate an
unresolved content gate. Rendering the shell proves only render/interceptor gates.
Missing navigation can mean incomplete stub shape/codes; sparse route lists may
require extraction from auth modules.

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

A common chain is flat `role_permissions` plus a `permissions/all` tree through
`getResultTree` to `userRouteAuth[CODE].url`. Artifacts include `route_map.json`,
`userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, and
`perm_codes_all.txt`. Match outer response_code to the interceptor gate, align flat
codes/tree, and include every route-map link. Update config and rerun Phase 3.
Large SPAs may need waitUntil/routeTimeout/perRouteMs tuning; see reference A3/I.

## Phase 5: Merge and report

| Artifact | Phase | Content |
| --- | --- | --- |
| js/, api_static.txt, routes.txt, chunkmap.txt | 1 | Bundles and paths |
| param_candidates.json | 1b | Static field candidates |
| config.json | 2 | Client gates and runtime settings |
| runtime_api.json | 3a | Detailed depth capture, including WS/SSE |
| param_samples.json, scan_raw.json, api_detail.json | 3b | Samples, click log, request details |
| route_map.json and permission artifacts | 4 | Permission reconstruction, if used |
| params_merged.json | 5 | Merged fields and confidence |
| api_merged.txt | 5 | METHOD /path [params] [static/runtime/both] |
| site_map.json | 5 | Routes, APIs, parameters, features, limitations |
| insert_assets | 5 | All discovered in-scope services/endpoints |

Diff samples into merged parameters; there is no universal merge script. Use
reference J7's high/medium/low/pending-trigger confidence criteria. Within approved
scope and without state changes, validation-error responses to incomplete requests
may establish required fields/enums. Consider wrappers such as data, variables,
and pre-encryption bizData. This is parameter reconnaissance, not exploitation.

Report runtimeMode, static/runtime API counts, confidence, uncovered modules, and
CHANGES.md. Suggested site_map fields:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{"method":"POST","path":"/api/user/list","transport":"json","fields":[]}],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

## Limits and reference

The method applies across webpack/Vite/Angular lazy loading and covers REST/JSON,
GraphQL, WebSocket, and SSE, not gRPC-web. Client fetch in SSR is observable;
RSC/Server Actions are not fully enumerable. JSVMP, WASM, strong HMAC/mTLS,
conditional/hidden parameters, or WASM payload construction may remain inaccessible;
label those limitations or pending triggers. Static harvest remains a fallback
when runtime rendering is blocked. Recipes, hooks, configuration, troubleshooting,
parameter methodology, and site-map examples are in [reference.md](reference.md).
