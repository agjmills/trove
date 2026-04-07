package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	csrf "filippo.io/csrf/gorilla"
	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
)

// readableStorage wraps mockStorage and returns a readable body for Open.
type readableStorage struct {
	mockStorage
	content string
}

func (s *readableStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.content)), nil
}

func setupShareTest(t *testing.T) (*ShareHandler, *gorm.DB, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.File{}, &models.ShareLink{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := &models.User{
		Username:         "alice",
		Email:            "alice@example.com",
		IdentityProvider: "internal",
		StorageQuota:     1024 * 1024 * 100,
	}
	db.Create(user)

	h := NewShareHandler(db, &readableStorage{content: "hello"})
	return h, db, user
}

func createTestFile(t *testing.T, db *gorm.DB, userID uint) *models.File {
	t.Helper()
	f := &models.File{
		UserID:           userID,
		StoragePath:      "test/path.bin",
		Filename:         "test.txt",
		OriginalFilename: "test.txt",
		LogicalPath:      "/",
		FileSize:         5,
		MimeType:         "text/plain",
		UploadStatus:     "completed",
	}
	db.Create(f)
	return f
}

func makeShareRequest(t *testing.T, h *ShareHandler, user *models.User, fileID string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/files/"+fileID+"/share", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fileID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.CreateShareLink(w, req)
	return w
}

// --- CreateShareLink ---

func TestCreateShareLink_Basic(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)

	w := makeShareRequest(t, h, user, fmt.Sprint(file.ID), "")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	var links []models.ShareLink
	db.Where("file_id = ?", file.ID).Find(&links)
	if len(links) != 1 {
		t.Fatalf("expected 1 share link, got %d", len(links))
	}
	if links[0].Token == "" {
		t.Error("token should not be empty")
	}
	if links[0].ExpiresAt != nil {
		t.Error("expiry should be nil when not set")
	}
	if links[0].MaxUses != nil {
		t.Error("max_uses should be nil when not set")
	}
}

func TestCreateShareLink_WithExpiry(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)

	w := makeShareRequest(t, h, user, fmt.Sprint(file.ID), "expires_at=2099-12-31")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	var link models.ShareLink
	db.Where("file_id = ?", file.ID).First(&link)
	if link.ExpiresAt == nil {
		t.Fatal("ExpiresAt should be set")
	}
	if link.ExpiresAt.Year() != 2099 {
		t.Errorf("expected year 2099, got %d", link.ExpiresAt.Year())
	}
}

func TestCreateShareLink_WithMaxUses(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)

	w := makeShareRequest(t, h, user, fmt.Sprint(file.ID), "max_uses=3")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	var link models.ShareLink
	db.Where("file_id = ?", file.ID).First(&link)
	if link.MaxUses == nil || *link.MaxUses != 3 {
		t.Errorf("expected MaxUses=3, got %v", link.MaxUses)
	}
}

func TestCreateShareLink_InvalidMaxUses(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)

	w := makeShareRequest(t, h, user, fmt.Sprint(file.ID), "max_uses=0")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for max_uses=0, got %d", w.Code)
	}
}

func TestCreateShareLink_WrongOwner(t *testing.T) {
	h, db, _ := setupShareTest(t)

	other := &models.User{Username: "bob", Email: "bob@example.com", IdentityProvider: "internal", StorageQuota: 1024}
	db.Create(other)
	file := createTestFile(t, db, other.ID)

	// alice tries to share bob's file
	alice := &models.User{Username: "alice2", Email: "alice2@example.com", IdentityProvider: "internal", StorageQuota: 1024}
	db.Create(alice)
	w := makeShareRequest(t, h, alice, fmt.Sprint(file.ID), "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for wrong owner, got %d", w.Code)
	}
}

func TestCreateShareLink_Unauthenticated(t *testing.T) {
	h, db, _ := setupShareTest(t)
	user := &models.User{Username: "u", Email: "u@example.com", IdentityProvider: "internal", StorageQuota: 1024}
	db.Create(user)
	file := createTestFile(t, db, user.ID)

	req := httptest.NewRequest(http.MethodPost, "/files/"+fmt.Sprint(file.ID)+"/share", nil)
	req = csrf.UnsafeSkipCheck(req)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fmt.Sprint(file.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.CreateShareLink(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// --- AccessShareLink ---

func createShareLink(t *testing.T, db *gorm.DB, fileID, userID uint, expiresAt *time.Time, maxUses *int) *models.ShareLink {
	t.Helper()
	return createShareLinkWithPassword(t, db, fileID, userID, expiresAt, maxUses, "")
}

func createShareLinkWithPassword(t *testing.T, db *gorm.DB, fileID, userID uint, expiresAt *time.Time, maxUses *int, password string) *models.ShareLink {
	t.Helper()
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	link := &models.ShareLink{
		Token:     token,
		FileID:    fileID,
		UserID:    userID,
		ExpiresAt: expiresAt,
		MaxUses:   maxUses,
	}
	if password != "" {
		h, err := auth.HashPassword(password, 4) // bcrypt.MinCost for test speed
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
		link.PasswordHash = &h
	}
	db.Create(link)
	return link
}

func makeVerifyRequest(t *testing.T, h *ShareHandler, token, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := "password=" + password
	req := httptest.NewRequest(http.MethodPost, "/s/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.VerifySharePassword(w, req)
	return w
}

func makeAccessRequest(t *testing.T, h *ShareHandler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/s/"+token, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.AccessShareLink(w, req)
	return w
}

func TestAccessShareLink_Success(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLink(t, db, file.ID, user.ID, nil, nil)

	w := makeAccessRequest(t, h, link.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}

	// Use counter incremented
	var updated models.ShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 1 {
		t.Errorf("want uses=1, got %d", updated.Uses)
	}
}

func TestAccessShareLink_InvalidToken(t *testing.T) {
	h, _, _ := setupShareTest(t)
	w := makeAccessRequest(t, h, "notavalidtoken")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for invalid token, got %d", w.Code)
	}
}

func TestAccessShareLink_Expired(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	past := time.Now().Add(-time.Hour)
	link := createShareLink(t, db, file.ID, user.ID, &past, nil)

	w := makeAccessRequest(t, h, link.Token)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for expired link, got %d", w.Code)
	}
}

func TestAccessShareLink_MaxUsesExhausted(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	maxUses := 2
	link := createShareLink(t, db, file.ID, user.ID, nil, &maxUses)

	// Use it twice (should succeed)
	for i := 0; i < 2; i++ {
		w := makeAccessRequest(t, h, link.Token)
		if w.Code != http.StatusOK {
			t.Fatalf("access %d: want 200, got %d", i+1, w.Code)
		}
	}

	// Third access should be blocked
	w := makeAccessRequest(t, h, link.Token)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 after max uses exhausted, got %d", w.Code)
	}
}

// --- RevokeShareLink ---

func makeRevokeRequest(t *testing.T, h *ShareHandler, user *models.User, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/share/"+token+"/revoke", nil)
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.RevokeShareLink(w, req)
	return w
}

func TestRevokeShareLink_Success(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLink(t, db, file.ID, user.ID, nil, nil)

	w := makeRevokeRequest(t, h, user, link.Token)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	// Link should now be gone
	w2 := makeAccessRequest(t, h, link.Token)
	if w2.Code != http.StatusNotFound {
		t.Errorf("revoked link should return 404, got %d", w2.Code)
	}
}

func TestRevokeShareLink_WrongOwner(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLink(t, db, file.ID, user.ID, nil, nil)

	other := &models.User{Username: "eve", Email: "eve@example.com", IdentityProvider: "internal", StorageQuota: 1024}
	db.Create(other)

	w := makeRevokeRequest(t, h, other, link.Token)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for wrong owner revoke, got %d", w.Code)
	}

	// Link should still work
	w2 := makeAccessRequest(t, h, link.Token)
	if w2.Code != http.StatusOK {
		t.Errorf("link should still be valid, got %d", w2.Code)
	}
}

// --- Password-protected share links ---

func TestCreateShareLink_WithPassword(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)

	w := makeShareRequest(t, h, user, fmt.Sprint(file.ID), "password=secret123")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	var link models.ShareLink
	db.Where("file_id = ?", file.ID).First(&link)
	if link.PasswordHash == nil {
		t.Fatal("PasswordHash should be set")
	}
	if auth.VerifyPassword(*link.PasswordHash, "secret123") == false {
		t.Error("PasswordHash should verify against the submitted password")
	}
}

func TestAccessShareLink_PasswordProtected_ShowsForm(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLinkWithPassword(t, db, file.ID, user.ID, nil, nil, "secret")

	w := makeAccessRequest(t, h, link.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (password form), got %d", w.Code)
	}
	// Should return HTML, not the file contents
	if w.Body.String() == "hello" {
		t.Error("should not serve file without password")
	}
	// Uses counter should NOT be incremented
	var updated models.ShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 0 {
		t.Errorf("uses should not be incremented for GET on protected link, got %d", updated.Uses)
	}
}

func TestVerifySharePassword_WrongPassword(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLinkWithPassword(t, db, file.ID, user.ID, nil, nil, "correct")

	w := makeVerifyRequest(t, h, link.Token, "wrong")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (form with error), got %d", w.Code)
	}
	if w.Body.String() == "hello" {
		t.Error("should not serve file with wrong password")
	}
	var updated models.ShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 0 {
		t.Errorf("uses should not increment on wrong password, got %d", updated.Uses)
	}
}

func TestVerifySharePassword_CorrectPassword(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLinkWithPassword(t, db, file.ID, user.ID, nil, nil, "correct")

	w := makeVerifyRequest(t, h, link.Token, "correct")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (file), got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello" {
		t.Errorf("want file contents, got %q", w.Body.String())
	}
	var updated models.ShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 1 {
		t.Errorf("want uses=1 after correct password, got %d", updated.Uses)
	}
}

func TestVerifySharePassword_MaxUsesRespected(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	maxUses := 1
	link := createShareLinkWithPassword(t, db, file.ID, user.ID, nil, &maxUses, "pw")

	// First correct attempt should succeed
	w := makeVerifyRequest(t, h, link.Token, "pw")
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Fatalf("first access should succeed, got %d %q", w.Code, w.Body.String())
	}

	// Second attempt should be blocked even with correct password
	w2 := makeVerifyRequest(t, h, link.Token, "pw")
	if w2.Code != http.StatusNotFound {
		t.Errorf("want 404 after max uses exhausted, got %d", w2.Code)
	}
}

func TestVerifySharePassword_NoPasswordRedirects(t *testing.T) {
	h, db, user := setupShareTest(t)
	file := createTestFile(t, db, user.ID)
	link := createShareLink(t, db, file.ID, user.ID, nil, nil)

	// POST to a non-password link should redirect to GET
	w := makeVerifyRequest(t, h, link.Token, "")
	if w.Code != http.StatusSeeOther {
		t.Errorf("want 303 redirect for non-password link, got %d", w.Code)
	}
}
