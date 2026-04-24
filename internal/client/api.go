package client

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// ServerError is returned when archery responds with status != 0.
type ServerError struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
}

func (e *ServerError) Error() string {
	return e.Msg
}

// QueryResult mirrors the data block of /query/ responses.
type QueryResult struct {
	FullSQL      string   `json:"full_sql"`
	IsExecute    bool     `json:"is_execute"`
	IsMasked     bool     `json:"is_masked"`
	QueryTime    float64  `json:"query_time"`
	MaskRuleHit  bool     `json:"mask_rule_hit"`
	Warning      *string  `json:"warning"`
	Error        *string  `json:"error"`
	IsCritical   bool     `json:"is_critical"`
	Rows         [][]any  `json:"rows"`
	ColumnList   []string `json:"column_list"`
	ColumnType   []string `json:"column_type"`
	AffectedRows int      `json:"affected_rows"`
}

type queryEnvelope struct {
	Status int          `json:"status"`
	Msg    string       `json:"msg"`
	Data   *QueryResult `json:"data"`
}

type listEnvelope struct {
	Status int      `json:"status"`
	Msg    string   `json:"msg"`
	Data   []string `json:"data"`
}

// Query runs SELECT against (db, schema). limit corresponds to archery's
// limit_num (server appends LIMIT internally).
func (c *Client) Query(db, schema, sql string, limit int) (*QueryResult, error) {
	form := url.Values{
		"instance_name": {c.cfg.Instance},
		"db_name":       {db},
		"schema_name":   {schema},
		"tb_name":       {""},
		"sql_content":   {sql},
		"limit_num":     {strconv.Itoa(limit)},
	}
	status, body, err := c.request(reqSpec{
		method:    "POST",
		path:      "/query/",
		form:      form,
		autoLogin: true,
	})
	if err != nil {
		return nil, err
	}
	if status >= 500 {
		return nil, fmt.Errorf("archery server error HTTP %d", status)
	}
	if status >= 400 {
		return nil, fmt.Errorf("archery HTTP %d: %s", status, snippet(body))
	}
	var env queryEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode query response: %w (body: %s)", err, snippet(body))
	}
	if env.Status != 0 {
		return nil, &ServerError{Status: env.Status, Msg: env.Msg}
	}
	if env.Data == nil {
		return nil, fmt.Errorf("archery returned ok but empty data")
	}
	return env.Data, nil
}

// ResourceType is the kind of metadata to list via /instance/instance_resource/.
type ResourceType string

const (
	ResDatabase ResourceType = "database"
	ResSchema   ResourceType = "schema"
	ResTable    ResourceType = "table"
	ResColumn   ResourceType = "column"
)

// InstanceResource lists databases / schemas / tables / columns. db/schema/table
// are passed only when meaningful for the resource type.
func (c *Client) InstanceResource(rt ResourceType, db, schema, table string) ([]string, error) {
	q := url.Values{
		"instance_name": {c.cfg.Instance},
		"resource_type": {string(rt)},
	}
	if db != "" {
		q.Set("db_name", db)
	}
	if schema != "" {
		q.Set("schema_name", schema)
	}
	if table != "" {
		q.Set("tb_name", table)
	}
	status, body, err := c.request(reqSpec{
		method:    "GET",
		path:      "/instance/instance_resource/",
		query:     q,
		autoLogin: true,
	})
	if err != nil {
		return nil, err
	}
	if status >= 500 {
		return nil, fmt.Errorf("archery server error HTTP %d", status)
	}
	if status >= 400 {
		return nil, fmt.Errorf("archery HTTP %d: %s", status, snippet(body))
	}
	var env listEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode resource response: %w (body: %s)", err, snippet(body))
	}
	if env.Status != 0 {
		return nil, &ServerError{Status: env.Status, Msg: env.Msg}
	}
	return env.Data, nil
}

func snippet(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "...(truncated)"
	}
	return string(b)
}

// Instance exposes the configured instance name (for diagnostics).
func (c *Client) Instance() string { return c.cfg.Instance }

// Endpoint exposes the configured base URL (for diagnostics).
func (c *Client) Endpoint() string { return c.cfg.Endpoint }
