package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	csrf "filippo.io/csrf/gorilla"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
)

func setupIDPTest(t *testing.T) (*AdminHandler, *gorm.DB, *scs.SessionManager) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := &config.Config{BcryptCost: 4, OIDCEnabled: true}
	h := NewAdminHandler(db, cfg, &mockStorage{})
	return h, db, scs.New()
}

func makeIDPRequest(t *testing.T, h *AdminHandler, sm *scs.SessionManager, admin *models.User, targetID uint, idp string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("identity_provider", idp)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/admin/users/%d/idp", targetID),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, admin)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatUint(uint64(targetID), 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(h.UpdateUserIDP)).ServeHTTP(w, req)
	return w
}

func TestUpdateUserIDP_SwitchToOIDC(t *testing.T) {
	h, db, sm := setupIDPTest(t)
	admin := createAdminTestUser(t, db, "admin", "admin@example.com", true)
	target := createAdminTestUser(t, db, "bob", "bob@example.com", false)

	w := makeIDPRequest(t, h, sm, admin, target.ID, "oidc")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.User
	db.First(&updated, target.ID)
	if updated.IdentityProvider != "oidc" {
		t.Errorf("IdentityProvider: want oidc, got %q", updated.IdentityProvider)
	}
	if updated.OIDCSubject != "" {
		t.Errorf("OIDCSubject should be cleared on IDP switch, got %q", updated.OIDCSubject)
	}
}

func TestUpdateUserIDP_SwitchBackToInternal(t *testing.T) {
	h, db, sm := setupIDPTest(t)
	admin := createAdminTestUser(t, db, "admin", "admin@example.com", true)

	// Start as OIDC user with a linked subject
	target := &models.User{
		Username: "carol", Email: "carol@example.com",
		IdentityProvider: "oidc", OIDCSubject: "sub-carol",
	}
	db.Create(target)

	w := makeIDPRequest(t, h, sm, admin, target.ID, "internal")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.User
	db.First(&updated, target.ID)
	if updated.IdentityProvider != "internal" {
		t.Errorf("IdentityProvider: want internal, got %q", updated.IdentityProvider)
	}
	if updated.OIDCSubject != "" {
		t.Errorf("OIDCSubject should be cleared when switching to internal, got %q", updated.OIDCSubject)
	}
}

func TestUpdateUserIDP_CannotSwitchSelfToOIDC(t *testing.T) {
	h, db, sm := setupIDPTest(t)
	admin := createAdminTestUser(t, db, "admin", "admin@example.com", true)

	w := makeIDPRequest(t, h, sm, admin, admin.ID, "oidc")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 when switching own account to OIDC, got %d", w.Code)
	}

	// Verify not changed
	var unchanged models.User
	db.First(&unchanged, admin.ID)
	if unchanged.IdentityProvider != "internal" {
		t.Errorf("admin's own IDP should not have changed, got %q", unchanged.IdentityProvider)
	}
}

func TestUpdateUserIDP_InvalidProvider(t *testing.T) {
	h, db, sm := setupIDPTest(t)
	admin := createAdminTestUser(t, db, "admin", "admin@example.com", true)
	target := createAdminTestUser(t, db, "bob", "bob@example.com", false)

	w := makeIDPRequest(t, h, sm, admin, target.ID, "saml")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for unknown provider, got %d", w.Code)
	}
}

func TestUpdateUserIDP_ClearsSubjectOnIDPSwitch(t *testing.T) {
	// Switching to OIDC when already OIDC (e.g., re-link) should clear subject
	h, db, sm := setupIDPTest(t)
	admin := createAdminTestUser(t, db, "admin", "admin@example.com", true)

	target := &models.User{
		Username: "dave", Email: "dave@example.com",
		IdentityProvider: "oidc", OIDCSubject: "old-sub-dave",
	}
	db.Create(target)

	w := makeIDPRequest(t, h, sm, admin, target.ID, "oidc")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	var updated models.User
	db.First(&updated, target.ID)
	if updated.OIDCSubject != "" {
		t.Errorf("OIDCSubject should be cleared to force re-link, got %q", updated.OIDCSubject)
	}
}
