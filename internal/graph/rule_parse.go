// Package graph provides Graph API utilities.
//
// This file provides JSON parsing helpers that convert user-supplied JSON
// strings into MessageRulePredicatesable and MessageRuleActionsable instances
// suitable for the Graph SDK (CR-0066).
package graph

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/microsoftgraph/msgraph-sdk-go/models"
)

// ParseRulePredicates parses a JSON string into a MessageRulePredicatesable
// for use with the Graph messageRules API. The JSON structure maps directly
// to the Graph API messageRulePredicates resource type.
//
// Parameters:
//   - jsonStr: a JSON string representing conditions or exceptions.
//
// Returns the parsed predicates, or an error if the JSON is invalid.
func ParseRulePredicates(jsonStr string) (models.MessageRulePredicatesable, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	p := models.NewMessageRulePredicates()

	if v, ok := getStringSlice(raw, "senderContains"); ok {
		p.SetSenderContains(v)
	}
	if v, ok := getStringSlice(raw, "subjectContains"); ok {
		p.SetSubjectContains(v)
	}
	if v, ok := getStringSlice(raw, "bodyContains"); ok {
		p.SetBodyContains(v)
	}
	if v, ok := getStringSlice(raw, "bodyOrSubjectContains"); ok {
		p.SetBodyOrSubjectContains(v)
	}
	if v, ok := getStringSlice(raw, "headerContains"); ok {
		p.SetHeaderContains(v)
	}
	if v, ok := getStringSlice(raw, "recipientContains"); ok {
		p.SetRecipientContains(v)
	}
	if v, ok := getStringSlice(raw, "categories"); ok {
		p.SetCategories(v)
	}
	if v, ok := raw["fromAddresses"]; ok {
		recipients, err := parseRecipientList(v)
		if err != nil {
			return nil, fmt.Errorf("fromAddresses: %w", err)
		}
		p.SetFromAddresses(recipients)
	}
	if v, ok := raw["sentToAddresses"]; ok {
		recipients, err := parseRecipientList(v)
		if err != nil {
			return nil, fmt.Errorf("sentToAddresses: %w", err)
		}
		p.SetSentToAddresses(recipients)
	}
	if v, ok := raw["hasAttachments"].(bool); ok {
		p.SetHasAttachments(&v)
	}
	if v, ok := raw["sentToMe"].(bool); ok {
		p.SetSentToMe(&v)
	}
	if v, ok := raw["sentCcMe"].(bool); ok {
		p.SetSentCcMe(&v)
	}
	if v, ok := raw["sentOnlyToMe"].(bool); ok {
		p.SetSentOnlyToMe(&v)
	}
	if v, ok := raw["sentToOrCcMe"].(bool); ok {
		p.SetSentToOrCcMe(&v)
	}
	if v, ok := raw["notSentToMe"].(bool); ok {
		p.SetNotSentToMe(&v)
	}
	if v, ok := raw["isApprovalRequest"].(bool); ok {
		p.SetIsApprovalRequest(&v)
	}
	if v, ok := raw["isAutomaticForward"].(bool); ok {
		p.SetIsAutomaticForward(&v)
	}
	if v, ok := raw["isAutomaticReply"].(bool); ok {
		p.SetIsAutomaticReply(&v)
	}
	if v, ok := raw["isEncrypted"].(bool); ok {
		p.SetIsEncrypted(&v)
	}
	if v, ok := raw["isMeetingRequest"].(bool); ok {
		p.SetIsMeetingRequest(&v)
	}
	if v, ok := raw["isMeetingResponse"].(bool); ok {
		p.SetIsMeetingResponse(&v)
	}
	if v, ok := raw["isNonDeliveryReport"].(bool); ok {
		p.SetIsNonDeliveryReport(&v)
	}
	if v, ok := raw["isReadReceipt"].(bool); ok {
		p.SetIsReadReceipt(&v)
	}
	if v, ok := raw["isSigned"].(bool); ok {
		p.SetIsSigned(&v)
	}
	if v, ok := raw["isVoicemail"].(bool); ok {
		p.SetIsVoicemail(&v)
	}
	if v, ok := raw["importance"].(string); ok {
		imp, err := models.ParseImportance(v)
		if err != nil {
			return nil, fmt.Errorf("importance: %w", err)
		}
		if imp != nil {
			cast := imp.(*models.Importance)
			p.SetImportance(cast)
		}
	}
	if v, ok := raw["sensitivity"].(string); ok {
		sens, err := models.ParseSensitivity(v)
		if err != nil {
			return nil, fmt.Errorf("sensitivity: %w", err)
		}
		if sens != nil {
			cast := sens.(*models.Sensitivity)
			p.SetSensitivity(cast)
		}
	}

	return p, nil
}

// ParseRuleActions parses a JSON string into a MessageRuleActionsable for use
// with the Graph messageRules API. The JSON structure maps directly to the
// Graph API messageRuleActions resource type.
//
// Parameters:
//   - jsonStr: a JSON string representing rule actions.
//
// Returns the parsed actions, or an error if the JSON is invalid.
func ParseRuleActions(jsonStr string) (models.MessageRuleActionsable, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	a := models.NewMessageRuleActions()

	if v, ok := raw["moveToFolder"].(string); ok {
		a.SetMoveToFolder(&v)
	}
	if v, ok := raw["copyToFolder"].(string); ok {
		a.SetCopyToFolder(&v)
	}
	if v, ok := raw["delete"].(bool); ok {
		a.SetDelete(&v)
	}
	if v, ok := raw["permanentDelete"].(bool); ok {
		a.SetPermanentDelete(&v)
	}
	if v, ok := raw["markAsRead"].(bool); ok {
		a.SetMarkAsRead(&v)
	}
	if v, ok := raw["stopProcessingRules"].(bool); ok {
		a.SetStopProcessingRules(&v)
	}
	if v, ok := getStringSlice(raw, "assignCategories"); ok {
		a.SetAssignCategories(v)
	}
	if v, ok := raw["markImportance"].(string); ok {
		imp, err := models.ParseImportance(v)
		if err != nil {
			return nil, fmt.Errorf("markImportance: %w", err)
		}
		if imp != nil {
			cast := imp.(*models.Importance)
			a.SetMarkImportance(cast)
		}
	}
	if v, ok := raw["forwardTo"]; ok {
		recipients, err := parseRecipientList(v)
		if err != nil {
			return nil, fmt.Errorf("forwardTo: %w", err)
		}
		a.SetForwardTo(recipients)
	}
	if v, ok := raw["forwardAsAttachmentTo"]; ok {
		recipients, err := parseRecipientList(v)
		if err != nil {
			return nil, fmt.Errorf("forwardAsAttachmentTo: %w", err)
		}
		a.SetForwardAsAttachmentTo(recipients)
	}
	if v, ok := raw["redirectTo"]; ok {
		recipients, err := parseRecipientList(v)
		if err != nil {
			return nil, fmt.Errorf("redirectTo: %w", err)
		}
		a.SetRedirectTo(recipients)
	}

	return a, nil
}

// getStringSlice extracts a []string from a map[string]any value that may be
// []any (from JSON unmarshal) or []string.
func getStringSlice(m map[string]any, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	switch typed := v.(type) {
	case []string:
		return typed, true
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, sOK := item.(string); sOK {
				result = append(result, s)
			}
		}
		return result, len(result) > 0
	}
	return nil, false
}

// parseRecipientList converts a JSON value (expected to be an array of objects
// with emailAddress.address fields, or an array of plain email strings) into
// a slice of models.Recipientable.
func parseRecipientList(v any) ([]models.Recipientable, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array, got %T", v)
	}
	recipients := make([]models.Recipientable, 0, len(arr))
	for _, item := range arr {
		switch typed := item.(type) {
		case string:
			// Plain email string shorthand.
			recipients = append(recipients, buildRecipient(typed, ""))
		case map[string]any:
			// Graph API format: {"emailAddress": {"address": "...", "name": "..."}}
			if ea, eaOK := typed["emailAddress"].(map[string]any); eaOK {
				addr, _ := ea["address"].(string)
				name, _ := ea["name"].(string)
				if addr != "" {
					recipients = append(recipients, buildRecipient(addr, name))
				}
			} else if addr, addrOK := typed["address"].(string); addrOK {
				// Flat format: {"address": "...", "name": "..."}
				name, _ := typed["name"].(string)
				recipients = append(recipients, buildRecipient(addr, name))
			}
		}
	}
	return recipients, nil
}

// buildRecipient creates a models.Recipientable from an email address and
// optional display name.
func buildRecipient(address, name string) models.Recipientable {
	address = strings.TrimSpace(address)
	ea := models.NewEmailAddress()
	ea.SetAddress(&address)
	if name != "" {
		ea.SetName(&name)
	}
	r := models.NewRecipient()
	r.SetEmailAddress(ea)
	return r
}
