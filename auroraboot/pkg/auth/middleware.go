package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// ContextKeyNodeID is the key used to store the authenticated node ID in the echo context.
const ContextKeyNodeID = "nodeID"

// maxRegistrationBodyBytes caps the unauthenticated registration body: it
// carries a token and a little node metadata, a few KiB at most.
const maxRegistrationBodyBytes = 64 * 1024

// secureCompare reports whether a and b are equal using a constant-time
// comparison, so a bearer-token / admin-password check does not leak, via
// response timing, how many leading bytes of the secret matched. Plain string
// == returns early on the first differing byte; subtle.ConstantTimeCompare does
// not, and returns 0 for unequal-length inputs without a data-dependent branch.
func secureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// AuthNodeID returns the authenticated node ID set by NodeAPIKeyMiddleware, or
// "" when the request was not authenticated as a node (e.g. an admin-bearer
// request). Handlers shared between the agent and admin groups use the empty
// return to distinguish the two callers.
func AuthNodeID(c echo.Context) string {
	id, _ := c.Get(ContextKeyNodeID).(string)
	return id
}

// RequireNodeMatch enforces that the authenticated node (set by
// NodeAPIKeyMiddleware) matches the nodeID path parameter. It is meant to wrap
// agent-group routes carrying a :nodeID segment so a node can only act on its
// own resources — preventing one registered node from impersonating another
// (BOLA). Returns 403 on mismatch and 401 if no node identity is present (the
// middleware should always run first, so the latter is defence in depth).
func RequireNodeMatch(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authID := AuthNodeID(c)
		if authID == "" {
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if c.Param("nodeID") != authID {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
		}
		return next(c)
	}
}

// AdminMiddleware lets through a request that AdminAuth accepts: the admin
// password as a Bearer header, or a live session cookie.
func AdminMiddleware(a *AdminAuth) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch a.check(c) {
			case isAdmin:
				return next(c)
			case foreignOrigin:
				return forbidden(c)
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
	}
}

// NodeAPIKeyMiddleware returns an Echo middleware that checks the Authorization header
// for a Bearer token matching a node's API key. On success it sets the node ID in the context.
func NodeAPIKeyMiddleware(nodeStore store.NodeStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token := extractBearer(c.Request().Header.Get("Authorization"))
			if token == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			node, err := nodeStore.GetByAPIKey(c.Request().Context(), token)
			if err != nil || node == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			c.Set(ContextKeyNodeID, node.ID)
			return next(c)
		}
	}
}

// AgentOrAdminMiddleware authenticates a request as EITHER an admin (the
// password as a Bearer header, or a live session cookie) OR a node (a Bearer
// token that is a node API key). It is used for the two routes that are
// legitimately shared between the agent and the admin/UI: GET
// /nodes/:nodeID/commands and PUT /nodes/:nodeID/commands/:commandID/status.
//
// On a node match it sets ContextKeyNodeID so downstream handlers can tell the
// caller is a node and enforce node-scoping (RequireNodeMatch / node-scoped
// store updates). On an admin match it sets nothing, leaving AuthNodeID empty,
// so admins act across nodes. Admin is checked first: the admin password is not
// a valid node API key, so the order is unambiguous.
//
// This middleware exists to avoid registering the same path twice (once per
// auth group), which Echo resolves by silently shadowing the first
// registration, routing agent command polls into the admin-only handler and
// answering 401 to every agent.
func AgentOrAdminMiddleware(a *AdminAuth, nodeStore store.NodeStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch a.check(c) {
			case isAdmin:
				return next(c)
			case foreignOrigin:
				return forbidden(c)
			}
			token := BearerToken(c)
			if token == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			node, err := nodeStore.GetByAPIKey(c.Request().Context(), token)
			if err != nil || node == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			c.Set(ContextKeyNodeID, node.ID)
			return next(c)
		}
	}
}

// ArtifactImageMiddleware authorizes GET /api/v1/artifacts/:id/image for either an
// admin or a node that has been assigned this artifact by an upgrade command.
//
// The container image is the one build artifact a node legitimately downloads: an
// operator queues an upgrade / upgrade-recovery command whose source is
// "artifact:<id>", the node polls it, and the agent pulls that artifact's image
// with its node API key. So a node is authorized for exactly the artifacts some
// command told it to install, never an arbitrary one. This stops a node (or a
// leaked node API key) from pulling every image the fleet has ever built, which
// may embed a cloud-config and secrets.
//
// An admin is accepted by Bearer header or session cookie (see AdminAuth), never
// by ?token=. A node API key is accepted only from the Authorization header: a
// credential in a URL would leak through access logs, proxies, browser history
// and Referer headers.
func ArtifactImageMiddleware(a *AdminAuth, nodeStore store.NodeStore, commandStore store.CommandStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch a.check(c) {
			case isAdmin:
				return next(c)
			case foreignOrigin:
				return forbidden(c)
			}
			// Otherwise the caller must be a node, authenticated only via the
			// Authorization header, and only for an artifact a command assigned it.
			headerToken := BearerToken(c)
			if headerToken == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			node, err := nodeStore.GetByAPIKey(c.Request().Context(), headerToken)
			if err != nil || node == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			if !nodeAssignedArtifact(c.Request().Context(), commandStore, node.ID, c.Param("id")) {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
			}
			c.Set(ContextKeyNodeID, node.ID)
			return next(c)
		}
	}
}

// ExtensionDownloadMiddleware authorizes GET
// /api/v1/extensions/:id/download/:filename for an admin, for a caller holding
// that extension's own download token, or for any authenticated node.
//
// Three credentials, with different scopes:
//
//   - An admin, by Bearer header or session cookie (see AdminAuth). The UI
//     downloads the .raw with the session cookie the browser sends on its own.
//   - The extension's own DownloadToken, over ?token= only. This is what the
//     install command's "source" URL carries. That URL is pushed to every node
//     in the selector, kept in the commands table and shown in the UI's command
//     preview, so it must not be the admin password: this token reads one
//     extension's .raw and nothing else. An empty DownloadToken never
//     authorizes, so a record written before the column existed fails closed
//     rather than opening the route to an empty ?token=.
//   - A node API key, from the Authorization header only, for the reason spelled
//     out on ArtifactImageMiddleware.
//
// Per-command scoping analogous to nodeAssignedArtifact (a node may pull only
// an extension some assigned command names) is a follow-up; today any
// registered node may fetch any extension.
//
// extensionStore may be nil, which simply disables the per-extension branch.
func ExtensionDownloadMiddleware(a *AdminAuth, nodeStore store.NodeStore, extensionStore store.ExtensionStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch a.check(c) {
			case isAdmin:
				return next(c)
			case foreignOrigin:
				return forbidden(c)
			}
			// This extension's own download token, from ?token=. Scoped to the
			// :id in the path, so presenting one extension's token for another
			// is rejected.
			if extensionDownloadTokenMatches(c, extensionStore, c.QueryParam("token")) {
				return next(c)
			}
			// Otherwise the caller must be a node, authenticated over the
			// header only.
			headerToken := BearerToken(c)
			if headerToken == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			node, err := nodeStore.GetByAPIKey(c.Request().Context(), headerToken)
			if err != nil || node == nil {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			c.Set(ContextKeyNodeID, node.ID)
			return next(c)
		}
	}
}

// extensionDownloadTokenMatches reports whether token is the DownloadToken of
// the extension named by the request's :id. A missing store, a missing record,
// a store error, an empty presented token or an empty stored token all fail
// closed.
//
// Both emptiness checks matter because secureCompare is a constant-time byte
// compare: "" equals "", so a record written before the column existed would
// otherwise be downloadable by anyone appending a bare "?token=". Either check
// alone closes that, and they are kept as a pair on purpose — the early return
// also spares every header-authenticated node a pointless store lookup.
func extensionDownloadTokenMatches(c echo.Context, extensionStore store.ExtensionStore, token string) bool {
	if extensionStore == nil || token == "" {
		return false
	}
	id := c.Param("id")
	if id == "" {
		return false
	}
	rec, err := extensionStore.GetByID(c.Request().Context(), id)
	if err != nil || rec == nil || rec.DownloadToken == "" {
		return false
	}
	return secureCompare(token, rec.DownloadToken)
}

// nodeAssignedArtifact reports whether some upgrade / upgrade-recovery command
// queued for nodeID names this artifact as its source ("artifact:<id>"). It does
// not filter on command phase: once an operator has told a node to install an
// artifact, re-fetching that same image (a retry, a resumed upgrade) is not a new
// exposure — the boundary being enforced is WHICH artifact, not how many times. A
// store error fails closed (treated as not assigned).
func nodeAssignedArtifact(ctx context.Context, commandStore store.CommandStore, nodeID, artifactID string) bool {
	if commandStore == nil || nodeID == "" || artifactID == "" {
		return false
	}
	cmds, err := commandStore.ListByNode(ctx, nodeID)
	if err != nil {
		return false
	}
	want := "artifact:" + artifactID
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if (cmd.Command == store.CmdUpgrade || cmd.Command == store.CmdUpgradeRecovery) && cmd.Args["source"] == want {
			return true
		}
	}
	return false
}

// RegistrationTokenAuth returns an Echo middleware that reads the JSON body,
// checks for a "registrationToken" field matching the expected token, and
// resets the body so downstream handlers can read it again. The body is read
// through a limit, so a client without a token can make it hold no more than
// maxRegistrationBodyBytes; a larger body is refused with 413.
//
// The token is a shared holder because SettingsHandler.RotateRegistrationToken
// replaces it at runtime; the middleware must read the current value on every
// request, otherwise rotated tokens wouldn't actually invalidate old ones
// (a pre-rotation registration would keep succeeding against the middleware's
// captured copy).
func RegistrationTokenAuth(token *RegistrationToken) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().ContentLength > maxRegistrationBodyBytes {
				return registrationBodyTooLarge(c)
			}
			bodyBytes, err := io.ReadAll(io.LimitReader(c.Request().Body, maxRegistrationBodyBytes+1))
			if err != nil {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "cannot read body"})
			}
			if len(bodyBytes) > maxRegistrationBodyBytes {
				return registrationBodyTooLarge(c)
			}

			var body struct {
				RegistrationToken string `json:"registrationToken"`
			}
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				c.Request().Body = io.NopCloser(bytes.NewReader(bodyBytes))
				return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			}

			// Reset body for downstream handler
			c.Request().Body = io.NopCloser(bytes.NewReader(bodyBytes))

			current := ""
			if token != nil {
				current = token.Get()
			}
			if !secureCompare(body.RegistrationToken, current) {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid registration token"})
			}
			return next(c)
		}
	}
}

func registrationBodyTooLarge(c echo.Context) error {
	return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
}

func extractBearer(header string) string {
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return ""
}
