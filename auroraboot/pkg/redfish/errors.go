package redfish

import (
	"errors"
	"fmt"
	"strings"

	"github.com/stmcginnis/gofish/schemas"
)

// enrichRedfishError augments a gofish error with any structured Redfish
// ExtendedInfo it carries. gofish surfaces BMC error bodies as a *schemas.Error
// whose Error() string is just "<status>: <raw JSON>" — technically complete but
// hard to read and easy to miss the actionable parts. When the body contains
// @Message.ExtendedInfo entries, the Redfish spec puts the human-useful detail
// there (Message, MessageId, Resolution). This pulls those out and appends a
// compact, readable summary so the wrapped error a caller sees is genuinely
// diagnostic rather than a bare status code.
//
// If err is not a Redfish error, or carries no ExtendedInfo, it is returned
// unchanged. The result is always run through scrub at the call site, so this must
// not re-introduce credentials (it only ever copies BMC-authored message text,
// never our request).
func enrichRedfishError(err error) error {
	if err == nil {
		return nil
	}
	var rfErr *schemas.Error
	if !errors.As(err, &rfErr) {
		return err
	}
	summary := extendedInfoSummary(rfErr.ExtendedInfos)
	if summary == "" {
		return err
	}
	return fmt.Errorf("%w [%s]", err, summary)
}

// extendedInfoSummary renders the actionable fields of the Redfish
// @Message.ExtendedInfo array into a single compact string. Empty entries are
// skipped; an all-empty array yields "".
func extendedInfoSummary(infos []schemas.ErrExtendedInfo) string {
	parts := make([]string, 0, len(infos))
	for _, info := range infos {
		var b strings.Builder
		if info.Message != "" {
			b.WriteString(info.Message)
		}
		if info.MessageID != "" {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "(%s)", info.MessageID)
		}
		if info.Resolution != "" {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "resolution: %s", info.Resolution)
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "; ")
}
