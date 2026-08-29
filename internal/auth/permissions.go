package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotEntitled reports that the IdP recognised the user but denied them
// access to THIS client. Distinct from a transport failure on purpose: the two
// warrant different responses, and conflating them either locks everyone out
// during an IdP outage or grants access to a user whose entitlement was pulled.
var ErrNotEntitled = errors.New("user is not entitled to this client")

// permissionsResponse is aut.hair's GET /api/client-permissions body.
type permissionsResponse struct {
	Sub         string   `json:"sub"`
	ClientID    string   `json:"client_id"`
	Teams       []string `json:"teams"`
	Permissions []string `json:"permissions"`
	Error       string   `json:"error"`
}

// permissionsTimeout bounds the lookup. It runs inline in the OIDC callback, so
// an unresponsive IdP must not hold the login handler open indefinitely.
const permissionsTimeout = 5 * time.Second

// maxPermissionsBody caps the response read. The endpoint is trusted, but a
// trusted host that starts streaming is still a way to exhaust this process.
const maxPermissionsBody = 64 << 10

// fetchClientPermissions asks the IdP what this user is entitled to for this
// client, using the access token from the code exchange.
//
// The endpoint takes no parameters — the token names both the user and the
// client — so there is nothing here to inject or spoof from a request.
//
// Returns ErrNotEntitled for a 403 carrying error="not_entitled" so the caller
// can distinguish revoked access from an unreachable IdP.
func (p *OIDCProvider) fetchClientPermissions(ctx context.Context, accessToken string) (teams, permissions []string, err error) {
	endpoint := strings.TrimSpace(p.cfg.PermissionsEndpoint)
	if endpoint == "" || accessToken == "" {
		return nil, nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, permissionsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build permissions request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("permissions request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPermissionsBody))
	if err != nil {
		return nil, nil, fmt.Errorf("read permissions response: %w", err)
	}

	var parsed permissionsResponse
	// A body that does not parse is reported as a transport-class failure
	// rather than silently yielding zero permissions, which would look
	// identical to a legitimately unprivileged user.
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil && resp.StatusCode == http.StatusOK {
		return nil, nil, fmt.Errorf("parse permissions response: %w", jsonErr)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return parsed.Teams, parsed.Permissions, nil
	case resp.StatusCode == http.StatusForbidden && parsed.Error == "not_entitled":
		return nil, nil, ErrNotEntitled
	default:
		return nil, nil, fmt.Errorf("permissions endpoint returned %d (%s)",
			resp.StatusCode, parsed.Error)
	}
}
