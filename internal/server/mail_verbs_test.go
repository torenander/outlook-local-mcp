package server

import (
	"testing"
	"time"

	"github.com/desek/outlook-local-mcp/internal/config"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/observability"
	"github.com/desek/outlook-local-mcp/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// verbAnnotationExpectation is the four-value MCP annotation matrix asserted
// for a single verb. Title is not asserted because per-verb Verb descriptors
// carry only the four hint annotations; the title lives on the aggregate tool.
type verbAnnotationExpectation struct {
	readOnly    bool
	destructive bool
	idempotent  bool
	openWorld   bool
}

// buildTestMailVerbs constructs the mail verb slice directly, which is the
// only way to inspect per-verb annotations: they are consumed when computing
// the aggregate tool's conservative annotations and are not re-exposed through
// the registered tool or the help output.
func buildTestMailVerbs(t *testing.T, cfg config.Config) []tools.Verb {
	t.Helper()

	m, err := observability.InitMetrics(noop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("InitMetrics: %v", err)
	}

	verbs, _ := buildMailVerbs(mailVerbsConfig{
		retryCfg:          graph.RetryConfig{},
		timeout:           30 * time.Second,
		cfg:               cfg,
		m:                 m,
		tracer:            tracenoop.NewTracerProvider().Tracer("test"),
		authMW:            func(h mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc { return h },
		accountResolverMW: func(h mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc { return h },
	})
	return verbs
}

// verbByName finds a verb in the slice, failing the test when it is absent.
func verbByName(t *testing.T, verbs []tools.Verb, name string) tools.Verb {
	t.Helper()
	for _, v := range verbs {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("verb %q not registered", name)
	return tools.Verb{}
}

// TestMailVerbAnnotations_FolderAndMove asserts the per-verb MCP annotation
// matrix for every folder and message-move verb (CR-0066 FR-11, AC-6).
//
// delete_folder is the one that matters most: destructiveHint=true is what
// tells an MCP client to confirm before an irreversible folder deletion, and
// CR-0066 shipped without a test pinning it.
func TestMailVerbAnnotations_FolderAndMove(t *testing.T) {
	verbs := buildTestMailVerbs(t, config.Config{
		AuthRecordPath:    "/tmp/test",
		CacheName:         "test",
		AuthMethod:        "browser",
		MailEnabled:       true,
		MailManageEnabled: true,
	})

	want := map[string]verbAnnotationExpectation{
		// Browsing folders is a pure read and is safe to repeat.
		"list_folders": {readOnly: true, destructive: false, idempotent: true, openWorld: true},
		// Creating a folder yields a new resource on every call.
		"create_folder": {readOnly: false, destructive: false, idempotent: false, openWorld: true},
		// Deleting a folder destroys its messages and subfolders irreversibly,
		// but deleting an already-deleted folder changes nothing further.
		"delete_folder": {readOnly: false, destructive: true, idempotent: true, openWorld: true},
		// A move mints a new message id, so repeating it is not a no-op.
		"move_message":  {readOnly: false, destructive: false, idempotent: false, openWorld: true},
		"move_messages": {readOnly: false, destructive: false, idempotent: false, openWorld: true},
	}

	for name, exp := range want {
		t.Run(name, func(t *testing.T) {
			verb := verbByName(t, verbs, name)
			ann := mcp.NewTool("_introspect", verb.Annotations...).Annotations

			if ann.ReadOnlyHint == nil || *ann.ReadOnlyHint != exp.readOnly {
				t.Errorf("%s readOnlyHint = %v, want %v", name, ann.ReadOnlyHint, exp.readOnly)
			}
			if ann.DestructiveHint == nil || *ann.DestructiveHint != exp.destructive {
				t.Errorf("%s destructiveHint = %v, want %v", name, ann.DestructiveHint, exp.destructive)
			}
			if ann.IdempotentHint == nil || *ann.IdempotentHint != exp.idempotent {
				t.Errorf("%s idempotentHint = %v, want %v", name, ann.IdempotentHint, exp.idempotent)
			}
			if ann.OpenWorldHint == nil || *ann.OpenWorldHint != exp.openWorld {
				t.Errorf("%s openWorldHint = %v, want %v", name, ann.OpenWorldHint, exp.openWorld)
			}
		})
	}
}

// TestMailVerbs_FolderBrowsingNotManageGated pins CR-0066 amended B4: browsing
// folders is a read that Mail.Read already covers, so list_folders must be
// available without MAIL_MANAGE_ENABLED, while every folder write stays gated.
func TestMailVerbs_FolderBrowsingNotManageGated(t *testing.T) {
	verbs := buildTestMailVerbs(t, config.Config{
		AuthRecordPath: "/tmp/test",
		CacheName:      "test",
		AuthMethod:     "browser",
		MailEnabled:    true,
	})

	names := make(map[string]bool, len(verbs))
	for _, v := range verbs {
		names[v.Name] = true
	}

	if !names["list_folders"] {
		t.Error("list_folders must be registered without MAIL_MANAGE_ENABLED")
	}
	for _, gated := range []string{"create_folder", "delete_folder", "move_message", "move_messages"} {
		if names[gated] {
			t.Errorf("%s must stay behind MAIL_MANAGE_ENABLED", gated)
		}
	}
}

// TestMailVerbs_MergedFolderBrowsing pins the collapse of list_child_folders
// and list_folder_tree into list_folders: the two old verb names must not
// reappear in the operation surface.
func TestMailVerbs_MergedFolderBrowsing(t *testing.T) {
	verbs := buildTestMailVerbs(t, config.Config{
		AuthRecordPath:    "/tmp/test",
		CacheName:         "test",
		AuthMethod:        "browser",
		MailEnabled:       true,
		MailManageEnabled: true,
	})

	for _, v := range verbs {
		if v.Name == "list_child_folders" || v.Name == "list_folder_tree" {
			t.Errorf("verb %q was merged into list_folders and must not be registered", v.Name)
		}
	}

	listFolders := verbByName(t, verbs, "list_folders")
	schema := mcp.NewTool("_introspect", listFolders.Schema...).InputSchema
	for _, param := range []string{"folder", "recursive", "max_depth", "max_results", "output", "account"} {
		if _, ok := schema.Properties[param]; !ok {
			t.Errorf("list_folders schema missing merged parameter %q", param)
		}
	}
}
