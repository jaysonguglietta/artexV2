> Current hardening: copy runtime_policy.js and package-lock.json with the runtime
> script; use Node >=22.12 and npm ci --ignore-scripts. API reads are mocked by
> default. Opt-in depth forwarding is same-origin GET/HEAD only, without redirects,
> with response/time limits. Keep TLS and Chromium sandboxing enabled. Browser
> hooks do not replace external egress policy. Do not use real credentials.

# api-recon — Reference manual

Search recipes, configuration examples, and troubleshooting for authorized assessments. Search commands assume a `js/` directory. For minified bundles, beautify a copy or use context windows around matches.

## Script reference

All files in `scripts/` are reference templates. Review and adapt them to the assessed site before running them. Keep adapted copies in the task workspace, such as `recon/`, and record changes in the report.

| Script | Site-specific adjustments |
|---|---|
| `harvest_static.py` | Endpoint patterns, webpack/Vite manifests, microfrontend publicPath, retries and concurrency |
| `runtime_harvest.js` | Neutralization fields and success values, stub shapes, routes, WebSocket recording, navigation timeouts and proxy |
| `preload.js` | Login patterns, L1 stubs, API patterns, optional L3 mocking, observations and Vue router handling |
| `spider_mpa.py` | Exclude destructive links, use synthetic cookies, bound depth/count, enforce origin restrictions |
| `extract_route_map.py` | routeMap/routeLink patterns and key naming |
| `build_perm_tree.py` | userRouteAuth parsing, parent heuristics and stub envelope fields |
| `config.json` | Combined site-specific settings |

Browser hooks are instrumentation, not an authorization or network boundary. Use a disposable environment with externally enforced egress restrictions. Keep real credentials out of capture files.

## A. Reverse-engineer the three client-side gates

These gates control client rendering. Mocking them does not establish server-side authorization or prove access to protected data.

### A1. Rendering gate: recognizing a signed-in client

Trace `isLogin = f(getUser())` to `getUser = decode(storage.read(KEY))`. Identify the storage key, cookie/localStorage container, and encoding. Use synthetic values only.

| Client decoder | Synthetic value |
|---|---|
| Plain string or token presence | `anything-truthy` |
| `JSON.parse(x)` | `json:{"id":1,"username":"test-user"}` |
| `JSON.parse(atob(x))` | `b64json:{"id":1,"username":"test-user"}` |
| JWT parsing | A synthetic unsigned token can test client rendering; it is never evidence of valid server authentication |
| SM2/AES/RSA decoding | Inspect the client decoder; fall back to static discovery if a synthetic blob cannot be decoded |

Put synthetic values in `cookies` or `localStorage`. Do not copy production secrets.

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

### A2. Response interceptor gate: login redirects

Identify response field names, success values (often 0 or 200), and failure values that trigger redirects. Search patterns retain original target-language error signatures as literal matching data. A disposable test endpoint can confirm behavior with a junk session. Configure `neutralize.fields` and `neutralize.success`; do not send destructive probe requests.

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
# Literal target-language signatures are intentional matching data.
grep -rhoaE '.{0,40}(未登录|请重新登录|登录已过期|unauthorized|登录失效|授权|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

```bash
curl --fail-with-body --max-time 10 -H 'Cookie: test-session=junk' https://authorized-test.example/api/read-only
```

### A3. Content gate: menus and permissions

Read the consuming code: determine how `getResultTree(tree, permissions)` filters entries and which fields `v-if` or `hasAuth(code)` reads.

| Data source | Typical payload | Consumer |
|---|---|---|
| role_permissions API | permissions array and role_type | Route guards and button ACLs |
| permissions/all API | Tree of code, position, children | Sidebar rendering |
| Bundled userRouteAuth | Code-to-URL map | Frontend paths |
| Bundled routeMap | Key, name, link | Aliases such as webpack o.DASHBOARD |

A small site may need only a synthetic permissive menu in `stubs`. For blank child modules, reconstruct the permission tree using section I. Client-side stubs do not change server permissions.

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

## B. config.json template

The example uses synthetic identity data and `forward: false`. Keep TLS validation and Chromium sandboxing enabled.

- runtimeMode: depth (Puppeteer), coverage (preload instrumentation), or both.
- Cookie values: b64json encodes JSON as base64; json uses raw JSON; no prefix means a literal.
- forward=false mocks recognized API requests. It does not make the browser offline: page resources, navigations and other browser features still need egress enforcement.
- Opt-in depth forwarding permits bounded same-origin GET/HEAD requests only, without redirects. POST and other unsafe methods stay mocked. Node forwarding refuses a configured browser proxy instead of silently bypassing it.
- mockTier selects preload levels such as L1+L2; L3 can return empty fallback arrays.
- routes starts from routes.txt; discovered anchor links may extend it within configured limits.
- captureResponses and recordWs apply to depth mode; captured data needs private storage and retention limits.
- Prefer domcontentloaded for large SPAs; networkidle2 may never settle.
- routeTimeout bounds each navigation in milliseconds.
- proxy configures Chromium. Do not assume Node fetch uses HTTP_PROXY/HTTPS_PROXY or the browser proxy.
- Request, response, route, frame, URL and runtime limits are bounded in runtime_policy.js. Review those limits before increasing them.

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": false,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

### B1. Two-stub example: role permissions and permission tree

Match envelope fields (response_code, code, data) to section A2. Include every leaf code needed by the synthetic permission tree.

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

## C. Coverage mode: preload configuration

Edit the CONFIG object at the top of scripts/preload.js before document-start injection. Confirm `window.__API_RECON_PRELOAD__ === true` and a stable pathname. Export the captured results using the example below; protect the output as potentially sensitive.

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: false,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* Same structure as config.json stubs. */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

## D. Browser hook coverage

| Hook | Discovery value | Coverage |
|---|---|---|
| fetch / XHR.open | Request URL and method | recordDetail and __API_RECON_LOG__ |
| XHR.setRequestHeader | Header names and values | Optional observe.xhrHeaders; avoid credentials |
| Storage/cookie reads | Session key names | Optional observe.storageReads/cookieReads |
| Vue route enumeration | Loaded frontend routes | __API_RECON_ROUTES__ |
| Vue guards and login redirects | Render otherwise hidden modules | neutralizeVueRouter; instrumentation only |
| React routes | Additional paths | Static analysis and interaction; no dedicated hook |
| Login navigation interception | Keep the inspection page open | Login paths only |
| Encryption libraries | Plaintext request parameters | Manual instrumentation of inputs required |
| Anti-debugging | Runtime discovery availability | Manual adaptation; static analysis remains available |

Coverage hooks cannot constrain every page network API or a compromised browser. Use external isolation and egress controls.

## E. Broader endpoint extraction

If static discovery finds too few endpoints, adapt extract_endpoints in harvest_static.py or inspect the broader patterns below.

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

## F. Troubleshooting

| Symptom | Diagnosis and action |
|---|---|
| Few static APIs | Adapt endpoint syntax and section E patterns |
| Fewer chunks than manifest entries | Check CSS-only or undeployed chunks and bounded retry results |
| Login page remains | Recheck key name, storage container, encoding and cookie URL in A1 |
| App shell opens but modules are blank | Check menu stubs, routes and the permission tree in I |
| Only bootstrap/locale requests | Check role_permissions plus permissions/all stubs and all required codes |
| Sidebar works but child pages are blank | Check intermediate nodes and userRouteAuth code correspondence |
| API responses redirect to login | Check neutralization fields and nested response handling |
| No WebSocket frames | A user interaction may be needed to subscribe; increase dwell time within limits |
| No real response bodies | Mock mode is expected; explicit read-only forwarding is required for real reads |
| Chromium missing | Install a reviewed version or set config.chromium / CHROMIUM |
| Mocking still returns to login | Inject at document start; inspect navigation behavior |
| Lists are empty | L3 fallback arrays are empty; inspect tabs, settings and detail views |
| Redux actions mistaken for routes | Exclude internal get/set/change/clear/toggle/upload paths |
| Vue still redirects | Check document-start timing and neutralizeVueRouter |
| Response URLs absent from log | Enable extractUrlsFromResponse or inspect captured detail |
| Unknown Authorization header name | Inspect header names with DevTools; avoid capturing secret values |
| Slow navigation | Use domcontentloaded and a lower routeTimeout |
| Proxy connection failure | Verify Chromium proxy address; Node forwarding with a browser proxy is intentionally refused |

Do not solve TLS or sandbox failures by disabling protections in a normal deployment. Explicit exceptions belong only in disposable, externally isolated tests.

## G. Targets with server-side enforcement

A signed server session or server-rendered protected menu may prevent the client shell from rendering. This is expected. Static bundles can still reveal endpoint paths; report the runtime coverage gap honestly.

The bundled mock workflow does not support real credentials. If authenticated testing is required, use a separately reviewed, dedicated test-account workflow with server-side authorization, secret handling, controlled egress and retention. Do not introduce production cookies into these examples.

## H. Assessment checklist

1. Confirm the authorized target scope and enforce it outside the browser/model.
2. Review and adapt harvest_static.py; inspect api_static.txt and routes.txt.
3. Expand context around known paths and follow bindings into param_candidates.json (J).
4. Analyze gates A1/A2/A3 and create site-specific config.json with synthetic values.
5. Review runtime_harvest.js, runtime_policy.js and preload.js before execution.
6. For depth mode, copy package.json and package-lock.json, use Node >=22.12 and npm ci --ignore-scripts, then run the adapted script.
7. For coverage mode, inject at document start and exercise the parameter matrix within the mock boundary.
8. If modules remain hidden, reconstruct permission stubs using I and retry.
9. Compare multiple parameter samples; record uncertainty and errors in params_merged.json.
10. Merge site_map.json and api_merged.txt; report coverage gaps and every script modification.

## I. Permission tree reconstruction

Use this when a simple synthetic menus array cannot mount child modules. This reconstructs client rendering data, not server authorization.

### I1. Locate the authorization module

Record permission API paths, response envelope fields, and the consuming chunk filenames.

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

### I2. Extract routeMap

The command produces recon/route_map.json. If no pattern matches, adapt extract_route_map.py or inspect the search expression below.

```bash
python3 scripts/extract_route_map.py recon/js recon/
```

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. Build the permission tree and stubs

The script parses userRouteAuth, including webpack aliases such as He=o.DASHBOARD, resolves aliases with route_map.json, and infers parents from prefixes (MONITOR_ALERT to MONITOR). It writes permissions_tree.json, permissions_all_stub.json and role_permissions_stub.json. The --config option updates stubs and extends routes.

Adapt DEFAULT_ROOTS (top-level codes), DEFAULT_PREFIX_PARENT (prefix-to-parent mappings), and DEFAULT_EXTRA_PARENT (non-prefix orphan relationships) at the top of the script.

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

### I4. Validate stub consistency

Compare permission count with userRouteAuth entries. Check that routes includes all route_map links.

```bash
wc -l recon/perm_codes_all.txt
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. Rerun and compare

Compare runtime_api.json before and after synthetic menu reconstruction. Distinct routes should trigger distinct module requests rather than the same bootstrap/locale calls. Route counts may grow substantially, but configured route limits still apply. More discovered endpoints do not establish server access.

```bash
node recon/runtime_harvest.js recon/config.json
```

### I6. If modules remain blank

- In coverage mode, inspect sidebar and tab interactions.
- Compare stub nesting with reviewed synthetic fixtures or documented API schemas.
- Search button-level guards such as hasPermission, checkRole and func; adapt synthetic permissions.
- Keep static paths and parameter candidates even when runtime coverage cannot be obtained. Mark missing method/body evidence explicitly.

## J. Parameter reconstruction

This is a methodology rather than a universal script: combine path context, UI bindings, multiple runtime samples, and cautiously interpreted errors.

### J1. Expand context around known paths

Start from a known phase-1 path and inspect nearby request construction objects.

```bash
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. Request wrappers and transport shape

Identify axios/shared request wrappers, GraphQL variables, FormData fields and route parameters. Preserve the distinction between query, body, path and headers.

```bash
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. Validation: required fields, formats and enums

Inspect form validators and option lists; a client constraint alone is not proof of a server constraint.

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. Follow form bindings to the API

Use DevTools Network initiator call stacks to trace fetch/send back to payload construction.

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

### J5. Encrypted parameters

Inspect encryption-function inputs in the isolated test workflow instead of guessing fields from ciphertext. Record plaintext schema observations in config.json or param_candidates.json without copying secrets.

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

### J6. Parameter interaction matrix

For each module, capture and compare requests for initial listing (pagination defaults), search (keywords/filters), advanced filters (optional fields), create/edit (entity shape), bulk/export (ids and exportType), and sorting/pagination (sortField/order).

Keep state-changing operations mocked. Any real write requires a separate authorized test workflow. Save param_samples.json entries with path, method, action, body, query and redacted headers.

### J7. Confidence rules

| Confidence | Evidence |
|---|---|
| High | Static callsite plus at least two consistent runtime samples |
| Medium | Static evidence only, or one runtime sample |
| Low | Inferred from a response/error without independent confirmation |
| Not triggered | Field exists in static code but the relevant UI/permission path was not exercised |

### J8. Common investigation sequences

| Workflow | Sequence |
|---|---|
| REST listing | Payload context (J1), several interaction samples (J6), validation rules (J3) |
| Create/edit form | Form names (J3), submit bindings (J4), mock request samples; real validation only in an authorized test environment |
| GraphQL | Variable declarations (J2), per-operation runtime variables |
| Encrypted body | Encryption inputs (J5) reveal the pre-encryption schema |

### J9. Mapping to reconnaissance phases

| Phase | Parameter work |
|---|---|
| 1: static | Expand path context (J1) |
| 2: interceptor | Identify globally injected tenantId/sign fields |
| 3: runtime | Interaction matrix and param_samples.json |
| 4: permission tree | Expose module-specific forms with synthetic permissions |
| 5: merge | params_merged.json with confidence; never infer requiredness from one sample |

### J10. Parameter troubleshooting

| Symptom | Action |
|---|---|
| Static field never observed | Mark not triggered; inspect permissions, advanced filters and dependent selects |
| Same path, different bodies | Keep separate action-specific schemas |
| Stub responses contain synthetic data | Infer request parameters from outgoing requests, not invented responses |
| Error mentions a nested field | Check data, bizData or variables envelopes |
| Only GraphQL operation names visible | Expand variables JSON and inspect static variable type declarations |

