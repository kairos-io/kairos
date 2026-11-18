package handlers

import (
	"net/http"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/labstack/echo/v4"
)

// quirkProfileResponse is the JSON shape the UI consumes to populate the BMC
// vendor selector from the profiles actually loaded at server start (built-in +
// operator), each with its project-derived support tier. It deliberately carries
// no quirks/hook detail — only what an operator needs to pick a profile.
type quirkProfileResponse struct {
	// Name is the selection name a BMCTarget.Vendor matches.
	Name string `json:"name"`
	// Tier is the support tier letter (A/B/C).
	Tier string `json:"tier"`
	// TierDescription is the human-readable tier annotation for a badge/tooltip.
	TierDescription string `json:"tierDescription"`
	// Origin is "builtin" or "operator".
	Origin string `json:"origin"`
	// ValidatedFirmware is the informational firmware label, when the profile
	// declared one (empty otherwise).
	ValidatedFirmware string `json:"validatedFirmware,omitempty"`
}

// ListQuirkProfiles returns the Redfish quirk profiles loaded into this server
// (the built-in profiles plus any from --redfish-quirks-dir), sorted by name, so
// the UI can offer them as vendor options with a tier badge instead of a hardcoded
// list. Read-only; consults the process-wide registry installed at start.
func (h *DeployHandler) ListQuirkProfiles(c echo.Context) error {
	infos := redfish.DefaultRegistry().Profiles()
	out := make([]quirkProfileResponse, 0, len(infos))
	for _, p := range infos {
		out = append(out, quirkProfileResponse{
			Name:              p.Name,
			Tier:              string(p.Tier),
			TierDescription:   p.TierDescription,
			Origin:            p.Origin,
			ValidatedFirmware: p.ValidatedFirmware,
		})
	}
	return c.JSON(http.StatusOK, out)
}
