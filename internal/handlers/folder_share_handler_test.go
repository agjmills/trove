package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	csrf "filippo.io/csrf/gorilla"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
)

func setupFolderShareTest(t *testing.T) (*FolderShareHandler, *gorm.DB, *models.User, *scs.SessionManager) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Folder{}, &models.File{}, &models.FolderShareLink{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := &models.User{
		Username:         "alice",
		Email:            "alice@example.com",
		IdentityProvider: "internal",
		StorageQuota:     1024 * 1024 * 100,
	}
	db.Create(user)

	sm := scs.New()
	h := NewFolderShareHandler(db, &readableStorage{content: "hello"}, sm)
	return h, db, user, sm
}

func createTestFileInFolder(t *testing.T, db *gorm.DB, userID uint, folder, filename string) *models.File {
	t.Helper()
	f := &models.File{
		UserID:           userID,
		StoragePath:      "test/" + filename,
		Filename:         filename,
		OriginalFilename: filename,
		LogicalPath:      folder,
		FileSize:         5,
		MimeType:         "text/plain",
		UploadStatus:     "completed",
	}
	db.Create(f)
	return f
}

func createFolderShareLink(t *testing.T, db *gorm.DB, userID uint, folderPath string, expiresAt *time.Time, maxUses *int) *models.FolderShareLink {
	t.Helper()
	return createFolderShareLinkWithPassword(t, db, userID, folderPath, expiresAt, maxUses, "")
}

func createFolderShareLinkWithPassword(t *testing.T, db *gorm.DB, userID uint, folderPath string, expiresAt *time.Time, maxUses *int, password string) *models.FolderShareLink {
	t.Helper()
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	link := &models.FolderShareLink{
		Token:      token,
		FolderPath: folderPath,
		UserID:     userID,
		ExpiresAt:  expiresAt,
		MaxUses:    maxUses,
	}
	if password != "" {
		ph, err := auth.HashPassword(password, 4)
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
		link.PasswordHash = &ph
	}
	db.Create(link)
	return link
}

func makeFolderAccessRequest(t *testing.T, h *FolderShareHandler, sm *scs.SessionManager, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/f/"+token, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.AccessFolderShareLink(w, r)
	})).ServeHTTP(w, req)
	return w
}

func makeFolderVerifyRequest(t *testing.T, h *FolderShareHandler, sm *scs.SessionManager, token, password string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body := "password=" + password
	req := httptest.NewRequest(http.MethodPost, "/f/"+token, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.VerifyFolderSharePassword(w, r)
	})).ServeHTTP(w, req)
	return w
}

func makeFolderDownloadRequest(t *testing.T, h *FolderShareHandler, sm *scs.SessionManager, token string, fileID uint, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/f/%s/files/%d", token, fileID), nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	rctx.URLParams.Add("id", fmt.Sprint(fileID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.DownloadSharedFolderFile(w, r)
	})).ServeHTTP(w, req)
	return w
}

func makeFolderCreateRequest(t *testing.T, h *FolderShareHandler, user *models.User, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/folders/share", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, user)
	w := httptest.NewRecorder()
	h.CreateFolderShareLink(w, req)
	return w
}

func makeFolderRevokeRequest(t *testing.T, h *FolderShareHandler, user *models.User, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/f/"+token+"/revoke", nil)
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, user)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.RevokeFolderShareLink(w, req)
	return w
}

// --- CreateFolderShareLink ---

func TestCreateFolderShareLink_Basic(t *testing.T) {
	h, db, user, _ := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/docs", "readme.txt")

	w := makeFolderCreateRequest(t, h, user, "folder_path=/docs")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", w.Code, w.Body.String())
	}

	var links []models.FolderShareLink
	db.Where("folder_path = ? AND user_id = ?", "/docs", user.ID).Find(&links)
	if len(links) != 1 {
		t.Fatalf("want 1 share link, got %d", len(links))
	}
	if links[0].Token == "" {
		t.Error("token should not be empty")
	}
}

func TestCreateFolderShareLink_WithPassword(t *testing.T) {
	h, db, user, _ := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/docs", "readme.txt")

	w := makeFolderCreateRequest(t, h, user, "folder_path=/docs&password=secret")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	var link models.FolderShareLink
	db.Where("folder_path = ? AND user_id = ?", "/docs", user.ID).First(&link)
	if link.PasswordHash == nil {
		t.Fatal("PasswordHash should be set")
	}
	if !auth.VerifyPassword(*link.PasswordHash, "secret") {
		t.Error("password hash should verify against submitted password")
	}
}

func TestCreateFolderShareLink_Unauthenticated(t *testing.T) {
	h, _, _, _ := setupFolderShareTest(t)
	req := httptest.NewRequest(http.MethodPost, "/folders/share", strings.NewReader("folder_path=/docs"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)
	w := httptest.NewRecorder()
	h.CreateFolderShareLink(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestCreateFolderShareLink_InvalidMaxUses(t *testing.T) {
	h, db, user, _ := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/docs", "readme.txt")

	w := makeFolderCreateRequest(t, h, user, "folder_path=/docs&max_uses=0")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for max_uses=0, got %d", w.Code)
	}
}

// --- AccessFolderShareLink ---

func TestAccessFolderShareLink_Success(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/photos", "pic.jpg")
	link := createFolderShareLink(t, db, user.ID, "/photos", nil, nil)

	w := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.FolderShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 1 {
		t.Errorf("want uses=1, got %d", updated.Uses)
	}
}

func TestAccessFolderShareLink_InvalidToken(t *testing.T) {
	h, _, _, sm := setupFolderShareTest(t)
	w := makeFolderAccessRequest(t, h, sm, "notavalidtoken", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestAccessFolderShareLink_Expired(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	past := time.Now().Add(-time.Hour)
	link := createFolderShareLink(t, db, user.ID, "/photos", &past, nil)

	w := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for expired link, got %d", w.Code)
	}
}

func TestAccessFolderShareLink_MaxUsesExhausted(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/photos", "pic.jpg")
	maxUses := 2
	link := createFolderShareLink(t, db, user.ID, "/photos", nil, &maxUses)

	for i := 0; i < 2; i++ {
		w := makeFolderAccessRequest(t, h, sm, link.Token, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("access %d: want 200, got %d", i+1, w.Code)
		}
	}

	w := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 after max uses exhausted, got %d", w.Code)
	}
}

func TestAccessFolderShareLink_IncludesSubfolderFiles(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/photos", "top.jpg")
	createTestFileInFolder(t, db, user.ID, "/photos/vacation", "beach.jpg")
	link := createFolderShareLink(t, db, user.ID, "/photos", nil, nil)

	w := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "top.jpg") {
		t.Error("should contain direct child file")
	}
	if !strings.Contains(body, "beach.jpg") {
		t.Error("should contain subfolder file")
	}
}

// --- Password-protected folder shares ---

func TestAccessFolderShareLink_PasswordProtected_ShowsForm(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	link := createFolderShareLinkWithPassword(t, db, user.ID, "/private", nil, nil, "secret")

	w := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (password form), got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "hello") {
		t.Error("should not serve file listing without password")
	}
	// Use counter must NOT be incremented
	var updated models.FolderShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 0 {
		t.Errorf("uses should not increment on unauthenticated GET, got %d", updated.Uses)
	}
}

func TestVerifyFolderSharePassword_WrongPassword(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	link := createFolderShareLinkWithPassword(t, db, user.ID, "/private", nil, nil, "correct")

	w := makeFolderVerifyRequest(t, h, sm, link.Token, "wrong", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (form with error), got %d", w.Code)
	}
	var updated models.FolderShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 0 {
		t.Errorf("uses should not increment on wrong password, got %d", updated.Uses)
	}
}

func TestVerifyFolderSharePassword_CorrectPassword(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/private", "secret.txt")
	link := createFolderShareLinkWithPassword(t, db, user.ID, "/private", nil, nil, "correct")

	w := makeFolderVerifyRequest(t, h, sm, link.Token, "correct", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303 redirect after correct password, got %d", w.Code)
	}

	var updated models.FolderShareLink
	db.First(&updated, link.ID)
	if updated.Uses != 1 {
		t.Errorf("want uses=1 after correct password, got %d", updated.Uses)
	}
}

func TestVerifyFolderSharePassword_MaxUsesRespected(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	maxUses := 1
	link := createFolderShareLinkWithPassword(t, db, user.ID, "/private", nil, &maxUses, "pw")

	w := makeFolderVerifyRequest(t, h, sm, link.Token, "pw", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("first verify should redirect, got %d", w.Code)
	}

	w2 := makeFolderVerifyRequest(t, h, sm, link.Token, "pw", nil)
	if w2.Code != http.StatusNotFound {
		t.Errorf("want 404 after max uses exhausted, got %d", w2.Code)
	}
}

func TestVerifyFolderSharePassword_NoPasswordRedirects(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	link := createFolderShareLink(t, db, user.ID, "/docs", nil, nil)

	w := makeFolderVerifyRequest(t, h, sm, link.Token, "", nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("want 303 for non-password link POST, got %d", w.Code)
	}
}

// --- DownloadSharedFolderFile ---

func TestDownloadSharedFolderFile_Success(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	file := createTestFileInFolder(t, db, user.ID, "/docs", "report.pdf")
	link := createFolderShareLink(t, db, user.ID, "/docs", nil, nil)

	w := makeFolderDownloadRequest(t, h, sm, link.Token, file.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "hello" {
		t.Errorf("want file contents, got %q", w.Body.String())
	}
}

func TestDownloadSharedFolderFile_SubfolderFile(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	file := createTestFileInFolder(t, db, user.ID, "/docs/sub", "nested.txt")
	link := createFolderShareLink(t, db, user.ID, "/docs", nil, nil)

	w := makeFolderDownloadRequest(t, h, sm, link.Token, file.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for subfolder file, got %d", w.Code)
	}
}

func TestDownloadSharedFolderFile_OutsideFolder(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	// File in a different folder — should be blocked
	file := createTestFileInFolder(t, db, user.ID, "/other", "private.txt")
	link := createFolderShareLink(t, db, user.ID, "/docs", nil, nil)

	w := makeFolderDownloadRequest(t, h, sm, link.Token, file.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for file outside shared folder, got %d", w.Code)
	}
}

func TestDownloadSharedFolderFile_PasswordProtected_NoSession(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	file := createTestFileInFolder(t, db, user.ID, "/private", "secret.txt")
	link := createFolderShareLinkWithPassword(t, db, user.ID, "/private", nil, nil, "pw")

	w := makeFolderDownloadRequest(t, h, sm, link.Token, file.ID, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("want 303 redirect (no session), got %d", w.Code)
	}
}

// --- RevokeFolderShareLink ---

func TestRevokeFolderShareLink_Success(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	link := createFolderShareLink(t, db, user.ID, "/docs", nil, nil)

	w := makeFolderRevokeRequest(t, h, user, link.Token)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}

	w2 := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w2.Code != http.StatusNotFound {
		t.Errorf("revoked link should return 404, got %d", w2.Code)
	}
}

func TestRevokeFolderShareLink_WrongOwner(t *testing.T) {
	h, db, user, sm := setupFolderShareTest(t)
	createTestFileInFolder(t, db, user.ID, "/docs", "readme.txt")
	link := createFolderShareLink(t, db, user.ID, "/docs", nil, nil)

	other := &models.User{Username: "eve", Email: "eve@example.com", IdentityProvider: "internal", StorageQuota: 1024}
	db.Create(other)

	w := makeFolderRevokeRequest(t, h, other, link.Token)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for wrong owner revoke, got %d", w.Code)
	}

	w2 := makeFolderAccessRequest(t, h, sm, link.Token, nil)
	if w2.Code != http.StatusOK {
		t.Errorf("link should still be valid, got %d", w2.Code)
	}
}
