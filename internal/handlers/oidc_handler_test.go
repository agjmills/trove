package handlers

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/oidc"
)

func setupOIDCHandler(t *testing.T) (*OIDCHandler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := &config.Config{
		DefaultUserQuota: 10 * 1024 * 1024 * 1024,
		OIDCAdminClaim:   "groups",
		OIDCAdminValue:   "trove-admins",
	}
	h := &OIDCHandler{db: db, cfg: cfg}
	return h, db
}

func makeClaims(subject, email, username string, isAdmin bool) oidc.Claims {
	return oidc.Claims{
		Subject:  subject,
		Email:    email,
		Username: username,
		IsAdmin:  isAdmin,
	}
}

// --- findOrProvisionUser ---

func TestFindOrProvisionUser_NewUser(t *testing.T) {
	h, db := setupOIDCHandler(t)

	user, err := h.findOrProvisionUser(makeClaims("sub-1", "alice@example.com", "alice", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Username != "alice" {
		t.Errorf("Username: want alice, got %q", user.Username)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("Email: want alice@example.com, got %q", user.Email)
	}
	if user.IdentityProvider != "oidc" {
		t.Errorf("IdentityProvider: want oidc, got %q", user.IdentityProvider)
	}
	if user.OIDCSubject != "sub-1" {
		t.Errorf("OIDCSubject: want sub-1, got %q", user.OIDCSubject)
	}

	// Persisted in DB
	var count int64
	db.Model(&models.User{}).Where("oidc_subject = ?", "sub-1").Count(&count)
	if count != 1 {
		t.Error("user not persisted in DB")
	}
}

func TestFindOrProvisionUser_FirstUserBecomesAdmin(t *testing.T) {
	h, _ := setupOIDCHandler(t)

	user, err := h.findOrProvisionUser(makeClaims("sub-1", "first@example.com", "first", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !user.IsAdmin {
		t.Error("first provisioned user should be admin regardless of claim")
	}
}

func TestFindOrProvisionUser_SubsequentUsersNotAdmin(t *testing.T) {
	h, db := setupOIDCHandler(t)

	// Seed one existing user so "first" logic doesn't apply
	db.Create(&models.User{Username: "existing", Email: "existing@example.com", IdentityProvider: "internal"})

	user, err := h.findOrProvisionUser(makeClaims("sub-2", "second@example.com", "second", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.IsAdmin {
		t.Error("non-first user without admin claim should not be admin")
	}
}

func TestFindOrProvisionUser_AdminClaimGrantsAdmin(t *testing.T) {
	h, db := setupOIDCHandler(t)
	db.Create(&models.User{Username: "existing", Email: "existing@example.com", IdentityProvider: "internal"})

	user, err := h.findOrProvisionUser(makeClaims("sub-3", "admin@example.com", "adminuser", true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !user.IsAdmin {
		t.Error("user with admin claim should be admin")
	}
}

func TestFindOrProvisionUser_FastPathBySubject(t *testing.T) {
	h, db := setupOIDCHandler(t)

	existing := &models.User{
		Username:         "alice",
		Email:            "alice@example.com",
		IdentityProvider: "oidc",
		OIDCSubject:      "sub-existing",
	}
	db.Create(existing)

	user, err := h.findOrProvisionUser(makeClaims("sub-existing", "alice@example.com", "alice", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != existing.ID {
		t.Errorf("expected existing user ID %d, got %d", existing.ID, user.ID)
	}

	// Only one user in DB
	var count int64
	db.Model(&models.User{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestFindOrProvisionUser_LinksOnFirstOIDCLogin(t *testing.T) {
	// Admin switched user to OIDC but subject not yet set
	h, db := setupOIDCHandler(t)

	pre := &models.User{
		Username:         "bob",
		Email:            "bob@example.com",
		IdentityProvider: "oidc",
		OIDCSubject:      "", // not linked yet
	}
	db.Create(pre)

	user, err := h.findOrProvisionUser(makeClaims("sub-new", "bob@example.com", "bob", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != pre.ID {
		t.Errorf("expected existing user, got new user ID %d", user.ID)
	}
	if user.OIDCSubject != "sub-new" {
		t.Errorf("OIDCSubject not stored: got %q", user.OIDCSubject)
	}

	// Still one user in DB
	var count int64
	db.Model(&models.User{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestFindOrProvisionUser_LinksOnFirstOIDCLogin_CaseInsensitiveEmail(t *testing.T) {
	// Authentik may return a differently-cased email than what is stored.
	h, db := setupOIDCHandler(t)

	pre := &models.User{
		Username:         "bob",
		Email:            "bob@example.com",
		IdentityProvider: "oidc",
		OIDCSubject:      "",
	}
	db.Create(pre)

	// IdP returns uppercased email
	user, err := h.findOrProvisionUser(makeClaims("sub-new", "BOB@EXAMPLE.COM", "bob", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != pre.ID {
		t.Errorf("expected existing user ID %d, got new user ID %d", pre.ID, user.ID)
	}
	if user.OIDCSubject != "sub-new" {
		t.Errorf("OIDCSubject not stored: got %q", user.OIDCSubject)
	}

	var count int64
	db.Model(&models.User{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestFindOrProvisionUser_InternalUserEmailBlocksOIDCProvision(t *testing.T) {
	// An internal user owns carol@example.com. An OIDC login for the same email
	// must NOT silently link to the internal account, and cannot auto-provision
	// a duplicate email. The correct flow is: admin switches the user's IDP first.
	h, db := setupOIDCHandler(t)

	db.Create(&models.User{
		Username:         "carol",
		Email:            "carol@example.com",
		IdentityProvider: "internal",
		PasswordHash:     "somehash",
	})

	_, err := h.findOrProvisionUser(makeClaims("sub-carol", "carol@example.com", "carol", false))
	if err == nil {
		t.Error("expected error when OIDC email collides with an internal account; admin must switch IDP explicitly")
	}

	// No new user should have been created
	var count int64
	db.Model(&models.User{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 user, got %d", count)
	}
}

func TestFindOrProvisionUser_AdminSyncOnReturningUser(t *testing.T) {
	h, db := setupOIDCHandler(t)

	existing := &models.User{
		Username:         "dave",
		Email:            "dave@example.com",
		IdentityProvider: "oidc",
		OIDCSubject:      "sub-dave",
		IsAdmin:          true,
	}
	db.Create(existing)

	// Admin claim revoked
	user, err := h.findOrProvisionUser(makeClaims("sub-dave", "dave@example.com", "dave", false))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.IsAdmin {
		t.Error("IsAdmin should be synced to false when claim is absent")
	}
}

// --- uniqueUsername ---

func TestUniqueUsername_NoCollision(t *testing.T) {
	h, db := setupOIDCHandler(t)
	result := h.uniqueUsername(db, "alice")
	if result != "alice" {
		t.Errorf("want alice, got %q", result)
	}
}

func TestUniqueUsername_OneCollision(t *testing.T) {
	h, db := setupOIDCHandler(t)
	db.Create(&models.User{Username: "alice", Email: "a@example.com", IdentityProvider: "internal"})
	result := h.uniqueUsername(db, "alice")
	if result != "alice_2" {
		t.Errorf("want alice_2, got %q", result)
	}
}

func TestUniqueUsername_MultipleCollisions(t *testing.T) {
	h, db := setupOIDCHandler(t)
	db.Create(&models.User{Username: "alice", Email: "a1@example.com", IdentityProvider: "internal"})
	db.Create(&models.User{Username: "alice_2", Email: "a2@example.com", IdentityProvider: "internal"})
	db.Create(&models.User{Username: "alice_3", Email: "a3@example.com", IdentityProvider: "internal"})
	result := h.uniqueUsername(db, "alice")
	if result != "alice_4" {
		t.Errorf("want alice_4, got %q", result)
	}
}
