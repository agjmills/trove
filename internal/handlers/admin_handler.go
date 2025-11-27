package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"gorm.io/gorm"
)

type AdminHandler struct {
	db      *gorm.DB
	cfg     *config.Config
	storage storage.StorageBackend
}

func NewAdminHandler(db *gorm.DB, cfg *config.Config, storageBackend storage.StorageBackend) *AdminHandler {
	return &AdminHandler{
		db:      db,
		cfg:     cfg,
		storage: storageBackend,
	}
}

// DashboardStats holds overall dashboard statistics
type DashboardStats struct {
	TotalUsers       int64
	TotalFiles       int64
	TotalStorageUsed int64
}

// ShowDashboard displays the admin dashboard with usage statistics
func (h *AdminHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil || !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var stats DashboardStats

	// Get total users
	if err := h.db.Model(&models.User{}).Count(&stats.TotalUsers).Error; err != nil {
		logger.Error("Failed to count users", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Get total files
	if err := h.db.Model(&models.File{}).Count(&stats.TotalFiles).Error; err != nil {
		logger.Error("Failed to count files", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Get total storage used across all users
	if err := h.db.Model(&models.User{}).Select("COALESCE(SUM(storage_used), 0)").Scan(&stats.TotalStorageUsed).Error; err != nil {
		logger.Error("Failed to sum storage used", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	render(w, "admin.html", map[string]any{
		"Title":     "Admin Dashboard",
		"User":      user,
		"Stats":     stats,
		"CSRFToken": csrf.Token(r),
		"FullWidth": true,
	})
}

// ShowUsers displays the user management page
func (h *AdminHandler) ShowUsers(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil || !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var users []models.User
	if err := h.db.Order("created_at DESC").Find(&users).Error; err != nil {
		logger.Error("Failed to fetch users", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Get file counts for all users in a single query to avoid N+1
	type fileAgg struct {
		UserID    uint
		FileCount int64
	}
	var aggs []fileAgg
	if err := h.db.Model(&models.File{}).
		Select("user_id, COUNT(*) as file_count").
		Group("user_id").
		Scan(&aggs).Error; err != nil {
		logger.Error("Failed to aggregate file counts", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Build a map for quick lookup
	fileCountMap := make(map[uint]int64)
	for _, agg := range aggs {
		fileCountMap[agg.UserID] = agg.FileCount
	}

	type UserWithStats struct {
		models.User
		FileCount int64
	}
	var usersWithStats []UserWithStats
	for _, u := range users {
		usersWithStats = append(usersWithStats, UserWithStats{
			User:      u,
			FileCount: fileCountMap[u.ID],
		})
	}

	render(w, "admin_users.html", map[string]any{
		"Title":        "User Management",
		"User":         user,
		"Users":        usersWithStats,
		"CSRFToken":    csrf.Token(r),
		"FullWidth":    true,
		"DefaultQuota": h.cfg.DefaultUserQuota,
	})
}

// CreateUserRequest holds the request data for creating a new user
type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

// CreateUser allows admins to create new users with an initial password
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	adminUser := auth.GetUser(r)
	if adminUser == nil || !adminUser.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req CreateUserRequest
	isJSON := isJSONRequest(r)

	if isJSON {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
	} else {
		req.Username = r.FormValue("username")
		req.Email = r.FormValue("email")
		req.Password = r.FormValue("password")
		req.IsAdmin = r.FormValue("is_admin") == "on"
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		http.Error(w, "Username, email, and password are required", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// Check for existing user
	var existing models.User
	if err := h.db.Where("username = ? OR email = ?", req.Username, req.Email).First(&existing).Error; err == nil {
		http.Error(w, "Username or email already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.Error("Database error checking for existing user", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Hash password
	passwordHash, err := auth.HashPassword(req.Password, h.cfg.BcryptCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// Create user
	newUser := models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: passwordHash,
		StorageQuota: h.cfg.DefaultUserQuota,
		IsAdmin:      req.IsAdmin,
	}

	if err := h.db.Create(&newUser).Error; err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	if isJSON {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":       newUser.ID,
			"username": newUser.Username,
			"email":    newUser.Email,
			"is_admin": newUser.IsAdmin,
		})
	} else {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}
}

// ToggleAdmin toggles the admin status of a user
func (h *AdminHandler) ToggleAdmin(w http.ResponseWriter, r *http.Request) {
	adminUser := auth.GetUser(r)
	if adminUser == nil || !adminUser.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Prevent admins from removing their own admin status
	if uint(userID) == adminUser.ID {
		http.Error(w, "Cannot modify your own admin status", http.StatusBadRequest)
		return
	}

	var targetUser models.User
	if err := h.db.First(&targetUser, userID).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Toggle admin status
	targetUser.IsAdmin = !targetUser.IsAdmin
	if err := h.db.Save(&targetUser).Error; err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// UpdateUserQuota updates a user's storage quota
func (h *AdminHandler) UpdateUserQuota(w http.ResponseWriter, r *http.Request) {
	adminUser := auth.GetUser(r)
	if adminUser == nil || !adminUser.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	quotaStr := r.FormValue("quota")
	quota, err := strconv.ParseInt(quotaStr, 10, 64)
	if err != nil || quota < 0 {
		http.Error(w, "Invalid quota value", http.StatusBadRequest)
		return
	}

	var targetUser models.User
	if err := h.db.First(&targetUser, userID).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Warn if setting quota below current usage
	if quota < targetUser.StorageUsed {
		logger.Warn("Setting quota below current storage usage",
			"user_id", userID,
			"new_quota", quota,
			"current_usage", targetUser.StorageUsed)
	}

	targetUser.StorageQuota = quota
	if err := h.db.Save(&targetUser).Error; err != nil {
		http.Error(w, "Failed to update user quota", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// DeleteUser deletes a user and their files in the background (admins cannot delete themselves)
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	adminUser := auth.GetUser(r)
	if adminUser == nil || !adminUser.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Prevent admins from deleting themselves
	if uint(userID) == adminUser.ID {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	var targetUser models.User
	if err := h.db.First(&targetUser, userID).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Get all files for this user before deleting
	var files []models.File
	if err := h.db.Where("user_id = ?", userID).Find(&files).Error; err != nil {
		logger.Error("Failed to fetch user files for deletion", "user_id", userID, "error", err)
	}

	// Delete user and their database records (folders, files metadata)
	// Using a transaction to ensure consistency
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		// Delete files metadata
		if err := tx.Where("user_id = ?", userID).Delete(&models.File{}).Error; err != nil {
			return err
		}
		// Delete folders
		if err := tx.Where("user_id = ?", userID).Delete(&models.Folder{}).Error; err != nil {
			return err
		}
		// Delete user
		if err := tx.Delete(&targetUser).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	// Delete actual files from storage in the background
	// TODO: Consider passing an app-lifecycle context for graceful shutdown support
	// instead of context.Background() so long-running deletions can be cancelled.
	if len(files) > 0 {
		go func(filesToDelete []models.File, username string) {
			logger.Info("Starting background file deletion", "user", username, "file_count", len(filesToDelete))
			deleted := 0
			failed := 0
			for _, file := range filesToDelete {
				if err := h.storage.Delete(context.Background(), file.StoragePath); err != nil {
					logger.Error("Failed to delete file from storage", "path", file.StoragePath, "error", err)
					failed++
				} else {
					deleted++
				}
			}
			logger.Info("Background file deletion complete", "user", username, "deleted", deleted, "failed", failed)
		}(files, targetUser.Username)
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// ResetUserPassword allows an admin to set a new password for a user
func (h *AdminHandler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	adminUser := auth.GetUser(r)
	if adminUser == nil || !adminUser.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	newPassword := r.FormValue("new_password")
	if len(newPassword) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	var targetUser models.User
	if err := h.db.First(&targetUser, userID).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Hash new password
	passwordHash, err := auth.HashPassword(newPassword, h.cfg.BcryptCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	targetUser.PasswordHash = passwordHash
	if err := h.db.Save(&targetUser).Error; err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
