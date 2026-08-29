package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/austinkregel/backup-server/internal/config"
)

func providerWith(cfg config.OIDCConfig) *OIDCProvider {
	return &OIDCProvider{cfg: cfg}
}

func TestFetchClientPermissions_Success(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"1","client_id":"9","teams":["3"],"permissions":["read","delete"]}`))
	}))
	defer srv.Close()

	p := providerWith(config.OIDCConfig{PermissionsEndpoint: srv.URL})
	teams, perms, err := p.fetchClientPermissions(context.Background(), "tok-abc")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok-abc")
	}
	if len(teams) != 1 || teams[0] != "3" {
		t.Errorf("teams = %v, want [3]", teams)
	}
	if len(perms) != 2 || perms[1] != "delete" {
		t.Errorf("permissions = %v, want [read delete]", perms)
	}
}

// A revoked entitlement must be distinguishable from an unreachable IdP: the
// two get different treatment at the callback.
func TestFetchClientPermissions_NotEntitled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"not_entitled","sub":"1","client_id":"9"}`))
	}))
	defer srv.Close()

	p := providerWith(config.OIDCConfig{PermissionsEndpoint: srv.URL})
	_, _, err := p.fetchClientPermissions(context.Background(), "tok")
	if !errors.Is(err, ErrNotEntitled) {
		t.Fatalf("err = %v, want ErrNotEntitled", err)
	}
}

func TestFetchClientPermissions_ServerErrorIsNotEntitlementDenial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := providerWith(config.OIDCConfig{PermissionsEndpoint: srv.URL})
	_, _, err := p.fetchClientPermissions(context.Background(), "tok")
	if err == nil {
		t.Fatal("err = nil, want an error")
	}
	if errors.Is(err, ErrNotEntitled) {
		t.Error("a 500 must not be reported as ErrNotEntitled; that would lock users out during an IdP outage")
	}
}

// Malformed JSON on a 200 must be an error, not an empty permission set that
// reads as a legitimately unprivileged user.
func TestFetchClientPermissions_MalformedBodyIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	p := providerWith(config.OIDCConfig{PermissionsEndpoint: srv.URL})
	_, _, err := p.fetchClientPermissions(context.Background(), "tok")
	if err == nil {
		t.Fatal("err = nil, want a parse error")
	}
}

func TestFetchClientPermissions_DisabledWhenUnconfigured(t *testing.T) {
	p := providerWith(config.OIDCConfig{})
	teams, perms, err := p.fetchClientPermissions(context.Background(), "tok")
	if err != nil || teams != nil || perms != nil {
		t.Fatalf("got (%v, %v, %v), want all nil when no endpoint is configured", teams, perms, err)
	}
}

func TestIsAdmin_PermissionAndGroup(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.OIDCConfig
		user *SessionUser
		want bool
	}{
		{"permission matches", config.OIDCConfig{AdminPermission: "delete"},
			&SessionUser{Permissions: []string{"read", "delete"}}, true},
		{"permission match is case-insensitive", config.OIDCConfig{AdminPermission: "Delete"},
			&SessionUser{Permissions: []string{"delete"}}, true},
		{"permission absent", config.OIDCConfig{AdminPermission: "delete"},
			&SessionUser{Permissions: []string{"read"}}, false},
		{"group still works alongside", config.OIDCConfig{AdminPermission: "delete", AdminGroup: "3"},
			&SessionUser{Groups: []string{"3"}}, true},
		{"neither configured denies", config.OIDCConfig{},
			&SessionUser{Permissions: []string{"delete"}, Groups: []string{"3"}}, false},
		{"nil user denies", config.OIDCConfig{AdminPermission: "delete"}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerWith(tt.cfg).IsAdmin(tt.user); got != tt.want {
				t.Errorf("IsAdmin = %v, want %v", got, tt.want)
			}
		})
	}
}
