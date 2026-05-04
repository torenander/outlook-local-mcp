// Package graph provides Graph API utilities.
//
// This file provides serialization helpers for the messageRule resource type
// used by the mail rule verbs (CR-0066). Three tiers are supported: raw (full
// Graph API shape), summary (curated field set), and a conditions/actions
// summarizer for text output.
package graph

import (
	"fmt"
	"strings"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// SerializeRule converts a MessageRuleable into a map[string]any containing
// all fields from the Graph API response. This is the "raw" output tier.
//
// Parameters:
//   - rule: a models.MessageRuleable from the Graph SDK. Must not be nil.
//
// Returns a map suitable for JSON marshalling.
func SerializeRule(rule models.MessageRuleable) map[string]any {
	m := map[string]any{
		"id":          SafeStr(rule.GetId()),
		"displayName": SafeStr(rule.GetDisplayName()),
		"sequence":    SafeInt32(rule.GetSequence()),
		"isEnabled":   SafeBool(rule.GetIsEnabled()),
		"isReadOnly":  SafeBool(rule.GetIsReadOnly()),
		"hasError":    SafeBool(rule.GetHasError()),
	}
	if c := rule.GetConditions(); c != nil {
		m["conditions"] = serializePredicates(c)
	}
	if a := rule.GetActions(); a != nil {
		m["actions"] = serializeActions(a)
	}
	if e := rule.GetExceptions(); e != nil {
		m["exceptions"] = serializePredicates(e)
	}
	return m
}

// SerializeSummaryRule converts a MessageRuleable into a compact map[string]any
// with a curated field set. This is the "summary" output tier.
//
// Parameters:
//   - rule: a models.MessageRuleable from the Graph SDK. Must not be nil.
//
// Returns a map suitable for JSON marshalling.
func SerializeSummaryRule(rule models.MessageRuleable) map[string]any {
	m := map[string]any{
		"id":                SafeStr(rule.GetId()),
		"displayName":       SafeStr(rule.GetDisplayName()),
		"sequence":          SafeInt32(rule.GetSequence()),
		"isEnabled":         SafeBool(rule.GetIsEnabled()),
		"conditionsSummary": SummarizePredicates(rule.GetConditions()),
		"actionsSummary":    SummarizeActions(rule.GetActions()),
	}
	return m
}

// SummarizePredicates returns a human-readable one-line summary of the
// conditions or exceptions in a MessageRulePredicatesable.
//
// Parameters:
//   - p: a MessageRulePredicatesable. May be nil.
//
// Returns a summary string, or "(none)" if p is nil or empty.
func SummarizePredicates(p models.MessageRulePredicatesable) string {
	if p == nil {
		return "(none)"
	}
	var parts []string
	if v := p.GetSenderContains(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("senderContains:%s", strings.Join(v, ",")))
	}
	if v := p.GetSubjectContains(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("subjectContains:%s", strings.Join(v, ",")))
	}
	if v := p.GetBodyContains(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("bodyContains:%s", strings.Join(v, ",")))
	}
	if v := p.GetBodyOrSubjectContains(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("bodyOrSubjectContains:%s", strings.Join(v, ",")))
	}
	if v := p.GetHeaderContains(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("headerContains:%s", strings.Join(v, ",")))
	}
	if v := p.GetRecipientContains(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("recipientContains:%s", strings.Join(v, ",")))
	}
	if v := p.GetFromAddresses(); len(v) > 0 {
		addrs := make([]string, 0, len(v))
		for _, r := range v {
			if ea := r.GetEmailAddress(); ea != nil {
				addrs = append(addrs, SafeStr(ea.GetAddress()))
			}
		}
		if len(addrs) > 0 {
			parts = append(parts, fmt.Sprintf("from:%s", strings.Join(addrs, ",")))
		}
	}
	if SafeBool(p.GetHasAttachments()) {
		parts = append(parts, "hasAttachments")
	}
	if SafeBool(p.GetSentToMe()) {
		parts = append(parts, "sentToMe")
	}
	if SafeBool(p.GetSentOnlyToMe()) {
		parts = append(parts, "sentOnlyToMe")
	}
	if imp := p.GetImportance(); imp != nil {
		parts = append(parts, fmt.Sprintf("importance:%s", imp.String()))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, "; ")
}

// SummarizeActions returns a human-readable one-line summary of the actions in
// a MessageRuleActionsable.
//
// Parameters:
//   - a: a MessageRuleActionsable. May be nil.
//
// Returns a summary string, or "(none)" if a is nil or empty.
func SummarizeActions(a models.MessageRuleActionsable) string {
	if a == nil {
		return "(none)"
	}
	var parts []string
	if v := SafeStr(a.GetMoveToFolder()); v != "" {
		parts = append(parts, fmt.Sprintf("moveTo:%s", v))
	}
	if v := SafeStr(a.GetCopyToFolder()); v != "" {
		parts = append(parts, fmt.Sprintf("copyTo:%s", v))
	}
	if SafeBool(a.GetDelete()) {
		parts = append(parts, "delete")
	}
	if SafeBool(a.GetPermanentDelete()) {
		parts = append(parts, "permanentDelete")
	}
	if SafeBool(a.GetMarkAsRead()) {
		parts = append(parts, "markAsRead")
	}
	if imp := a.GetMarkImportance(); imp != nil {
		parts = append(parts, fmt.Sprintf("markImportance:%s", imp.String()))
	}
	if v := a.GetAssignCategories(); len(v) > 0 {
		parts = append(parts, fmt.Sprintf("categories:%s", strings.Join(v, ",")))
	}
	if v := a.GetForwardTo(); len(v) > 0 {
		addrs := recipientAddresses(v)
		if len(addrs) > 0 {
			parts = append(parts, fmt.Sprintf("forwardTo:%s", strings.Join(addrs, ",")))
		}
	}
	if v := a.GetRedirectTo(); len(v) > 0 {
		addrs := recipientAddresses(v)
		if len(addrs) > 0 {
			parts = append(parts, fmt.Sprintf("redirectTo:%s", strings.Join(addrs, ",")))
		}
	}
	if SafeBool(a.GetStopProcessingRules()) {
		parts = append(parts, "stopProcessingRules")
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, "; ")
}

// recipientAddresses extracts email addresses from a slice of Recipientable.
func recipientAddresses(recipients []models.Recipientable) []string {
	addrs := make([]string, 0, len(recipients))
	for _, r := range recipients {
		if ea := r.GetEmailAddress(); ea != nil {
			if addr := SafeStr(ea.GetAddress()); addr != "" {
				addrs = append(addrs, addr)
			}
		}
	}
	return addrs
}

// serializePredicates converts a MessageRulePredicatesable into a map[string]any
// containing all non-nil fields.
func serializePredicates(p models.MessageRulePredicatesable) map[string]any {
	if p == nil {
		return nil
	}
	m := make(map[string]any)
	if v := p.GetBodyContains(); v != nil {
		m["bodyContains"] = v
	}
	if v := p.GetBodyOrSubjectContains(); v != nil {
		m["bodyOrSubjectContains"] = v
	}
	if v := p.GetCategories(); v != nil {
		m["categories"] = v
	}
	if v := p.GetFromAddresses(); v != nil {
		m["fromAddresses"] = serializeRecipientList(v)
	}
	if SafeBool(p.GetHasAttachments()) {
		m["hasAttachments"] = true
	}
	if v := p.GetHeaderContains(); v != nil {
		m["headerContains"] = v
	}
	if v := p.GetImportance(); v != nil {
		m["importance"] = v.String()
	}
	if SafeBool(p.GetIsApprovalRequest()) {
		m["isApprovalRequest"] = true
	}
	if SafeBool(p.GetIsAutomaticForward()) {
		m["isAutomaticForward"] = true
	}
	if SafeBool(p.GetIsAutomaticReply()) {
		m["isAutomaticReply"] = true
	}
	if SafeBool(p.GetIsEncrypted()) {
		m["isEncrypted"] = true
	}
	if SafeBool(p.GetIsMeetingRequest()) {
		m["isMeetingRequest"] = true
	}
	if SafeBool(p.GetIsMeetingResponse()) {
		m["isMeetingResponse"] = true
	}
	if SafeBool(p.GetIsNonDeliveryReport()) {
		m["isNonDeliveryReport"] = true
	}
	if SafeBool(p.GetIsPermissionControlled()) {
		m["isPermissionControlled"] = true
	}
	if SafeBool(p.GetIsReadReceipt()) {
		m["isReadReceipt"] = true
	}
	if SafeBool(p.GetIsSigned()) {
		m["isSigned"] = true
	}
	if SafeBool(p.GetIsVoicemail()) {
		m["isVoicemail"] = true
	}
	if v := p.GetMessageActionFlag(); v != nil {
		m["messageActionFlag"] = v.String()
	}
	if SafeBool(p.GetNotSentToMe()) {
		m["notSentToMe"] = true
	}
	if v := p.GetRecipientContains(); v != nil {
		m["recipientContains"] = v
	}
	if v := p.GetSenderContains(); v != nil {
		m["senderContains"] = v
	}
	if v := p.GetSensitivity(); v != nil {
		m["sensitivity"] = v.String()
	}
	if SafeBool(p.GetSentCcMe()) {
		m["sentCcMe"] = true
	}
	if SafeBool(p.GetSentOnlyToMe()) {
		m["sentOnlyToMe"] = true
	}
	if SafeBool(p.GetSentToMe()) {
		m["sentToMe"] = true
	}
	if SafeBool(p.GetSentToOrCcMe()) {
		m["sentToOrCcMe"] = true
	}
	if v := p.GetSentToAddresses(); v != nil {
		m["sentToAddresses"] = serializeRecipientList(v)
	}
	if v := p.GetSubjectContains(); v != nil {
		m["subjectContains"] = v
	}
	return m
}

// serializeActions converts a MessageRuleActionsable into a map[string]any
// containing all non-nil/non-default fields.
func serializeActions(a models.MessageRuleActionsable) map[string]any {
	if a == nil {
		return nil
	}
	m := make(map[string]any)
	if v := a.GetAssignCategories(); v != nil {
		m["assignCategories"] = v
	}
	if v := SafeStr(a.GetCopyToFolder()); v != "" {
		m["copyToFolder"] = v
	}
	if SafeBool(a.GetDelete()) {
		m["delete"] = true
	}
	if v := a.GetForwardAsAttachmentTo(); v != nil {
		m["forwardAsAttachmentTo"] = serializeRecipientList(v)
	}
	if v := a.GetForwardTo(); v != nil {
		m["forwardTo"] = serializeRecipientList(v)
	}
	if SafeBool(a.GetMarkAsRead()) {
		m["markAsRead"] = true
	}
	if v := a.GetMarkImportance(); v != nil {
		m["markImportance"] = v.String()
	}
	if v := SafeStr(a.GetMoveToFolder()); v != "" {
		m["moveToFolder"] = v
	}
	if SafeBool(a.GetPermanentDelete()) {
		m["permanentDelete"] = true
	}
	if v := a.GetRedirectTo(); v != nil {
		m["redirectTo"] = serializeRecipientList(v)
	}
	if SafeBool(a.GetStopProcessingRules()) {
		m["stopProcessingRules"] = true
	}
	return m
}

// serializeRecipientList converts a slice of Recipientable into a slice of
// maps with "name" and "address" keys.
func serializeRecipientList(recipients []models.Recipientable) []map[string]string {
	result := make([]map[string]string, 0, len(recipients))
	for _, r := range recipients {
		if ea := r.GetEmailAddress(); ea != nil {
			result = append(result, map[string]string{
				"name":    SafeStr(ea.GetName()),
				"address": SafeStr(ea.GetAddress()),
			})
		}
	}
	return result
}
