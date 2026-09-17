package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rjchien728/archery-cli/internal/config"
)

// archeryErrorPage mirrors the shape of archery's error template: several <h4>
// elements, of which only the message carries no attributes.
const archeryErrorPage = `<html><body>
<div class="modal"><h4 class="modal-title" id="myModalLabel1">两步验证</h4></div>
<div class="modal"><h4 class="modal-title" id="myModalLabel2">扫码绑定</h4></div>
<h4>审核失败, 错误信息: 不允许的操作, 工单当前状态为 1</h4>
</body></html>`

const submitPage = `<html><body>
<select id="group_name" name="group_name">
  <option value="APENFT" group-id="3">APENFT</option>
  <option value="AINFT-NONPROD" group-id="6">AINFT-NONPROD</option>
</select>
<select id="instance_name"></select>
</body></html>`

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg := &config.Config{
		Endpoint: srv.URL,
		Instance: "test-instance",
		Username: "tester",
		Password: "secret",
	}
	c, err := New(cfg, WithCookiePath(filepath.Join(t.TempDir(), "cookies.json")))
	require.NoError(t, err)
	return c
}

func TestErrorFromHTML(t *testing.T) {
	tests := []struct {
		desc string
		body string
		want string
	}{
		{
			desc: "picks the bare h4, not the modal titles",
			body: archeryErrorPage,
			want: "审核失败, 错误信息: 不允许的操作, 工单当前状态为 1",
		},
		{
			desc: "no bare h4 yields empty",
			body: `<html><body><h4 class="modal-title">两步验证</h4></body></html>`,
			want: "",
		},
		{
			desc: "not html yields empty",
			body: `{"status": 0}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, errorFromHTML([]byte(tt.body)))
		})
	}
}

func TestErrorFromDRF(t *testing.T) {
	tests := []struct {
		desc string
		body string
		want string
	}{
		{
			desc: "permission denied detail",
			body: `{"detail":"您没有执行该操作的权限。"}`,
			want: "您没有执行该操作的权限。",
		},
		{
			desc: "field errors object",
			body: `{"instance_id":{"errors":"不存在该实例：99999"}}`,
			want: "instance_id: 不存在该实例：99999",
		},
		{
			desc: "field errors list",
			body: `{"db_name":["该字段是必填项。"]}`,
			want: "db_name: 该字段是必填项。",
		},
		{
			desc: "nested workflow object",
			body: `{"workflow":{"group_id":["该字段是必填项。"]}}`,
			want: "workflow.group_id: 该字段是必填项。",
		},
		{
			desc: "several fields join in key order",
			body: `{"instance_id":["必填"],"db_name":["必填"]}`,
			want: "db_name: 必填; instance_id: 必填",
		},
		{
			desc: "non-json yields empty",
			body: `<html></html>`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, errorFromDRF([]byte(tt.body)))
		})
	}
}

func TestParseGroupOptions(t *testing.T) {
	t.Run("reads name and group-id from the dropdown", func(t *testing.T) {
		groups, err := parseGroupOptions([]byte(submitPage))
		require.NoError(t, err)
		assert.Equal(t, []GroupRef{{ID: 3, Name: "APENFT"}, {ID: 6, Name: "AINFT-NONPROD"}}, groups)
	})

	t.Run("page without the dropdown is an error naming the escape hatch", func(t *testing.T) {
		_, err := parseGroupOptions([]byte(`<html><body>no dropdown</body></html>`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--group")
	})
}

func TestFormAction(t *testing.T) {
	tests := []struct {
		desc      string
		handler   http.HandlerFunc
		assertion func(t *testing.T, err error)
	}{
		{
			desc: "302 to detail is success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/detail/896/")
				w.WriteHeader(http.StatusFound)
			},
			assertion: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			desc: "200 with the error page is a rejection, not a success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, archeryErrorPage)
			},
			assertion: func(t *testing.T, err error) {
				require.Error(t, err, "a 200 here means archery refused the action")
				var se *ServerError
				require.ErrorAs(t, err, &se)
				assert.Contains(t, se.Msg, "不允许的操作")
			},
		},
		{
			desc: "still bounced to /login/ after a successful re-login is a failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// request() retries once through Login() when it sees a redirect
				// to /login/. Here that re-login succeeds but the action is still
				// refused — the response formAction must not read as success.
				switch r.URL.Path {
				case "/login/":
					http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "t", Path: "/"})
				case "/authenticate/":
					http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "s", Path: "/"})
					_, _ = io.WriteString(w, `{"status":0,"msg":"ok","data":null}`)
				default:
					w.Header().Set("Location", "/login/?next=/passed/")
					w.WriteHeader(http.StatusFound)
				}
			},
			assertion: func(t *testing.T, err error) {
				require.Error(t, err, "a redirect to /login/ means the action never ran")
				assert.Contains(t, err.Error(), "instead of the workflow detail page")
			},
		},
		{
			desc: "302 with no Location at all is a failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusFound)
			},
			assertion: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "instead of the workflow detail page")
			},
		},
		{
			desc: "200 without a readable message still fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `<html><body>nothing useful</body></html>`)
			},
			assertion: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "without a readable message")
			},
		},
		{
			desc: "500 surfaces as a server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			assertion: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "server error HTTP 500")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := testClient(t, tt.handler)
			tt.assertion(t, c.Approve(896, "ok"))
		})
	}
}

func TestApproveSendsRemark(t *testing.T) {
	var got url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		got = r.PostForm
		assert.Equal(t, "/passed/", r.URL.Path)
		w.Header().Set("Location", "/detail/896/")
		w.WriteHeader(http.StatusFound)
	})
	require.NoError(t, c.Approve(896, "looks fine"))
	assert.Equal(t, "896", got.Get("workflow_id"))
	assert.Equal(t, "looks fine", got.Get("audit_remark"))
}

func TestApproveAllowsEmptyRemark(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/detail/896/")
		w.WriteHeader(http.StatusFound)
	})
	require.NoError(t, c.Approve(896, ""), "archery only enforces a remark on cancel")
}

// archery's web UI asks for a reason before enabling the cancel button, but the
// endpoint accepts a blank one (verified against the live instance), so the CLI
// passes it through rather than inventing a rule of its own.
func TestCancelPassesRemarkThroughIncludingEmpty(t *testing.T) {
	var got url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		got = r.PostForm
		w.Header().Set("Location", "/detail/896/")
		w.WriteHeader(http.StatusFound)
	})
	require.NoError(t, c.Cancel(896, ""))
	assert.Equal(t, "896", got.Get("workflow_id"))
	assert.Equal(t, "", got.Get("cancel_remark"))
	assert.True(t, got.Has("cancel_remark"), "the key must still be sent; archery rejects a missing one")
}

func TestExecuteRejectsUnknownMode(t *testing.T) {
	called := false
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	err := c.Execute(896, "turbo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mode")
	assert.False(t, called)
}

func TestCheckSurfacesDRFValidationError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"instance_id":{"errors":"不存在该实例：99999"}}`)
	})
	_, err := c.Check(99999, "db_x", "SELECT 1;")
	require.Error(t, err)
	var se *ServerError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, "instance_id: 不存在该实例：99999", se.Msg)
}

func TestCheckDecodesRows(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"is_execute":false,"error_count":0,"warning_count":0,"syntax_type":2,
			"rows":[{"id":1,"stagestatus":"Audit completed","errormessage":"None",
			"sql":"UPDATE t SET a=1;","affected_rows":3,"execute_time":0,"actual_affected_rows":""}]}`)
	})
	got, err := c.Check(20, "db_x", "UPDATE t SET a=1;")
	require.NoError(t, err)
	require.Len(t, got.Rows, 1)
	assert.Equal(t, "Audit completed", got.Rows[0].StageStatus)
	assert.Equal(t, 3, got.Rows[0].AffectedRows)
	assert.Equal(t, 2, got.SyntaxType)
}

func TestSubmitSendsNestedPayload(t *testing.T) {
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		_, _ = io.WriteString(w, `{"workflow_id":900,"workflow":{"id":900,"status":"workflow_manreviewing","group_name":"G"}}`)
	})
	got, err := c.Submit(SubmitRequest{
		Name: "t", GroupID: 6, InstanceID: 20, DBName: "db_x", SQL: "UPDATE t SET a=1;",
	})
	require.NoError(t, err)
	assert.Equal(t, 900, got.WorkflowID)
	assert.Equal(t, "workflow_manreviewing", got.Workflow.Status)

	wf := body["workflow"].(map[string]any)
	assert.Equal(t, "6", wf["group_id"], "archery wants the ids as strings, like the web form posts them")
	assert.Equal(t, "20", wf["instance"])
	assert.Equal(t, float64(0), wf["is_offline_export"], "offline export is a different workflow type and stays off")
	assert.Equal(t, "UPDATE t SET a=1;", body["sql_content"])
}

func TestListDefaultsSyntaxTypesAndOmitsEmptyFilters(t *testing.T) {
	var got url.Values
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		got = r.PostForm
		_, _ = io.WriteString(w, `{"total":1,"rows":[{"id":898,"workflow_name":"x","status":"workflow_abort"}]}`)
	})
	res, err := c.List(ListOptions{Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Total)
	assert.Equal(t, []string{"0", "1", "2"}, got["syntax_type[]"])
	assert.Equal(t, "", got.Get("group_id"), "an unset group must not become group_id=0")
	assert.Equal(t, "", got.Get("instance_id"))
}

func TestResolveTarget(t *testing.T) {
	// Two groups whose instances do not overlap, plus a name present in both.
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/submitotherinstance/":
			_, _ = io.WriteString(w, submitPage)
		case "/group/instances/":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "can_write", r.PostForm.Get("tag_code"))
			switch r.PostForm.Get("group_name") {
			case "AINFT-NONPROD":
				_, _ = io.WriteString(w, `{"status":0,"msg":"ok","data":[{"id":20,"instance_name":"chat-nonprod"},{"id":19,"instance_name":"api-nonprod"},{"id":99,"instance_name":"shared"},{"id":77,"instance_name":"twice"},{"id":77,"instance_name":"twice"}]}`)
			case "APENFT":
				_, _ = io.WriteString(w, `{"status":0,"msg":"ok","data":[{"id":6,"instance_name":"apenft-prod"},{"id":99,"instance_name":"shared"}]}`)
			default:
				_, _ = io.WriteString(w, `{"status":0,"msg":"ok","data":[]}`)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}

	tests := []struct {
		desc      string
		instance  string
		group     string
		assertion func(t *testing.T, target *Target, err error)
	}{
		{
			desc:     "instance name resolves to its only group",
			instance: "chat-nonprod",
			assertion: func(t *testing.T, target *Target, err error) {
				require.NoError(t, err)
				assert.Equal(t, &Target{GroupID: 6, GroupName: "AINFT-NONPROD", InstanceID: 20}, target)
			},
		},
		{
			desc:     "explicit group narrows the search",
			instance: "shared",
			group:    "APENFT",
			assertion: func(t *testing.T, target *Target, err error) {
				require.NoError(t, err)
				assert.Equal(t, 3, target.GroupID)
				assert.Equal(t, 99, target.InstanceID)
			},
		},
		{
			desc:     "ambiguous instance is an error, not a guess",
			instance: "shared",
			assertion: func(t *testing.T, target *Target, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "several groups")
				assert.Contains(t, err.Error(), "--group")
			},
		},
		{
			desc:     "unknown instance names the groups searched",
			instance: "nope",
			assertion: func(t *testing.T, target *Target, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not writable")
			},
		},
		{
			desc:     "unknown group is rejected before instance lookup",
			instance: "chat-nonprod",
			group:    "NOSUCH",
			assertion: func(t *testing.T, target *Target, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not found among your groups")
			},
		},
		{
			desc:     "an instance listed twice in one group is not ambiguous",
			instance: "twice",
			assertion: func(t *testing.T, target *Target, err error) {
				require.NoError(t, err, "the same instance repeated inside one group is one hit, not two")
				assert.Equal(t, 6, target.GroupID)
				assert.Equal(t, 77, target.InstanceID)
			},
		},
		{
			desc:     "both ids given skips lookup entirely",
			instance: "20",
			group:    "6",
			assertion: func(t *testing.T, target *Target, err error) {
				require.NoError(t, err)
				assert.Equal(t, &Target{GroupID: 6, InstanceID: 20}, target)
			},
		},
		{
			desc:     "numeric group with a named instance cannot be resolved",
			instance: "chat-nonprod",
			group:    "6",
			assertion: func(t *testing.T, target *Target, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "instance must be an id too")
			},
		},
		{
			desc:     "empty instance is rejected",
			instance: "",
			assertion: func(t *testing.T, target *Target, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "no instance given")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := testClient(t, handler)
			target, err := c.ResolveTarget(tt.instance, tt.group)
			tt.assertion(t, target, err)
		})
	}
}

func TestGroupIDResolvesWithoutAnInstance(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/submitotherinstance/", r.URL.Path, "resolving a group must not touch the instance endpoint")
		_, _ = io.WriteString(w, submitPage)
	})
	id, err := c.GroupID("AINFT-NONPROD")
	require.NoError(t, err)
	assert.Equal(t, 6, id)

	id, err = c.GroupID("3")
	require.NoError(t, err)
	assert.Equal(t, 3, id, "a numeric group passes straight through")

	_, err = c.GroupID("NOSUCH")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found among your groups")
}

func TestGroupsCachedForClientLifetime(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.WriteString(w, submitPage)
	})
	for i := 0; i < 3; i++ {
		_, err := c.Groups()
		require.NoError(t, err)
	}
	assert.Equal(t, 1, hits, "the group dropdown is scraped once per Client")
}

func TestInstancesSurfacesEnvelopeError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":1,"msg":"您无权操作，请联系管理员","data":[]}`)
	})
	_, err := c.Instances("APENFT")
	require.Error(t, err, "status 1 in a 200 body is a failure")
	var se *ServerError
	require.ErrorAs(t, err, &se)
	assert.Contains(t, se.Msg, "无权操作")
}

func TestLoginFetchesPasswordLazily(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/":
			http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "t", Path: "/"})
		case "/authenticate/":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "prompted-pw", r.PostForm.Get("password"), "Login must use the value the callback returned")
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "s", Path: "/"})
			_, _ = io.WriteString(w, `{"status":0,"msg":"ok","data":null}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	// No password up front: the callback is the only source, and it must be
	// invoked exactly when a login happens.
	c.cfg.Password = ""
	c.passwordFunc = func() (string, error) { calls++; return "prompted-pw", nil }

	require.NoError(t, c.Login())
	assert.Equal(t, 1, calls, "the password callback runs once, only because a login occurred")
}

func TestStatusAndLogDecode(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getWorkflowStatus/":
			_, _ = io.WriteString(w, `{"status":"workflow_finish","msg":"","data":""}`)
		case "/workflow/log/":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "2", r.PostForm.Get("workflow_type"))
			_, _ = io.WriteString(w, `{"total":1,"rows":[{"operation_type_desc":"审核通过","operation_info":"审批备注: ok","operator_display":"tester","operation_time":"2026-09-16 18:34:08"}]}`)
		case "/sqlworkflow/detail_content/":
			assert.Equal(t, "896", r.URL.Query().Get("workflow_id"))
			_, _ = io.WriteString(w, `{"rows":[{"id":1,"stagestatus":"Execute Successfully","affected_rows":0,"execute_time":0.002356,"actual_affected_rows":""}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	status, err := c.Status(896)
	require.NoError(t, err)
	assert.Equal(t, "workflow_finish", status)

	logs, err := c.Log(896)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "审核通过", logs[0].OperationType)

	rows, err := c.Detail(896)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Execute Successfully", rows[0].StageStatus)
}
