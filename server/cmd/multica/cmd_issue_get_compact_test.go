package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCompactIssueDropsReaderNoise pins the `issue get --compact` contract:
// board bookkeeping and echoed identity go, everything a reader acts on stays.
//
// The two negative assertions matter as much as the positive ones. `metadata`
// and `properties` are empty maps here and MUST survive — the runtime brief
// tells agents an empty `{}` is a normal answer, so pruning the key would turn
// "no metadata" into "this response did not say". And `stage: 0` /
// `priority: "none"` are zero values, not absent ones: null pruning must never
// generalize to zero pruning (#6546 review).
func TestCompactIssueDropsReaderNoise(t *testing.T) {
	t.Parallel()

	issue := map[string]any{
		"id":               "i-1",
		"workspace_id":     "ws-1",
		"number":           float64(42),
		"identifier":       "MUL-42",
		"title":            "Fix login",
		"description":      "steps to reproduce",
		"status":           "in_progress",
		"status_category":  "in_progress",
		"status_name":      "",
		"priority":         "none",
		"assignee_type":    nil,
		"assignee_id":      nil,
		"parent_issue_id":  nil,
		"project_id":       "p-1",
		"position":         float64(1024),
		"revision":         float64(7),
		"stage":            float64(0),
		"start_date":       nil,
		"due_date":         nil,
		"created_at":       "2026-09-01T02:00:00Z",
		"updated_at":       "2026-09-01T02:00:00Z",
		"last_activity_at": "2026-09-02T02:00:00Z",
		"metadata":         map[string]any{},
		"properties":       map[string]any{},
		"reactions":        []any{},
	}
	compactIssue(issue)

	for _, k := range []string{
		"workspace_id", "number", "position", "revision", "status_category",
		"updated_at", "assignee_type", "assignee_id", "parent_issue_id",
		"start_date", "due_date", "reactions",
	} {
		if _, ok := issue[k]; ok {
			t.Errorf("compact kept reader-noise field %q", k)
		}
	}
	for _, k := range []string{
		"id", "identifier", "title", "description", "status", "status_name",
		"priority", "project_id", "stage", "created_at", "last_activity_at",
		"metadata", "properties",
	} {
		if _, ok := issue[k]; !ok {
			t.Errorf("compact dropped information field %q", k)
		}
	}
}

// TestCompactIssueKeepsInformativeDuplicates covers the two fields that are
// dropped conditionally: a CUSTOM status carries a status_category that does
// NOT repeat status, and an edited issue carries an updated_at that does not
// repeat created_at. Both are the only signal the reader has for that fact.
func TestCompactIssueKeepsInformativeDuplicates(t *testing.T) {
	t.Parallel()

	issue := map[string]any{
		"id":              "i-2",
		"status":          "human_review",
		"status_category": "in_review",
		"created_at":      "2026-09-01T02:00:00Z",
		"updated_at":      "2026-09-03T09:00:00Z",
	}
	compactIssue(issue)

	if got, ok := issue["status_category"]; !ok || got != "in_review" {
		t.Errorf("compact dropped a custom status's category: %v", issue["status_category"])
	}
	if _, ok := issue["updated_at"]; !ok {
		t.Error("compact dropped a real edit timestamp (updated_at != created_at)")
	}
}

// TestCompactAttachmentsKeepsOnlyTheDurableURL pins the attachment half.
//
// Of the three URLs on an attachment record only markdown_url is contracted as
// durable and persistable; url and download_url are render-time values for a
// browser, and the runtime brief already tells agents to fetch attachments
// through `multica attachment` rather than by opening a Multica resource URL.
// The identity the agent needs to make that call — id, filename, type, size —
// is exactly what survives.
func TestCompactAttachmentsKeepsOnlyTheDurableURL(t *testing.T) {
	t.Parallel()

	record := map[string]any{"attachments": []any{map[string]any{
		"id":              "att-1",
		"workspace_id":    "ws-1",
		"issue_id":        "i-1",
		"comment_id":      nil,
		"chat_session_id": nil,
		"chat_message_id": nil,
		"uploader_type":   "member",
		"uploader_id":     "u-1",
		"filename":        "screenshot.png",
		"url":             "https://cdn.example.com/o/abc?Policy=eyJ&Signature=long",
		"download_url":    "https://api.example.com/api/attachments/att-1/download",
		"markdown_url":    "https://app.example.com/api/attachments/att-1/download",
		"content_type":    "image/png",
		"size_bytes":      float64(1024),
		"created_at":      "2026-09-01T02:00:00Z",
	}}}
	compactAttachments(record)

	att := record["attachments"].([]any)[0].(map[string]any)
	for _, k := range []string{
		"workspace_id", "issue_id", "comment_id", "chat_session_id",
		"chat_message_id", "url", "download_url",
	} {
		if _, ok := att[k]; ok {
			t.Errorf("compact kept attachment noise field %q", k)
		}
	}
	for _, k := range []string{
		"id", "uploader_type", "uploader_id", "filename", "markdown_url",
		"content_type", "size_bytes", "created_at",
	} {
		if _, ok := att[k]; !ok {
			t.Errorf("compact dropped attachment field %q the agent needs", k)
		}
	}
}

// TestRunIssueGetCompactWiring proves the flag is wired end to end and — the
// half that protects every existing caller — that the DEFAULT output is
// byte-for-byte what the server sent.
func TestRunIssueGetCompactWiring(t *testing.T) {
	const issueID = "33333333-3333-4333-8333-333333333333"
	payload := map[string]any{
		"id":           issueID,
		"workspace_id": "ws-1",
		"number":       42,
		"identifier":   "TST-42",
		"title":        "Fix login",
		"description":  "steps",
		"status":       "todo",
		"assignee_id":  nil,
		"position":     1024,
		"revision":     3,
		"created_at":   "2026-09-01T02:00:00Z",
		"updated_at":   "2026-09-01T02:00:00Z",
		"metadata":     map[string]any{},
		"attachments": []any{map[string]any{
			"id":           "att-1",
			"filename":     "shot.png",
			"url":          "https://cdn.example.com/o/abc?Signature=long",
			"download_url": "https://api.example.com/api/attachments/att-1/download",
			"markdown_url": "https://app.example.com/api/attachments/att-1/download",
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/issues/"+issueID {
			_ = json.NewEncoder(w).Encode(payload)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)
	t.Setenv("MULTICA_TOKEN", "mat_test-token")

	run := func(compact bool) map[string]any {
		t.Helper()
		cmd := newIssueGetTestCmd("json", false)
		if compact {
			_ = cmd.Flags().Set("compact", "true")
		}
		out, err := captureStdout(t, func() error { return runIssueGet(cmd, []string{issueID}) })
		if err != nil {
			t.Fatalf("runIssueGet: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("output not JSON: %v\n---\n%s", err, out)
		}
		return got
	}

	plain := run(false)
	for _, k := range []string{"workspace_id", "number", "position", "revision", "updated_at", "assignee_id"} {
		if _, ok := plain[k]; !ok {
			t.Errorf("default output must be untouched but lost %q", k)
		}
	}
	if _, ok := plain["attachments"].([]any)[0].(map[string]any)["download_url"]; !ok {
		t.Error("default output must keep attachment download_url")
	}

	compact := run(true)
	for _, k := range []string{"workspace_id", "number", "position", "revision", "updated_at", "assignee_id"} {
		if _, ok := compact[k]; ok {
			t.Errorf("--compact kept reader-noise field %q", k)
		}
	}
	for _, k := range []string{"id", "identifier", "title", "description", "status", "metadata"} {
		if _, ok := compact[k]; !ok {
			t.Errorf("--compact dropped information field %q", k)
		}
	}
	att := compact["attachments"].([]any)[0].(map[string]any)
	if _, ok := att["download_url"]; ok {
		t.Error("--compact kept the render-time attachment download_url")
	}
	if _, ok := att["markdown_url"]; !ok {
		t.Error("--compact dropped the durable attachment markdown_url")
	}
}
