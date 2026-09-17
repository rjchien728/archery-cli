package client

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// WorkflowTypeSQLReview is archery's workflow_type for SQL 上线 (2). The other
// types (query permission = 1, archive = 3) are separate UI flows with their own
// endpoints and are out of scope here.
const WorkflowTypeSQLReview = 2

// SQLRow is one statement inside a workflow, as returned by both sqlcheck (audit
// stage) and detail_content (execution stage).
//
// ExecuteTime and ActualAffectedRows are any because archery types them
// inconsistently: a statement it accepted carries execute_time as a number, one
// it rejected carries "" — decoding either into a float64 fails on the other,
// and the rejected case is exactly when the caller needs to read the row.
type SQLRow struct {
	ID                 int    `json:"id"`
	Stage              string `json:"stage"`
	ErrLevel           int    `json:"errlevel"`
	StageStatus        string `json:"stagestatus"`
	ErrorMessage       string `json:"errormessage"`
	SQL                string `json:"sql"`
	AffectedRows       int    `json:"affected_rows"`
	ExecuteTime        any    `json:"execute_time"`
	BackupDBName       string `json:"backup_dbname"`
	ActualAffectedRows any    `json:"actual_affected_rows"`
}

// CheckResult is the response of /api/v1/workflow/sqlcheck/.
type CheckResult struct {
	IsExecute    bool     `json:"is_execute"`
	WarningCount int      `json:"warning_count"`
	ErrorCount   int      `json:"error_count"`
	IsCritical   bool     `json:"is_critical"`
	SyntaxType   int      `json:"syntax_type"`
	AffectedRows int      `json:"affected_rows"`
	Rows         []SQLRow `json:"rows"`
}

// SubmitRequest mirrors the fields archery's submit form posts. Everything
// except SQL, DBName and the resolved ids is optional.
type SubmitRequest struct {
	Name         string
	GroupID      int
	InstanceID   int
	DBName       string
	SQL          string
	IsBackup     bool
	DemandURL    string
	RunDateStart string
	RunDateEnd   string
}

// SubmitResult is the response of POST /api/v1/workflow/.
type SubmitResult struct {
	WorkflowID int `json:"workflow_id"`
	Workflow   struct {
		ID              int    `json:"id"`
		WorkflowName    string `json:"workflow_name"`
		Status          string `json:"status"`
		GroupID         int    `json:"group_id"`
		GroupName       string `json:"group_name"`
		DBName          string `json:"db_name"`
		Instance        int    `json:"instance"`
		SyntaxType      int    `json:"syntax_type"`
		AuditAuthGroups string `json:"audit_auth_groups"`
		IsBackup        bool   `json:"is_backup"`
		CreateTime      string `json:"create_time"`
	} `json:"workflow"`
	SQLContent string `json:"sql_content"`
}

// WorkflowSummary is one row of the workflow list.
type WorkflowSummary struct {
	ID           int    `json:"id"`
	WorkflowName string `json:"workflow_name"`
	Engineer     string `json:"engineer_display"`
	Status       string `json:"status"`
	IsBackup     bool   `json:"is_backup"`
	CreateTime   string `json:"create_time"`
	InstanceName string `json:"instance__instance_name"`
	DBName       string `json:"db_name"`
	GroupName    string `json:"group_name"`
	SyntaxType   int    `json:"syntax_type"`
}

// ListResult is the response of /sqlworkflow_list/.
type ListResult struct {
	Total int               `json:"total"`
	Rows  []WorkflowSummary `json:"rows"`
}

// ListOptions filters the workflow list. Zero values mean "no filter", matching
// the web UI's empty form fields.
type ListOptions struct {
	Status      string
	GroupID     int
	InstanceID  int
	SyntaxTypes []int
	StartDate   string
	EndDate     string
	Search      string
	Limit       int
	Offset      int
}

// LogRow is one entry of a workflow's audit trail.
type LogRow struct {
	OperationType string `json:"operation_type_desc"`
	OperationInfo string `json:"operation_info"`
	Operator      string `json:"operator_display"`
	OperationTime string `json:"operation_time"`
}

// Target is the (group_id, instance_id) pair the workflow API needs. The web UI
// fills both from dropdowns; ResolveTarget derives them so callers keep passing
// names.
type Target struct {
	GroupID    int
	GroupName  string
	InstanceID int
}

// GroupRef is a resource group the current user may submit workflows to.
type GroupRef struct {
	ID   int
	Name string
}

// InstanceRef is an instance inside a group.
type InstanceRef struct {
	ID     int    `json:"id"`
	Name   string `json:"instance_name"`
	Type   string `json:"type"`
	DBType string `json:"db_type"`
}

// Check runs archery's SQL audit without creating a workflow.
func (c *Client) Check(instanceID int, dbName, sql string) (*CheckResult, error) {
	payload := map[string]any{
		"full_sql":    sql,
		"instance_id": strconv.Itoa(instanceID),
		"db_name":     dbName,
	}
	var out CheckResult
	if err := c.restJSON("POST", "/api/v1/workflow/sqlcheck/", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Submit creates a workflow. It lands in workflow_manreviewing; approving and
// executing are separate calls, mirroring the web UI.
func (c *Client) Submit(req SubmitRequest) (*SubmitResult, error) {
	payload := map[string]any{
		"workflow": map[string]any{
			"workflow_name":     req.Name,
			"demand_url":        req.DemandURL,
			"group_id":          strconv.Itoa(req.GroupID),
			"instance":          strconv.Itoa(req.InstanceID),
			"db_name":           req.DBName,
			"is_backup":         req.IsBackup,
			"run_date_start":    req.RunDateStart,
			"run_date_end":      req.RunDateEnd,
			"is_offline_export": 0,
		},
		"sql_content": req.SQL,
	}
	var out SubmitResult
	if err := c.restJSON("POST", "/api/v1/workflow/", payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns workflows visible to the current user.
func (c *Client) List(opts ListOptions) (*ListResult, error) {
	form := url.Values{}
	syntaxTypes := opts.SyntaxTypes
	if len(syntaxTypes) == 0 {
		syntaxTypes = []int{0, 1, 2}
	}
	for _, st := range syntaxTypes {
		form.Add("syntax_type[]", strconv.Itoa(st))
	}
	form.Set("limit", strconv.Itoa(opts.Limit))
	form.Set("offset", strconv.Itoa(opts.Offset))
	form.Set("navStatus", opts.Status)
	form.Set("instance_id", optionalID(opts.InstanceID))
	form.Set("group_id", optionalID(opts.GroupID))
	form.Set("start_date", opts.StartDate)
	form.Set("end_date", opts.EndDate)
	form.Set("search", opts.Search)

	res, err := c.request(reqSpec{method: "POST", path: "/sqlworkflow_list/", form: form, autoLogin: true})
	if err != nil {
		return nil, err
	}
	if err := httpStatusError(res); err != nil {
		return nil, err
	}
	var out ListResult
	if err := json.Unmarshal(res.body, &out); err != nil {
		return nil, fmt.Errorf("decode workflow list: %w (body: %s)", err, snippet(res.body))
	}
	return &out, nil
}

// Detail returns the per-statement audit/execution result of one workflow.
func (c *Client) Detail(workflowID int) ([]SQLRow, error) {
	q := url.Values{"workflow_id": {strconv.Itoa(workflowID)}}
	res, err := c.request(reqSpec{method: "GET", path: "/sqlworkflow/detail_content/", query: q, autoLogin: true})
	if err != nil {
		return nil, err
	}
	if err := httpStatusError(res); err != nil {
		return nil, err
	}
	var out struct {
		Rows []SQLRow `json:"rows"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		return nil, fmt.Errorf("decode workflow detail: %w (body: %s)", err, snippet(res.body))
	}
	return out.Rows, nil
}

// Log returns a workflow's audit trail.
func (c *Client) Log(workflowID int) ([]LogRow, error) {
	form := url.Values{
		"workflow_id":   {strconv.Itoa(workflowID)},
		"workflow_type": {strconv.Itoa(WorkflowTypeSQLReview)},
	}
	res, err := c.request(reqSpec{method: "POST", path: "/workflow/log/", form: form, autoLogin: true})
	if err != nil {
		return nil, err
	}
	if err := httpStatusError(res); err != nil {
		return nil, err
	}
	var out struct {
		Rows []LogRow `json:"rows"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		return nil, fmt.Errorf("decode workflow log: %w (body: %s)", err, snippet(res.body))
	}
	return out.Rows, nil
}

// Status returns the workflow's current status string (workflow_manreviewing,
// workflow_finish, ...).
func (c *Client) Status(workflowID int) (string, error) {
	form := url.Values{"workflow_id": {strconv.Itoa(workflowID)}}
	res, err := c.request(reqSpec{method: "POST", path: "/getWorkflowStatus/", form: form, autoLogin: true})
	if err != nil {
		return "", err
	}
	if err := httpStatusError(res); err != nil {
		return "", err
	}
	var out struct {
		Status string `json:"status"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		return "", fmt.Errorf("decode workflow status: %w (body: %s)", err, snippet(res.body))
	}
	return out.Status, nil
}

// Approve passes a workflow's manual review. remark may be empty; archery's UI
// only enforces a remark on cancel.
func (c *Client) Approve(workflowID int, remark string) error {
	return c.formAction("/passed/", url.Values{
		"workflow_id":  {strconv.Itoa(workflowID)},
		"audit_remark": {remark},
	})
}

// Cancel rejects a pending workflow or aborts one the caller submitted. remark
// may be empty: archery's web UI asks for a reason before enabling the button,
// but the endpoint itself accepts a blank one.
func (c *Client) Cancel(workflowID int, remark string) error {
	return c.formAction("/cancel/", url.Values{
		"workflow_id":   {strconv.Itoa(workflowID)},
		"cancel_remark": {remark},
	})
}

// Execute runs an approved workflow. mode is "auto" (archery executes) or
// "manual" (caller ran it by hand and is recording that).
func (c *Client) Execute(workflowID int, mode string) error {
	if mode != "auto" && mode != "manual" {
		return fmt.Errorf("invalid mode %q (want auto or manual)", mode)
	}
	return c.formAction("/execute/", url.Values{
		"workflow_id": {strconv.Itoa(workflowID)},
		"mode":        {mode},
	})
}

// Groups lists the resource groups the user may submit workflows to.
//
// There is no API for this: /group/group/ and /api/v1/user/resourcegroup/ are
// admin-only, so the submit page's group dropdown is the only source. Results
// are cached for the life of the Client.
func (c *Client) Groups() ([]GroupRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groups != nil {
		return c.groups, nil
	}
	res, err := c.request(reqSpec{method: "GET", path: "/submitotherinstance/", autoLogin: true})
	if err != nil {
		return nil, err
	}
	if err := httpStatusError(res); err != nil {
		return nil, err
	}
	groups, err := parseGroupOptions(res.body)
	if err != nil {
		return nil, err
	}
	c.groups = groups
	return groups, nil
}

// GroupID resolves a group name to its numeric id, or passes a numeric id
// through. Unlike ResolveTarget it needs no instance, which is what filtering a
// list by group alone requires.
func (c *Client) GroupID(group string) (int, error) {
	if id, ok := numericID(group); ok {
		return id, nil
	}
	groups, err := c.Groups()
	if err != nil {
		return 0, err
	}
	for _, g := range groups {
		if g.Name == group {
			return g.ID, nil
		}
	}
	return 0, fmt.Errorf("group %q not found among your groups (%s)", group, groupNames(groups))
}

// Instances lists the writable instances of one group.
func (c *Client) Instances(groupName string) ([]InstanceRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.instances[groupName]; ok {
		return cached, nil
	}
	form := url.Values{"group_name": {groupName}, "tag_code": {"can_write"}}
	res, err := c.request(reqSpec{method: "POST", path: "/group/instances/", form: form, autoLogin: true})
	if err != nil {
		return nil, err
	}
	if err := httpStatusError(res); err != nil {
		return nil, err
	}
	var env struct {
		Status int           `json:"status"`
		Msg    string        `json:"msg"`
		Data   []InstanceRef `json:"data"`
	}
	if err := json.Unmarshal(res.body, &env); err != nil {
		return nil, fmt.Errorf("decode group instances: %w (body: %s)", err, snippet(res.body))
	}
	if env.Status != 0 {
		return nil, &ServerError{Status: env.Status, Msg: env.Msg}
	}
	if c.instances == nil {
		c.instances = map[string][]InstanceRef{}
	}
	c.instances[groupName] = env.Data
	return env.Data, nil
}

// ResolveTarget turns an instance name into the numeric ids the workflow API
// needs. Both arguments also accept a numeric id directly, which skips lookup —
// the escape hatch for when the submit page's HTML changes shape.
//
// group may be empty, in which case the instance is searched across every group
// the user can submit to; an instance present in more than one group is an error
// rather than a guess.
func (c *Client) ResolveTarget(instance, group string) (*Target, error) {
	if instance == "" {
		return nil, fmt.Errorf("no instance given")
	}
	instanceID, instanceIsID := numericID(instance)
	groupID, groupIsID := numericID(group)

	if instanceIsID && groupIsID {
		return &Target{GroupID: groupID, InstanceID: instanceID}, nil
	}
	if groupIsID {
		return nil, fmt.Errorf("group given as id %d, so instance must be an id too: archery has no endpoint listing instances by group id (pass the instance id, or give the group by name)", groupID)
	}

	var candidates []GroupRef
	if group != "" {
		groups, err := c.Groups()
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			if g.Name == group {
				candidates = append(candidates, g)
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("group %q not found among your groups (%s)", group, groupNames(groups))
		}
	} else {
		groups, err := c.Groups()
		if err != nil {
			return nil, err
		}
		candidates = groups
	}

	var hits []Target
	seen := map[[2]int]bool{}
	for _, g := range candidates {
		instances, err := c.Instances(g.Name)
		if err != nil {
			return nil, err
		}
		for _, in := range instances {
			matched := (instanceIsID && in.ID == instanceID) || (!instanceIsID && in.Name == instance)
			if !matched || seen[[2]int{g.ID, in.ID}] {
				continue
			}
			seen[[2]int{g.ID, in.ID}] = true
			hits = append(hits, Target{GroupID: g.ID, GroupName: g.Name, InstanceID: in.ID})
		}
	}

	switch len(hits) {
	case 1:
		return &hits[0], nil
	case 0:
		return nil, fmt.Errorf("instance %q is not writable in any of your groups (%s); workflows can only target instances tagged can_write", instance, groupNames(candidates))
	default:
		var names []string
		for _, h := range hits {
			names = append(names, h.GroupName)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("instance %q exists in several groups (%s); pick one with --group", instance, strings.Join(names, ", "))
	}
}

// formAction posts to one of archery's Django form views. Those signal success
// with a 302 to /detail/<id>/ and failure with a 200 carrying an error page, so
// a 200 here is a rejection, not a success.
func (c *Client) formAction(path string, form url.Values) error {
	res, err := c.request(reqSpec{method: "POST", path: path, form: form, autoLogin: true})
	if err != nil {
		return err
	}
	if res.status == 302 || res.status == 301 {
		// A redirect anywhere else is not success: an expired session bounces to
		// /login/, and request() has already retried once by this point.
		if strings.Contains(res.location, "/detail/") {
			return nil
		}
		return fmt.Errorf("archery redirected %s to %q instead of the workflow detail page; the session may have expired or the action was refused", path, res.location)
	}
	if res.status >= 500 {
		return fmt.Errorf("archery server error HTTP %d", res.status)
	}
	if res.status >= 400 {
		return fmt.Errorf("archery HTTP %d: %s", res.status, snippet(res.body))
	}
	if msg := errorFromHTML(res.body); msg != "" {
		return &ServerError{Status: 1, Msg: msg}
	}
	return fmt.Errorf("archery rejected %s without a redirect and without a readable message (body: %s)", path, snippet(res.body))
}

// restJSON calls one of the /api/v1/ endpoints, which are DRF views: failures
// come back as 4xx with a JSON error document, not as an error page.
func (c *Client) restJSON(method, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", path, err)
	}
	res, err := c.request(reqSpec{method: method, path: path, jsonBody: body, autoLogin: true})
	if err != nil {
		return err
	}
	if res.status >= 500 {
		return fmt.Errorf("archery server error HTTP %d", res.status)
	}
	if res.status >= 400 {
		if msg := errorFromDRF(res.body); msg != "" {
			return &ServerError{Status: res.status, Msg: msg}
		}
		return fmt.Errorf("archery HTTP %d: %s", res.status, snippet(res.body))
	}
	if err := json.Unmarshal(res.body, out); err != nil {
		return fmt.Errorf("decode %s response: %w (body: %s)", path, err, snippet(res.body))
	}
	return nil
}

// httpStatusError covers the endpoints that return plain JSON on success; their
// per-payload status field is checked by the caller.
func httpStatusError(res *response) error {
	if res.status >= 500 {
		return fmt.Errorf("archery server error HTTP %d", res.status)
	}
	if res.status >= 400 {
		return fmt.Errorf("archery HTTP %d: %s", res.status, snippet(res.body))
	}
	if res.status >= 300 {
		return fmt.Errorf("archery redirected to %q instead of answering; the session may have expired", res.location)
	}
	return nil
}

// errorFromHTML pulls the message out of archery's error page. That page carries
// several <h4> elements — the modal titles all have attributes, the error text is
// the only bare one.
func errorFromHTML(body []byte) string {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "h4" && len(n.Attr) == 0 {
			if txt := strings.TrimSpace(textOf(n)); txt != "" {
				found = txt
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	return found
}

func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return sb.String()
}

// errorFromDRF flattens a DRF validation document into one line. The shapes seen
// in the wild: {"detail": "..."}, {"field": ["..."]}, {"field": {"errors": "..."}}
// and one more level of nesting for the submit endpoint's "workflow" object.
func errorFromDRF(body []byte) string {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	var parts []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch t := v.(type) {
		case string:
			parts = append(parts, joinPrefix(prefix, t))
		case []any:
			for _, item := range t {
				walk(prefix, item)
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				next := k
				if k == "detail" || k == "errors" {
					next = prefix
				} else if prefix != "" {
					next = prefix + "." + k
				}
				walk(next, t[k])
			}
		default:
			if t != nil {
				parts = append(parts, joinPrefix(prefix, fmt.Sprint(t)))
			}
		}
	}
	walk("", doc)
	return strings.Join(parts, "; ")
}

func joinPrefix(prefix, msg string) string {
	if prefix == "" {
		return msg
	}
	return prefix + ": " + msg
}

// parseGroupOptions reads the submit page's group dropdown, where each option
// carries its numeric id in a group-id attribute.
func parseGroupOptions(body []byte) ([]GroupRef, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse submit page: %w", err)
	}
	var out []GroupRef
	var inSelect bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "select" && attr(n, "id") == "group_name" {
			inSelect = true
			defer func() { inSelect = false }()
		}
		if inSelect && n.Type == html.ElementNode && n.Data == "option" {
			name := attr(n, "value")
			if id, err := strconv.Atoi(attr(n, "group-id")); err == nil && name != "" {
				out = append(out, GroupRef{ID: id, Name: name})
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	if len(out) == 0 {
		return nil, fmt.Errorf("no groups found on archery's submit page: it has no group dropdown, or the page changed shape (pass ids directly via --group and --instance)")
	}
	return out, nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// numericID reports whether s is a bare positive integer, which callers may pass
// wherever a name is accepted.
func numericID(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func optionalID(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func groupNames(groups []GroupRef) string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
