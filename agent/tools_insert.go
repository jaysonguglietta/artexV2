package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// assetInterceptCandidates extracts domain/IP/URL candidates from pending input, including URL host classification so URL-only services/endpoints match domain or IP rules.
func assetInterceptCandidates(item assetInputItem) (domains, ips, urls []string) {
	add := func(dst *[]string, s string) {
		if s = strings.TrimSpace(s); s != "" {
			*dst = append(*dst, s)
		}
	}
	add(&domains, item.Domain)
	for _, d := range item.BoundDomains {
		add(&domains, d)
	}
	add(&ips, item.IP)
	add(&ips, item.ServiceIP)
	add(&urls, item.URL)
	if item.URL != "" {
		if u, err := url.Parse(item.URL); err == nil {
			if h := u.Hostname(); h != "" {
				if net.ParseIP(h) != nil {
					add(&ips, h)
				} else {
					add(&domains, h)
				}
			}
		}
	}
	return domains, ips, urls
}

// assetInputLabel returns a compact asset label for interception messages.
func assetInputLabel(item assetInputItem) string {
	typ := strings.TrimSpace(item.Type)
	var target string
	switch {
	case strings.TrimSpace(item.Domain) != "":
		target = strings.TrimSpace(item.Domain)
	case strings.TrimSpace(item.URL) != "":
		target = strings.TrimSpace(item.URL)
	case strings.TrimSpace(item.IP) != "":
		target = strings.TrimSpace(item.IP)
	case strings.TrimSpace(item.ServiceIP) != "":
		target = strings.TrimSpace(item.ServiceIP)
	default:
		target = "(Unknown)"
	}
	if typ != "" {
		return fmt.Sprintf("[%s] %s", typ, target)
	}
	return target
}

// =====================================================================
// Unified asset insertion tools
// =====================================================================

// SetAssetStore wires the asset store and company store onto this ToolSet
// so the insert_assets, add_company_scope, and list_assets tools are active.
func (t *ToolSet) SetAssetStore(as *db.AssetStore, cs *db.CompanyStore) {
	t.as = as
	t.cs = cs
}

// assetInputItem is one element of the insert_assets "assets" array.
type assetInputItem struct {
	Type string `json:"type"` // root_domain|ip|subdomain|app|service|endpoint

	// ---- root_domain / subdomain ----
	Domain      string   `json:"domain"`
	ICP         string   `json:"icp"`
	RecordType  string   `json:"record_type"`
	RecordValue []string `json:"record_value"`

	// ---- ip ----
	IP           string           `json:"ip"`
	BoundDomains []string         `json:"bound_domains"`
	OpenPorts    []db.PortService `json:"open_ports"`

	// ---- app ----
	AppName     string `json:"app_name"`
	BundleID    string `json:"bundle_id"`
	Category    string `json:"category"`
	Description string `json:"description"`
	AppICP      string `json:"app_icp"`
	CompanyID   *int64 `json:"company_id"` // explicit company link (app only; others auto-attribute via scope)

	// ---- service (http) ----
	URL           string           `json:"url"`
	Technologies  []string         `json:"technologies"`
	StatusCode    *int             `json:"status_code"`
	ContentLength *int64           `json:"content_length"`
	PageTitle     string           `json:"page_title"`
	FaviconMMH3   string           `json:"favicon_mmh3"`
	Auth          []map[string]any `json:"auth"`
	ServiceName   string           `json:"service_name"`
	ServiceIP     string           `json:"service_ip"` // optional enrichment IP

	// ---- service (other) ----
	Port  int    `json:"port"`
	Proto string `json:"proto"`

	// ---- endpoint ----
	Method string           `json:"method"`
	Params []map[string]any `json:"params"`
}

// insertAssets is the unified insert_assets agent tool.
func (t *ToolSet) insertAssets() actool.CoreTool {
	return writeTool(
		"insert_assets",
		"Register newly discovered assets in a batch, mixing the enumerated types when needed.\n"+
			"Required fields: root_domain -> domain; ip -> IPv4/IPv6 ip, not a hostname; subdomain -> domain; app -> app_name; HTTP service -> url; other service -> service_name+port and at least ip or domain; endpoint -> url+method. See individual field descriptions for details.\n"+
			"auth/technologies/params append and merge rather than replacing existing values.\n"+
			"Returns {results:[{index,id,type}], errors:[{index,error}]}",
		obj(map[string]any{
			// The model cannot supply task_id; trusted orchestration assigns it through SetTaskID.
			"assets": map[string]any{
				"type":        "array",
				"description": "Asset records to register",
				"items": obj(map[string]any{
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"root_domain", "ip", "subdomain", "app", "service", "endpoint"},
						"description": "Asset type",
					},
					// root_domain / subdomain
					"domain":      str("Root domain or subdomain; required for root_domain/subdomain"),
					"icp":         str("Optional ICP registration number"),
					"record_type": str("Optional subdomain DNS record type: A/AAAA/CNAME/MX, etc."),
					"record_value": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional subdomain DNS values, such as [\"192.0.2.10\",\"192.0.2.11\"]",
					},
					// ip
					"ip": str("IPv4/IPv6 address, never a hostname. For hostnames use type=subdomain and domain. Required for ip assets; optional IP association for service/endpoint"),
					"bound_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional domains associated with an IP asset",
					},
					"open_ports": map[string]any{
						"type":        "array",
						"description": "Optional open ports for an IP asset",
						"items": obj(map[string]any{
							"port":    intp("Port number"),
							"service": str("Optional service name, such as http/ssh/mysql"),
						}, "port"),
					},
					// app
					"app_name":    str("Application name; required for app"),
					"bundle_id":   str("Optional application bundle ID"),
					"category":    str("Optional application category"),
					"description": str("Optional application description"),
					"app_icp":     str("Optional application ICP registration"),
					"company_id":  intp("Optional company ID for app. Applications cannot be assigned automatically by scope; use the ID returned by add_company_scope"),
					// service (http)
					"url":         str("Complete URL including scheme and port; required for HTTP service and automatically sets service_type=http"),
					"status_code": intp("Optional HTTP status, such as 200/301/403/404"),
					"content_length": map[string]any{
						"type":        "integer",
						"description": "Optional HTTP response-body size in bytes",
					},
					"page_title":   str("Optional page <title>"),
					"favicon_mmh3": str("Optional favicon MMH3 hash"),
					"technologies": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional technology fingerprints, such as [\"Nginx\",\"Vue\",\"Bootstrap\"]",
					},
					"auth": map[string]any{
						"type":        "array",
						"description": "Optional discovered authentication records with type/username/password, etc.; append without replacing",
						"items":       map[string]any{"type": "object"},
					},
					// Non-HTTP service.
					"service_name": str("Service name such as ssh/mysql/redis; required for non-HTTP service"),
					"port":         intp("Port; required for non-HTTP service"),
					// endpoint
					"method": str("HTTP method such as GET/POST/PUT/PATCH/DELETE; required for endpoint"),
					"params": map[string]any{
						"type":        "array",
						"description": "Optional request parameters with location (query/body/header/path), name, value, and type; append without replacing",
						"items":       map[string]any{"type": "object"},
					},
				}, "type"),
			},
		}, "assets"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("insert_assets unavailable: AssetStore is not initialized"), nil
			}
			var a struct {
				Assets []assetInputItem `json:"assets"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("invalid input: " + err.Error()), nil
			}
			// Task association comes only from trusted SetTaskID, never model arguments. Taskless auto/pentest/chat callers use taskID zero.
			taskID := t.taskID

			type result struct {
				Index int    `json:"index"`
				ID    int64  `json:"id"`
				Type  string `json:"type"`
			}
			type errEntry struct {
				Index int    `json:"index"`
				Error string `json:"error"`
			}

			var results []result
			var errs []errEntry

			// Load policy before any writes or enrichment. Missing policy is an
			// error, never an empty allowlist or blocklist.
			blockRules, err := t.as.ListAssetInterceptRules()
			if err != nil {
				return actool.Errorf("Cannot load global asset rules; insertion refused: " + err.Error()), nil
			}
			var allowRules []db.AssetInterceptRule
			if t.taskID > 0 {
				tb, ta, err := t.as.TaskInterceptRulesSplit(t.taskID)
				if err != nil {
					return actool.Errorf("Cannot load task asset rules; insertion refused: " + err.Error()), nil
				}
				blockRules = append(blockRules, tb...)
				allowRules = ta
			}

			for i, item := range a.Assets {
				// Apply deny rules before allow rules and refuse writes/enrichment side effects for blocked assets.
				domains, ips, urls := assetInterceptCandidates(item)
				if d := db.EvaluateAssetGate(blockRules, allowRules, domains, ips, urls); !d.Allowed {
					errs = append(errs, errEntry{
						Index: i,
						Error: fmt.Sprintf("Asset %s %s; insertion prohibited", assetInputLabel(item), d.Reason),
					})
					continue
				}

				typ := strings.TrimSpace(item.Type)
				var id int64
				var err error

				switch typ {
				case "root_domain":
					id, err = t.as.UpsertRootDomain(db.UpsertRootDomainReq{
						Domain: item.Domain,
						ICP:    item.ICP,
						TaskID: taskID,
					})

				case "ip":
					id, err = t.as.UpsertIP(db.UpsertIPReq{
						IP:           item.IP,
						BoundDomains: item.BoundDomains,
						OpenPorts:    item.OpenPorts,
						TaskID:       taskID,
					})

				case "subdomain":
					id, err = t.as.UpsertSubdomain(db.UpsertSubdomainReq{
						Domain:      item.Domain,
						RecordType:  item.RecordType,
						RecordValue: item.RecordValue,
						ICP:         item.ICP,
						TaskID:      taskID,
					})

				case "app":
					id, err = t.as.UpsertApp(db.UpsertAppReq{
						Name:        item.AppName,
						BundleID:    item.BundleID,
						Category:    item.Category,
						Description: item.Description,
						ICP:         item.AppICP,
						CompanyID:   item.CompanyID,
						TaskID:      taskID,
					})

				case "service":
					// distinguish HTTP vs other by presence of url
					if item.URL != "" {
						// agent may send "ip" or "service_ip" for the enrichment IP; accept both
						svcIP := item.ServiceIP
						if svcIP == "" {
							svcIP = item.IP
						}
						id, err = t.as.UpsertHTTPService(db.UpsertHTTPServiceReq{
							URL:           item.URL,
							Technologies:  item.Technologies,
							StatusCode:    item.StatusCode,
							ContentLength: item.ContentLength,
							PageTitle:     item.PageTitle,
							FaviconMMH3:   item.FaviconMMH3,
							Auth:          item.Auth,
							IP:            svcIP,
							TaskID:        taskID,
						})
					} else {
						id, err = t.as.UpsertOtherService(db.UpsertOtherServiceReq{
							Domain:      item.Domain,
							IP:          item.IP,
							Port:        item.Port,
							ServiceName: item.ServiceName,
							Auth:        item.Auth,
							TaskID:      taskID,
						})
					}

				case "endpoint":
					id, err = t.as.UpsertEndpoint(db.UpsertEndpointReq{
						URL:    item.URL,
						Method: item.Method,
						Params: item.Params,
						IP:     item.ServiceIP,
						TaskID: taskID,
					})

				default:
					errs = append(errs, errEntry{Index: i, Error: "unknown type: " + typ})
					continue
				}

				if err != nil {
					errs = append(errs, errEntry{Index: i, Error: err.Error()})
					continue
				}
				results = append(results, result{Index: i, ID: id, Type: typ})
				t.writes.Assets++
				t.anchorOwner(id)
				if taskID > 0 {
					var sourceNodeID *int64
					if t.ownerNode > 0 {
						nodeID := t.ownerNode
						sourceNodeID = &nodeID
					}
					summary := "Registered by agent through insert_assets"
					if t.ownerNode > 0 {
						summary = fmt.Sprintf("Registered by worker intent #%d through insert_assets", t.ownerNode)
					}
					_ = t.as.SetTaskAssetSource(taskID, id, "agent", summary, sourceNodeID)
				}
				// Automatically add conservative scope only for the explicitly inserted top-level asset, not derived side-effect assets. Taskless calls do nothing. Accumulate attribution/query scope independently of whether coverage metrics use it as a denominator; it is not external network authorization.
				{
					svcIP := item.ServiceIP
					if svcIP == "" {
						svcIP = item.IP
					}
					_ = t.as.AddAutoScope(taskID, typ, item.Domain, item.URL, svcIP)
				}
			}

			return jsonResult(map[string]any{
				"results": results,
				"errors":  errs,
			})
		},
	)
}

// addCompanyScope writes to company_scope table and triggers asset attribution.
func (t *ToolSet) addCompanyScope() actool.CoreTool {
	return writeTool(
		"add_company_scope",
		"Add domain/IP/CIDR/ICP/company keywords to a company's asset scope. Domain, network, and ICP entries automatically assign matching assets; keywords only give agents scope hints.\n"+
			"Company names are unique: create if absent, otherwise reuse and merge scope.\n"+
			"One scope entry per line; recognized forms are root domain, URL, IP, CIDR, ICP registration, or company keyword.\n"+
			"Always provide reason with ownership evidence, such as whois, certificate, or ASN.\n"+
			"Reject bare TLDs and overly broad networks (IPv4 /16-/32, IPv6 /32-/128). Invalid lines are skipped and returned in errors.",
		obj(map[string]any{
			"company": str("Unique company name; create if absent, otherwise reuse"),
			"scope":   str("One scope entry per line: domain, URL, IP, CIDR, ICP registration, or company keyword"),
			"reason":  str("Required ownership evidence/source"),
			"logo":    str("Optional company icon URL; applies only when creating a company"),
		}, "company", "scope"),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.cs == nil {
				return actool.Errorf("add_company_scope unavailable: CompanyStore is not initialized"), nil
			}
			var a struct {
				Company string `json:"company"`
				Scope   string `json:"scope"`
				Reason  string `json:"reason"`
				Logo    string `json:"logo"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if strings.TrimSpace(a.Company) == "" {
				return actool.Errorf("company cannot be empty"), nil
			}
			companyID, _, err := t.cs.UpsertCompany(a.Company, a.Logo)
			if err != nil {
				return actool.Errorf("Failed to create/find company: " + err.Error()), nil
			}
			lines := splitLines(a.Scope)
			added, skipped, invalid, errMsgs := t.cs.AddScope(companyID, lines, a.Reason)
			out := map[string]any{
				"company_id": companyID,
				"added":      added,
				"skipped":    skipped,
				"invalid":    invalid,
			}
			if len(errMsgs) > 0 {
				out["errors"] = errMsgs
			}
			return jsonResult(out)
		},
	)
}

// addTaskScope lets the plan agent add test scope to THE CURRENT TASK — the coverage
// denominator and the task's authorization edge. Worker discoveries are auto-scoped
// (precise host) by insertAssets; this tool is for DELIBERATELY WIDENING: pull a whole
// root domain or whole company into scope, or add a specific subdomain / ip.
func (t *ToolSet) addTaskScope() actool.CoreTool {
	return writeTool(
		"add_task_scope",
		"Add test scope to this task, used for asset coverage. Scope registration does not independently authorize network access; stay within the operator-approved boundary.\n"+
			"Kinds: company (all company assets), root_domain (entire root and subdomains), subdomain (exact host), ip, cidr, icp, keyword.\n"+
			"Explicitly encountered worker hosts automatically enter coverage scope as exact subdomains. This tool can extend it to a root/company or specified host/IP; never expand testing beyond independently approved authorization.\n"+
			"value: existing company name/ID for company; domain for root_domain/subdomain; IP/network for ip/cidr; registration number or company keyword for icp/keyword.\n"+
			"Always provide an auditable reason. Use entries for a batch.",
		obj(map[string]any{
			"entries": map[string]any{"type": "array", "description": "Batch [{kind,value}], where kind is company/root_domain/subdomain/ip/cidr/icp/keyword", "items": map[string]any{"type": "object"}},
			"kind":    str("Single kind: company / root_domain / subdomain / ip / cidr / icp / keyword"),
			"value":   str("Single company name/ID, domain, IP, CIDR, ICP registration, or keyword"),
			"reason":  str("Required auditable basis for adding scope"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("add_task_scope unavailable: AssetStore is not initialized"), nil
			}
			if t.taskID <= 0 {
				return actool.Errorf("add_task_scope requires task context; no current task"), nil
			}
			type scopeEntry struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}
			var a struct {
				Entries    []scopeEntry `json:"entries"`
				scopeEntry              // Single-entry mode.
				Reason     string       `json:"reason"`
			}
			_ = json.Unmarshal(in, &a)
			items := a.Entries
			if len(items) == 0 {
				items = []scopeEntry{a.scopeEntry}
			}
			var added []map[string]any
			errs := map[string]string{}
			for i, e := range items {
				ts, err := t.as.AddAgentScope(t.taskID, strings.TrimSpace(e.Kind), e.Value, a.Reason, "agent")
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				added = append(added, map[string]any{"kind": ts.Kind, "domain": ts.Domain, "net": ts.Net, "value": ts.Value, "company_id": ts.CompanyID})
			}
			out := map[string]any{"added": added}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		},
	)
}

// listUntestedAssets lets the plan agent pull the current + directly inherited
// scope's not-yet-tested assets on demand (filter by type, paginated).
func (t *ToolSet) listUntestedAssets() actool.CoreTool {
	return readTool(
		"list_untested_assets",
		"List assets in this task and directly linked task scopes that lack fact anchors. Linked scope is read-only. Decide yourself whether more testing is warranted; the list does not make that decision.\n"+
			"Optional asset-type filter: root_domain/subdomain/service/app/endpoint/ip.\n"+
			"Pagination: page starts at 1; page_size defaults to 10. Returns {assets:[{id,type,label}],total,page,page_size}. Requires task context.",
		obj(map[string]any{
			"type":      str("Optional asset type: root_domain/subdomain/service/app/endpoint/ip"),
			"page":      intp("One-based page; default 1"),
			"page_size": intp("Page size; default 10"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("list_untested_assets unavailable: AssetStore is not initialized"), nil
			}
			if t.taskID <= 0 || t.ts == nil {
				return actool.Errorf("list_untested_assets requires task context"), nil
			}
			var a struct {
				Type     string `json:"type"`
				Page     int    `json:"page"`
				PageSize int    `json:"page_size"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Page <= 0 {
				a.Page = 1
			}
			if a.PageSize <= 0 {
				a.PageSize = 10
			}
			offset := (a.Page - 1) * a.PageSize
			assets, total, err := t.as.ListUntestedAssetsWithSources(t.taskID, strings.TrimSpace(a.Type), a.PageSize, offset)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return jsonResult(map[string]any{
				"assets": assets, "total": total, "page": a.Page, "page_size": a.PageSize,
			})
		},
	)
}

// listAssets lets an agent query the asset table.
func (t *ToolSet) listAssets() actool.CoreTool {
	return readTool(
		"list_assets",
		"Search assets with a DSL or fetch id/ids directly, with pagination. Returns only assets within this task and directly linked tasks' test scopes.\n"+
			"DSL: field=value for ILIKE, field==value for exact, field!=value for exclusion; numeric > >= < <=; bare words search text. AND binds before OR; parentheses group expressions. Asset type uses the separate type parameter.\n"+
			"Without id/ids, dsl must be nonempty; unrestricted full-store queries are not allowed.\n"+
			"Fields: domain (root/subdomain/service), root_domain, ip, url, page_title, icp, service_name, app_name, method, service_type (http|other), record_type, technology (array; = substring, == exact), port/status_code/company_id (integers).\n"+
			"Examples: status_code>=400 AND technology=shiro; (port==80 OR port==443) AND technology=nginx",
		obj(map[string]any{
			"dsl":    str("DSL expression; required without id/ids. See tool description for syntax and fields"),
			"type":   str("Asset type: root_domain|ip|subdomain|app|service|endpoint, combined with dsl. Type alone is insufficient; still supply dsl"),
			"id":     intp("Optional direct asset ID; mutually exclusive with dsl/type"),
			"ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Optional direct asset IDs; mutually exclusive with dsl/type"},
			"limit":  intp("Optional result limit; default 10"),
			"offset": intp("Optional pagination offset; default 0"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf("list_assets unavailable: AssetStore is not initialized"), nil
			}
			var a struct {
				DSL    string  `json:"dsl"`
				Type   string  `json:"type"`
				ID     int64   `json:"id"`
				IDs    []int64 `json:"ids"`
				Limit  int     `json:"limit"`
				Offset int     `json:"offset"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 10
			}

			var assets []*db.Asset
			var err error
			switch {
			case a.ID > 0:
				assets, err = t.as.GetByIDsInScope(t.taskID, []int64{a.ID})
			case len(a.IDs) > 0:
				assets, err = t.as.GetByIDsInScope(t.taskID, a.IDs)
			case a.DSL != "":
				assets, err = t.as.QueryDSLInScope(a.DSL, a.Type, t.taskID, a.Limit, a.Offset)
			default:
				return actool.Errorf("Without id/ids, dsl cannot be empty. Supply query criteria instead of requesting all assets"), nil
			}
			if err != nil {
				return actool.Errorf("DSL error: " + err.Error()), nil
			}
			return jsonResult(map[string]any{
				"count":  len(assets),
				"assets": assets,
			})
		},
	)
}

// listCompanies enumerates companies with attribution scope and asset counts.
func (t *ToolSet) listCompanies() actool.CoreTool {
	return readTool(
		"list_companies",
		"List companies with scope and assigned-asset counts. Use the returned "+
			"company_id to associate apps in insert_assets or filter list_assets. "+
			"Optional search filters company names by case-insensitive substring; empty returns all.",
		obj(map[string]any{
			"search": str("Optional case-insensitive company-name substring; empty returns all"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			if t.cs == nil {
				return actool.Errorf("list_companies unavailable: CompanyStore is not initialized"), nil
			}
			var a struct {
				Search string `json:"search"`
			}
			_ = json.Unmarshal(in, &a)
			cos, err := t.cs.ListCompanies()
			if err != nil {
				return actool.Errorf("Company query failed: " + err.Error()), nil
			}
			q := strings.ToLower(strings.TrimSpace(a.Search))
			type companyOut struct {
				ID         int64    `json:"id"`
				Name       string   `json:"name"`
				AssetCount int      `json:"asset_count"`
				Scope      []string `json:"scope"`
			}
			out := make([]companyOut, 0, len(cos))
			for _, c := range cos {
				if q != "" && !strings.Contains(strings.ToLower(c.Name), q) {
					continue
				}
				scope := make([]string, 0, len(c.Scope))
				for _, r := range c.Scope {
					scope = append(scope, r.Raw)
				}
				out = append(out, companyOut{ID: c.ID, Name: c.Name, AssetCount: c.AssetCount, Scope: scope})
			}
			return jsonResult(map[string]any{"count": len(out), "companies": out})
		},
	)
}

// splitLines splits a multi-line string into non-empty trimmed lines.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// WorkerTools returns the tool set for a work agent.
func (t *ToolSet) WorkerTools() []actool.CoreTool {
	return []actool.CoreTool{
		// Keep list_findings so workers check confirmed task findings before duplicate reporting.
		t.listFindings(),
		t.addFinding(), t.recordFact(),
		// Asset handlers guard missing stores internally. Company ownership scope belongs to planning/control/Auto roles rather than intent-focused workers.
		t.insertAssets(), t.listAssets(),
		// Workers can reuse other intents' observations through global trace search and detailed per-intent traces to avoid repeated work.
		t.searchAllWorkerTraces(), t.getWorkerTrace(),
		// node_detail retrieves complete details after an intent/node ID is known.
		t.nodeDetail(),
		// Keep list_facts, list_companies and list_worker_traces on planner/main roles rather than the single-intent worker surface.
	}
}

// MainAgentTools returns the human-interface tool set.
func (t *ToolSet) MainAgentTools() []actool.CoreTool {
	return []actool.CoreTool{
		t.graphOverview(), t.listFindings(), t.listFacts(), t.nodeDetail(),
		t.expandDigest(), // cold-digest §6.1
		t.getWorkerOutput(), t.getWorkerTrace(), t.searchAllWorkerTraces(), t.addHint(), t.addIntent(),
		// steer_work delivers a human course correction to a running intent without interruption or discarded progress.
		t.steerWorkTool(),
		// set_goals adds runtime objectives for subsequent planner completion checks.
		t.setGoals(),
		// set_constraints adds runtime allow/deny guidance for planner/worker behavior; enforced network authorization remains external.
		t.setConstraints(),
		// asset management (handlers guard nil store internally)
		t.insertAssets(), t.addCompanyScope(), t.listAssets(),
		t.addFinding(), t.recordFact(),
		t.addTaskScope(),
		// list_untested_assets provides typed, paginated in-task untested assets on demand.
		t.listUntestedAssets(),
	}
}

// AllDomainTools returns the union of all domain tools across all agent types,
// deduped by name (mainagent order wins). Used by the server to build a registry
// for injecting domain tools into agents (Auto, custom) that don't own a per-task
// ToolSet. The caller provides real stores; tools are callable at taskID=0 scope.
func (t *ToolSet) AllDomainTools() []actool.CoreTool {
	seen := map[string]bool{}
	var out []actool.CoreTool
	all := append(append(t.MainAgentTools(), t.PlannerTools()...), t.WorkerTools()...)
	for _, tool := range all {
		if !seen[tool.Name()] {
			seen[tool.Name()] = true
			out = append(out, tool)
		}
	}
	return out
}
