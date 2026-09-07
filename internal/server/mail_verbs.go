// Package server — this file builds the mail domain verb slice for the
// aggregate "mail" MCP tool (CR-0060 Phase 3c).
//
// It lives in the server package rather than tools to avoid the import cycle
// that would arise from tools importing tools/help (which itself imports tools).
//
// Verb registration is feature-flag gated:
//   - Always-on (when mail is enabled at all): help, list_folders, list_messages,
//     get_message, search_messages.
//   - Gated by MailEnabled: get_conversation, list_attachments, get_attachment.
//   - Gated by MailManageEnabled: create_draft, create_reply_draft,
//     create_forward_draft, update_draft, delete_draft, create_folder,
//     delete_folder, move_message, move_messages.
//
// Folder browsing is NOT MailManageEnabled-gated: list_folders is a read that
// works with the Mail.Read scope, so it stays in the read tier alongside
// list_messages (CR-0066 amended, reversing that CR's rejected alternative 1).
//
// The aggregate "mail" tool is registered unconditionally (FR-1). The operation
// enum only includes verbs whose feature flag is enabled at server start (FR-2).
package server

import (
	"time"

	"github.com/desek/outlook-local-mcp/internal/audit"
	"github.com/desek/outlook-local-mcp/internal/config"
	"github.com/desek/outlook-local-mcp/internal/graph"
	"github.com/desek/outlook-local-mcp/internal/observability"
	"github.com/desek/outlook-local-mcp/internal/tools"
	"github.com/desek/outlook-local-mcp/internal/tools/help"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/trace"
)

// mailVerbsConfig holds the dependencies required to build the mail domain verb
// slice. All fields are captured at server start.
type mailVerbsConfig struct {
	// retryCfg is the Graph API retry configuration applied to all mail handlers.
	retryCfg graph.RetryConfig

	// timeout is the maximum duration for a single Graph API call.
	timeout time.Duration

	// cfg is the full server configuration, used for feature-flag gating and
	// derived values such as MaxAttachmentSizeBytes and ProvenanceTag.
	cfg config.Config

	// provenancePropertyID is the fully-qualified MAPI extended property ID for
	// provenance tagging, built once at startup. Empty string disables tagging.
	provenancePropertyID string

	// m is the ToolMetrics instance for observability instrumentation.
	m *observability.ToolMetrics

	// tracer is the OTEL tracer for span creation.
	tracer trace.Tracer

	// authMW is the authentication middleware factory applied to every mail verb.
	authMW func(mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc

	// accountResolverMW is the account-resolver middleware applied to every mail
	// verb (mail tools resolve the Graph client via AccountResolver).
	accountResolverMW func(mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc

	// readOnly controls whether write verbs are blocked by ReadOnlyGuard.
	readOnly bool
}

// buildMailVerbs constructs the ordered []tools.Verb slice for the mail domain
// aggregate tool and returns a pointer to an initially empty VerbRegistry.
//
// Verbs are partitioned into three tiers based on feature flags:
//   - Always-on: list_folders, list_messages, get_message, search_messages
//     (registered whenever mail access is active at all; the caller is
//     responsible for calling this function only when mail is needed).
//   - MailEnabled-gated: get_conversation, list_attachments, get_attachment
//     (require Mail.Read scope provided by MailEnabled).
//   - MailManageEnabled-gated: create_draft, create_reply_draft,
//     create_forward_draft, update_draft, delete_draft, create_folder,
//     delete_folder, move_message, move_messages (require Mail.ReadWrite
//     scope provided by MailManageEnabled).
//
// Each verb's Handler is pre-wrapped with authMW, accountResolverMW,
// observability, and audit middleware using the fully-qualified identity
// "mail.<verb>" per CR-0060 FR-13 and FR-14. Write verbs additionally include
// ReadOnlyGuard between observability and audit.
//
// The returned registry pointer is empty at the time of return. The caller
// MUST call RegisterDomainTool with the returned verbs, then assign the returned
// VerbRegistry back through the pointer so that the help verb can introspect
// all registered verbs at call time.
//
// Parameters:
//   - c: mailVerbsConfig with all required dependencies.
//
// Returns:
//   - verbs: ordered Verb slice for use with RegisterDomainTool.
//   - registryPtr: pointer whose value is assigned after registration.
func buildMailVerbs(c mailVerbsConfig) ([]tools.Verb, *tools.VerbRegistry) {
	empty := make(tools.VerbRegistry)
	registryPtr := &empty

	// wrap builds the read-verb chain: authMW -> accountResolverMW -> WithObservability -> AuditWrap -> Handler.
	wrap := func(name, auditOp string, h mcpserver.ToolHandlerFunc) tools.Handler {
		return tools.Handler(c.authMW(c.accountResolverMW(observability.WithObservability(name, c.m, c.tracer, audit.AuditWrap(name, auditOp, h)))))
	}

	// wrapWrite adds ReadOnlyGuard between observability and audit for write verbs.
	wrapWrite := func(name, auditOp string, h mcpserver.ToolHandlerFunc) tools.Handler {
		return tools.Handler(c.authMW(c.accountResolverMW(observability.WithObservability(name, c.m, c.tracer, ReadOnlyGuard(name, c.readOnly, audit.AuditWrap(name, auditOp, h))))))
	}

	rc := c.retryCfg

	verbs := []tools.Verb{
		help.NewHelpVerb(registryPtr),
		// Always-on read verbs.
		buildListFoldersVerb(c, rc, wrap),
		buildListMessagesVerb(c, rc, wrap),
		buildGetMessageVerb(c, rc, wrap),
		buildSearchMessagesVerb(c, rc, wrap),
	}

	// MailEnabled-gated read verbs.
	if c.cfg.MailEnabled {
		verbs = append(verbs,
			buildGetConversationVerb(c, rc, wrap),
			buildListAttachmentsVerb(c, rc, wrap),
			buildGetAttachmentVerb(c, rc, wrap),
		)
	}

	// MailManageEnabled-gated write verbs.
	if c.cfg.MailManageEnabled {
		verbs = append(verbs,
			buildCreateDraftVerb(c, rc, wrapWrite),
			buildCreateReplyDraftVerb(c, rc, wrapWrite),
			buildCreateForwardDraftVerb(c, rc, wrapWrite),
			buildUpdateDraftVerb(c, rc, wrapWrite),
			buildDeleteDraftVerb(c, rc, wrapWrite),
			// Folder management writes. Folder *browsing* is the read-tier
			// list_folders verb registered above, not gated here.
			buildCreateFolderVerb(c, rc, wrapWrite),
			buildDeleteFolderVerb(c, rc, wrapWrite),
			// Message move operations.
			buildMoveMessageVerb(c, rc, wrapWrite),
			buildMoveMessagesVerb(c, rc, wrapWrite),
		)
	}

	return verbs, registryPtr
}

// buildListFoldersVerb constructs the list_folders Verb: the mail domain's
// single folder-browsing operation (CR-0066 amended).
//
// It subsumes the former list_child_folders and list_folder_tree verbs. Those
// differed from list_folders only in where a listing starts and how deep it
// goes, so they are now the `folder` and `recursive`/`max_depth` parameters of
// this verb instead of two extra entries in the operation enum.
//
// The verb is registered in the read tier, not behind MailManageEnabled:
// browsing folders is a read that the Mail.Read scope already covers.
func buildListFoldersVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "list_folders",
		Summary:     "browse mail folders; one level by default, whole tree with recursive=true",
		Description: "Browses the mail folder hierarchy. Omit `folder` to list the top-level folders; set it to a well-known name (\"inbox\"), a display-name path (\"Inbox/01 Projects\"), a top-level folder name, or a Graph folder ID to list that folder's subfolders. Set recursive=true to descend, with max_depth counting the level you asked for (max_depth=1 is exactly one level). Default text output is a markdown tree labelled by path, not by 150-character Graph IDs — every path it prints is accepted back by `folder` here and by `parent`, `destination`, and `folder_id` on the other mail verbs, so the cheap output stays chainable. Use output=summary for JSON including IDs, or output=raw for the unmodified Graph fields. Listings that Graph truncates say so; nothing is capped silently.",
		Examples: []tools.Example{
			{Args: map[string]any{}, Comment: "list the top-level folders"},
			{Args: map[string]any{"folder": "Inbox", "recursive": true}, Comment: "list everything under Inbox, 3 levels deep"},
			{Args: map[string]any{"folder": "Inbox/01 Projects"}, Comment: "list one level under a nested folder, addressed by path"},
			{Args: map[string]any{"recursive": true, "max_depth": 2, "output": "summary"}, Comment: "two-level tree as JSON, including folder IDs"},
		},
		SeeDocs: []string{"concepts#output-tiers", "concepts#mail-gating"},
		Handler: wrap("mail.list_folders", "read", tools.NewHandleListFolders(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			// folder_id is accepted as a legacy alias at call time. It is not
			// declared here because list_messages already contributes it to the
			// aggregate schema with a description that suits that verb better.
			mcp.WithString("folder",
				mcp.Description("Folder to target: well-known name (inbox), path (\"Inbox/01 Projects\"), top-level name, or Graph folder ID. Omit for the default scope."),
			),
			mcp.WithBoolean("recursive",
				mcp.Description("Descend into subfolders (default false = one level)."),
			),
			mcp.WithNumber("max_depth",
				mcp.Description("Levels to return when recursive=true, counting the requested level (default 3, max 10)."),
				mcp.Min(1),
				mcp.Max(10),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum folders per level (default 100, max 1000); truncation is reported."),
				mcp.Min(1),
				mcp.Max(1000),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw'."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildListMessagesVerb constructs the list_messages Verb.
func buildListMessagesVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "list_messages",
		Summary:     "list messages in a folder or across all folders; filter by date, sender, thread",
		Description: "Lists messages in a mail folder or across all folders. `folder` accepts a well-known name (\"inbox\"), a display-name path (\"Inbox/01 Projects\") as printed by list_folders, a top-level folder name, or a Graph folder ID; folder_id is accepted as an alias. Supports optional filters for date range, sender, conversation thread, read state, draft state, attachment presence, importance, and flag status. Results include a bodyPreview; use get_message with output=raw for the full HTML body. For full-text search, use search_messages instead.",
		Examples: []tools.Example{
			{Args: map[string]any{"folder": "Inbox", "is_read": false}, Comment: "list unread messages in inbox"},
			{Args: map[string]any{"folder": "Inbox/01 Projects", "max_results": 10}, Comment: "list messages in a nested folder addressed by path"},
			{Args: map[string]any{"from": "alice@contoso.com", "max_results": 10}, Comment: "list recent messages from a sender"},
		},
		SeeDocs: []string{"concepts#output-tiers", "concepts#mail-gating"},
		Handler: wrap("mail.list_messages", "read", tools.NewHandleListMessages(rc, c.timeout, c.provenancePropertyID)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			// Declared even though list_folders already contributes `folder` to
			// the aggregate union, so this verb does not silently depend on
			// list_folders staying registered: if `folder` ever left the union,
			// a stripping client would turn a scoped query into an all-folders
			// query with plausible-looking results. Duplicate names are deduped
			// by aggregateSchemaOptions, so this costs no schema bytes.
			mcp.WithString("folder",
				mcp.Description("Folder to list messages from: well-known name (inbox), path (\"Inbox/01 Projects\"), top-level name, or Graph folder ID. Omit to list from all folders."),
			),
			mcp.WithString("folder_id",
				mcp.Description("Alias for `folder`; also accepts folder names and paths."),
			),
			mcp.WithString("start_datetime",
				mcp.Description("Start of date range (ISO 8601, e.g. 2026-03-12T00:00:00Z). Filters by receivedDateTime >=."),
			),
			mcp.WithString("end_datetime",
				mcp.Description("End of date range (ISO 8601). Filters by receivedDateTime <=."),
			),
			mcp.WithString("from",
				mcp.Description("Sender email address to filter by (e.g. alice@contoso.com)."),
			),
			mcp.WithString("conversation_id",
				mcp.Description("Conversation ID to retrieve all messages in a thread."),
			),
			mcp.WithBoolean("is_read",
				mcp.Description("Filter by read/unread state. Omit to include both."),
			),
			mcp.WithBoolean("is_draft",
				mcp.Description("Filter by draft state. Omit to include both."),
			),
			mcp.WithBoolean("has_attachments",
				mcp.Description("Filter by attachment presence. Omit to include both."),
			),
			mcp.WithString("importance",
				mcp.Description("Filter by message importance."),
				mcp.Enum("low", "normal", "high"),
			),
			mcp.WithString("flag_status",
				mcp.Description("Filter by follow-up flag status."),
				mcp.Enum("notFlagged", "flagged", "complete"),
			),
			mcp.WithBoolean("provenance",
				mcp.Description("Filter to messages created by this MCP server (requires provenance tagging)."),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum number of messages to return (default 25, max 100)."),
				mcp.Min(1),
				mcp.Max(100),
			),
			mcp.WithString("timezone",
				mcp.Description("IANA timezone name for the Prefer: outlook.timezone header."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw'."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildGetMessageVerb constructs the get_message Verb.
func buildGetMessageVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "get_message",
		Summary:     "get full message details by ID; bodyPreview by default, full body via output=raw",
		Description: "Fetches full metadata for a single mail message by its ID. Text and summary output include a bodyPreview (first 255 characters). To read the complete HTML body and all headers, use output=raw. Use list_messages or search_messages to obtain a message ID.",
		SeeDocs:     []string{"concepts#output-tiers"},
		Handler:     wrap("mail.get_message", "read", tools.NewHandleGetMessage(rc, c.timeout, c.provenancePropertyID)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the message to retrieve."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw' (includes full HTML body and headers)."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildSearchMessagesVerb constructs the search_messages Verb.
func buildSearchMessagesVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "search_messages",
		Summary:     "full-text KQL search across messages; ranked by relevance, not chronologically",
		Description: "Searches mail messages using Keyword Query Language (KQL). Results are ranked by relevance, not chronological order. KQL supports field-scoped queries such as 'subject:\"meeting\"', 'from:alice@contoso.com', and 'hasAttachments:true'. Use list_messages with date filters for chronological browsing. `folder` restricts the search and accepts a well-known name, a display-name path (\"Inbox/01 Projects\") as printed by list_folders, a top-level folder name, or a Graph folder ID; folder_id is accepted as an alias.",
		Examples: []tools.Example{
			{Args: map[string]any{"query": "subject:\"quarterly review\""}, Comment: "find messages with a specific subject"},
			{Args: map[string]any{"query": "from:alice@contoso.com hasAttachments:true"}, Comment: "find messages with attachments from a sender"},
			{Args: map[string]any{"query": "budget", "folder": "Inbox/01 Projects"}, Comment: "search within a nested folder addressed by path"},
		},
		SeeDocs: []string{"concepts#output-tiers"},
		Handler: wrap("mail.search_messages", "read", tools.NewHandleSearchMessages(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("KQL search string (e.g. subject:\"Design Review\" from:alice@contoso.com)."),
			),
			mcp.WithString("folder",
				mcp.Description("Folder to restrict the search to: well-known name, path (\"Inbox/01 Projects\"), top-level name, or Graph folder ID. Omit to search all folders."),
			),
			mcp.WithString("folder_id",
				mcp.Description("Alias for `folder`; also accepts folder names and paths."),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum number of messages to return (default 25, max 100)."),
				mcp.Min(1),
				mcp.Max(100),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw'."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildGetConversationVerb constructs the get_conversation Verb (MailEnabled-gated).
func buildGetConversationVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "get_conversation",
		Summary:     "retrieve all messages in an email thread in chronological order",
		Description: "Retrieves all messages that share a conversation thread in chronological order. Supply either a message_id (the server resolves the conversationId) or a conversation_id directly. Requires MAIL_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrap("mail.get_conversation", "read", tools.NewHandleGetConversation(rc, c.timeout, c.provenancePropertyID)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Description("A message ID in the conversation; conversationId is resolved from it."),
			),
			mcp.WithString("conversation_id",
				mcp.Description("Conversation ID to retrieve directly (skips the initial message fetch)."),
			),
			mcp.WithNumber("max_results",
				mcp.Description("Maximum number of messages to return (default 50, max 100)."),
				mcp.Min(1),
				mcp.Max(100),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw'."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildListAttachmentsVerb constructs the list_attachments Verb (MailEnabled-gated).
func buildListAttachmentsVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "list_attachments",
		Summary:     "list attachment metadata (id, name, contentType, size) for a message",
		Description: "Lists the attachments of a mail message, returning metadata: attachment ID, name, content type, and size in bytes. Use get_attachment with the returned attachment_id to download the content. Requires MAIL_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrap("mail.list_attachments", "read", tools.NewHandleListAttachments(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the parent message."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw'."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildGetAttachmentVerb constructs the get_attachment Verb (MailEnabled-gated).
func buildGetAttachmentVerb(c mailVerbsConfig, rc graph.RetryConfig, wrap func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "get_attachment",
		Summary:     "download an attachment; returns metadata and base64 content up to the size limit",
		Description: "Downloads a mail attachment by ID and returns its metadata plus base64-encoded content. Attachments larger than the server's MaxAttachmentSizeBytes limit are rejected with an informative error. Requires MAIL_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrap("mail.get_attachment", "read", tools.NewHandleGetAttachment(rc, c.timeout, c.cfg.MaxAttachmentSizeBytes)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the parent message."),
			),
			mcp.WithString("attachment_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the attachment."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
			mcp.WithString("output",
				mcp.Description("Output mode: 'text' (default), 'summary', or 'raw'."),
				mcp.Enum("text", "summary", "raw"),
			),
		},
	}
}

// buildCreateDraftVerb constructs the create_draft Verb (MailManageEnabled-gated).
func buildCreateDraftVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "create_draft",
		Summary:     "create a new email draft in the Drafts folder (not sent automatically)",
		Description: "Creates a new email draft and saves it to the Drafts folder. The draft is never sent automatically; the user opens Outlook and sends it manually. Supports To, Cc, Bcc recipients, subject, plain-text or HTML body, and importance. Requires MAIL_MANAGE_ENABLED=true.",
		Examples: []tools.Example{
			{Args: map[string]any{"to_recipients": "alice@contoso.com", "subject": "Follow-up", "body": "Hi Alice..."}, Comment: "create a simple plain-text draft"},
		},
		SeeDocs: []string{"concepts#mail-gating"},
		Handler: wrapWrite("mail.create_draft", "write", tools.NewHandleCreateDraft(rc, c.timeout, c.provenancePropertyID)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("to_recipients",
				mcp.Description("Comma-separated list of To recipient email addresses."),
			),
			mcp.WithString("cc_recipients",
				mcp.Description("Comma-separated list of Cc recipient email addresses."),
			),
			mcp.WithString("bcc_recipients",
				mcp.Description("Comma-separated list of Bcc recipient email addresses."),
			),
			mcp.WithString("subject",
				mcp.Description("Draft subject line."),
			),
			mcp.WithString("body",
				mcp.Description("Draft body content. Plain text unless content_type is 'html'."),
			),
			mcp.WithString("content_type",
				mcp.Description("Body content type: 'text' (default) or 'html'."),
				mcp.Enum("text", "html"),
			),
			mcp.WithString("importance",
				mcp.Description("Draft importance: low, normal, or high."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildCreateReplyDraftVerb constructs the create_reply_draft Verb (MailManageEnabled-gated).
func buildCreateReplyDraftVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "create_reply_draft",
		Summary:     "create a reply draft to an existing message preserving threading headers",
		Description: "Creates a reply draft for an existing message, preserving all email threading headers (References, In-Reply-To). The original message is quoted automatically. Use reply_all=true to reply to all original recipients. Requires MAIL_MANAGE_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrapWrite("mail.create_reply_draft", "write", tools.NewHandleCreateReplyDraft(rc, c.timeout, c.provenancePropertyID)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the source message to reply to."),
			),
			mcp.WithString("comment",
				mcp.Description("Optional reply body text prepended to the quoted original."),
			),
			mcp.WithBoolean("reply_all",
				mcp.Description("When true, reply to all original recipients (To + Cc). Default false."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildCreateForwardDraftVerb constructs the create_forward_draft Verb (MailManageEnabled-gated).
func buildCreateForwardDraftVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "create_forward_draft",
		Summary:     "create a forward draft of an existing message with new recipients",
		Description: "Creates a forward draft for an existing message with the original message quoted. Supply the new To recipients and an optional forward comment. Requires MAIL_MANAGE_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrapWrite("mail.create_forward_draft", "write", tools.NewHandleCreateForwardDraft(rc, c.timeout, c.provenancePropertyID)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the source message to forward."),
			),
			mcp.WithString("to_recipients",
				mcp.Description("Comma-separated list of To recipient email addresses."),
			),
			mcp.WithString("comment",
				mcp.Description("Optional forward body text prepended to the quoted original."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildUpdateDraftVerb constructs the update_draft Verb (MailManageEnabled-gated).
func buildUpdateDraftVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "update_draft",
		Summary:     "update draft fields (PATCH semantics; non-draft messages rejected)",
		Description: "Updates fields of an existing draft using PATCH semantics: only supplied fields are changed. Attempting to update a non-draft message returns an error. Supports recipients, subject, body, content type, and importance. Requires MAIL_MANAGE_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrapWrite("mail.update_draft", "write", tools.NewHandleUpdateDraft(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the draft message to update."),
			),
			mcp.WithString("to_recipients",
				mcp.Description("Comma-separated list of To recipient email addresses (replaces existing)."),
			),
			mcp.WithString("cc_recipients",
				mcp.Description("Comma-separated list of Cc recipient email addresses (replaces existing)."),
			),
			mcp.WithString("bcc_recipients",
				mcp.Description("Comma-separated list of Bcc recipient email addresses (replaces existing)."),
			),
			mcp.WithString("subject",
				mcp.Description("New draft subject line."),
			),
			mcp.WithString("body",
				mcp.Description("New draft body content."),
			),
			mcp.WithString("content_type",
				mcp.Description("Body content type: 'text' or 'html'."),
				mcp.Enum("text", "html"),
			),
			mcp.WithString("importance",
				mcp.Description("New draft importance: low, normal, or high."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildDeleteDraftVerb constructs the delete_draft Verb (MailManageEnabled-gated).
func buildDeleteDraftVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "delete_draft",
		Summary:     "permanently delete a draft message (irreversible; non-draft messages rejected)",
		Description: "Permanently deletes a draft message. This operation is irreversible. Attempting to delete a non-draft message returns an error as a safety guard. Requires MAIL_MANAGE_ENABLED=true.",
		SeeDocs:     []string{"concepts#mail-gating"},
		Handler:     wrapWrite("mail.delete_draft", "delete", tools.NewHandleDeleteDraft(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the draft message to delete."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildCreateFolderVerb constructs the create_folder Verb (MailManageEnabled-gated).
func buildCreateFolderVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "create_folder",
		Summary:     "create a mail folder, optionally nested under a parent",
		Description: "Creates a new mail folder. When `parent` is provided the folder is created inside it; otherwise it is created at the top level. `parent` accepts a well-known name (\"inbox\"), a display-name path (\"Inbox/01 Projects\") as printed by list_folders, a top-level folder name, or a Graph folder ID; parent_folder_id is accepted as an alias. Requires MAIL_MANAGE_ENABLED=true.",
		Examples: []tools.Example{
			{Args: map[string]any{"display_name": "Projects"}, Comment: "create a top-level folder"},
			{Args: map[string]any{"display_name": "Swedfund", "parent": "Inbox/01 Projects"}, Comment: "create a nested folder addressed by path"},
		},
		SeeDocs: []string{"concepts#mail-gating"},
		Handler: wrapWrite("mail.create_folder", "write", tools.NewHandleCreateFolder(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("display_name",
				mcp.Required(),
				mcp.Description("Display name for the new folder."),
			),
			mcp.WithString("parent",
				mcp.Description("Parent folder: well-known name, path (\"Inbox/01 Projects\"), top-level name, or Graph folder ID. Omit for the top level."),
			),
			// Declared, unlike the other legacy aliases, because losing it fails
			// SILENTLY: a client that strips undeclared arguments would turn a
			// nested create into a top-level create with no error. See the
			// alias-declaration rule in CR-0066.
			mcp.WithString("parent_folder_id",
				mcp.Description("Alias for `parent`."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildDeleteFolderVerb constructs the delete_folder Verb (MailManageEnabled-gated).
func buildDeleteFolderVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "delete_folder",
		Summary:     "permanently delete a mail folder and all its contents (irreversible)",
		Description: "Permanently deletes a mail folder and all messages and child folders it contains. This operation is irreversible. `folder` accepts a display-name path (\"Inbox/01 Projects\") as printed by list_folders, a top-level folder name, a well-known name, or a Graph folder ID; folder_id is accepted as an alias. Well-known folders (Inbox, Sent Items, Drafts) cannot be deleted; the Graph API rejects such requests with HTTP 400. Requires MAIL_MANAGE_ENABLED=true.",
		Examples: []tools.Example{
			{Args: map[string]any{"folder": "Inbox/01 Projects/Obsolete"}, Comment: "delete a nested folder addressed by path"},
		},
		SeeDocs: []string{"concepts#mail-gating"},
		Handler: wrapWrite("mail.delete_folder", "delete", tools.NewHandleDeleteFolder(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("folder",
				mcp.Required(),
				mcp.Description("Folder to delete: path (\"Inbox/01 Projects\"), top-level name, well-known name, or Graph folder ID."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildMoveMessageVerb constructs the move_message Verb (MailManageEnabled-gated).
func buildMoveMessageVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "move_message",
		Summary:     "move a single message to a different folder",
		Description: "Moves a single mail message to a destination folder. `destination` accepts a well-known name (\"archive\"), a display-name path (\"Inbox/01 Projects\") as printed by list_folders, a top-level folder name, or a Graph folder ID. The Graph API creates a new copy of the message in the destination folder and returns the new message ID; the original ID becomes invalid. Requires MAIL_MANAGE_ENABLED=true.",
		Examples: []tools.Example{
			{Args: map[string]any{"message_id": "AAMkAG...", "destination": "Archive"}, Comment: "archive a message by folder name"},
			{Args: map[string]any{"message_id": "AAMkAG...", "destination": "Inbox/01 Projects/Swedfund"}, Comment: "file a message into a nested folder by path"},
		},
		SeeDocs: []string{"concepts#mail-gating"},
		Handler: wrapWrite("mail.move_message", "write", tools.NewHandleMoveMessage(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_id",
				mcp.Required(),
				mcp.Description("The unique identifier of the message to move."),
			),
			mcp.WithString("destination",
				mcp.Required(),
				mcp.Description("Destination folder: well-known name, path (\"Inbox/01 Projects\"), top-level name, or Graph folder ID."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// buildMoveMessagesVerb constructs the move_messages Verb (MailManageEnabled-gated).
func buildMoveMessagesVerb(c mailVerbsConfig, rc graph.RetryConfig, wrapWrite func(string, string, mcpserver.ToolHandlerFunc) tools.Handler) tools.Verb {
	return tools.Verb{
		Name:        "move_messages",
		Summary:     "move multiple messages to a destination folder in batch",
		Description: "Moves multiple mail messages to a destination folder. `destination` is resolved once for the batch and accepts a well-known name, a display-name path (\"Inbox/01 Projects\") as printed by list_folders, a top-level folder name, or a Graph folder ID. Each message is moved individually; failures do not abort remaining moves. The output reports per-message success or failure. Accepts up to 50 comma-separated message IDs. Requires MAIL_MANAGE_ENABLED=true.",
		Examples: []tools.Example{
			{Args: map[string]any{"message_ids": "AAMkAG...,AAMkAG...", "destination": "Archive"}, Comment: "archive several messages at once"},
		},
		SeeDocs: []string{"concepts#mail-gating"},
		Handler: wrapWrite("mail.move_messages", "write", tools.NewHandleMoveMessages(rc, c.timeout)),
		Annotations: []mcp.ToolOption{
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
		},
		Schema: []mcp.ToolOption{
			mcp.WithString("message_ids",
				mcp.Required(),
				mcp.Description("Comma-separated list of message IDs to move (max 50)."),
			),
			mcp.WithString("destination",
				mcp.Required(),
				mcp.Description("Destination folder: well-known name, path (\"Inbox/01 Projects\"), top-level name, or Graph folder ID."),
			),
			mcp.WithString("account",
				mcp.Description("Account label or UPN to use. Omit to auto-select the default account."),
			),
		},
	}
}

// mailToolAnnotations returns the conservative aggregate MCP annotations for
// the mail domain tool per CR-0060 FR-9 and AC-9.
//
// readOnlyHint is false because write verbs (create_draft, create_reply_draft,
// create_forward_draft, update_draft, delete_draft) may be present when
// MailManageEnabled is true. destructiveHint is true because delete_draft
// permanently removes a message. idempotentHint is false because create_draft,
// create_reply_draft, and create_forward_draft are non-idempotent.
// openWorldHint is true because all verbs call Microsoft Graph.
//
// Per FR-9 these values represent the most conservative annotation across all
// verbs that may be registered for the domain. They remain fixed at
// construction time and must be consistent across deployment configurations.
func mailToolAnnotations() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithTitleAnnotation("Mail"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	}
}
