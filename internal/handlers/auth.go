package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/csrf"
	"gorm.io/gorm"
)

// isJSONRequest checks if the request expects JSON format.
// It uses a prefix match to handle Content-Type values like "application/json; charset=utf-8".
func isJSONRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/json")
}

type AuthHandler struct {
	db             *gorm.DB
	cfg            *config.Config
	sessionManager *scs.SessionManager
}

func NewAuthHandler(db *gorm.DB, cfg *config.Config, sessionManager *scs.SessionManager) *AuthHandler {
	return &AuthHandler{
		db:             db,
		cfg:            cfg,
		sessionManager: sessionManager,
	}
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.EnableRegistration {
		http.Error(w, "Registration is disabled", http.StatusForbidden)
		return
	}

	var req RegisterRequest

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
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		if isJSON {
			http.Error(w, "All fields are required", http.StatusBadRequest)
		} else {
			render(w, "register.html", map[string]any{
				"Title":              "Register",
				"Error":              "All fields are required",
				"CSRFToken":          csrf.Token(r),
				"EnableRegistration": h.cfg.EnableRegistration,
			})
		}
		return
	}

	var existing models.User
	if err := h.db.Where("username = ? OR email = ?", req.Username, req.Email).First(&existing).Error; err == nil {
		http.Error(w, "Username or email already exists", http.StatusConflict)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password, h.cfg.BcryptCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	user := models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: passwordHash,
		StorageQuota: h.cfg.DefaultUserQuota,
	}

	if err := h.db.Create(&user).Error; err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Create session using scs
	err = h.sessionManager.RenewToken(r.Context())
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	h.sessionManager.Put(r.Context(), "user_id", int(user.ID))

	if isJSON {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		})
	} else {
		http.Redirect(w, r, "/files", http.StatusSeeOther)
	}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	isJSON := isJSONRequest(r)
	if isJSON {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
	} else {
		req.Username = r.FormValue("username")
		req.Password = r.FormValue("password")
	}

	var user models.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		if isJSON {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		} else {
			render(w, "login.html", map[string]any{
				"Title":              "Login",
				"Error":              "Invalid credentials",
				"EnableRegistration": h.cfg.EnableRegistration,
			})
		}
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		if isJSON {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		} else {
			render(w, "login.html", map[string]any{
				"Title":              "Login",
				"Error":              "Invalid credentials",
				"EnableRegistration": h.cfg.EnableRegistration,
			})
		}
		return
	}

	// Create session using scs
	err := h.sessionManager.RenewToken(r.Context())
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	h.sessionManager.Put(r.Context(), "user_id", int(user.ID))

	if isJSON {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		})
	} else {
		http.Redirect(w, r, "/files", http.StatusSeeOther)
	}
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Destroy session using scs
	err := h.sessionManager.Destroy(r.Context())
	if err != nil {
		http.Error(w, "Failed to logout", http.StatusInternalServerError)
		return
	}

	if isJSONRequest(r) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Logged out"})
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// Page rendering methods

func (h *AuthHandler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user != nil {
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	render(w, "login.html", map[string]any{
		"Title":              "Login",
		"CSRFToken":          csrf.Token(r),
		"EnableRegistration": h.cfg.EnableRegistration,
	})
}

func (h *AuthHandler) ShowRegister(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.EnableRegistration {
		http.NotFound(w, r)
		return
	}

	user := auth.GetUser(r)
	if user != nil {
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	render(w, "register.html", map[string]any{
		"Title":              "Register",
		"CSRFToken":          csrf.Token(r),
		"EnableRegistration": h.cfg.EnableRegistration,
	})
}

func (h *AuthHandler) ShowSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	render(w, "settings.html", map[string]any{
		"Title":     "Settings",
		"User":      user,
		"CSRFToken": csrf.Token(r),
		"FullWidth": true,
	})
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	isJSON := isJSONRequest(r)
	if user == nil {
		if isJSON {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		} else {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}
		return
	}

	var req ChangePasswordRequest

	if isJSON {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
	} else {
		req.CurrentPassword = r.FormValue("current_password")
		req.NewPassword = r.FormValue("new_password")
		req.ConfirmPassword = r.FormValue("confirm_password")
	}

	// Validate input
	if req.CurrentPassword == "" || req.NewPassword == "" || req.ConfirmPassword == "" {
		if isJSON {
			http.Error(w, "All fields are required", http.StatusBadRequest)
		} else {
			render(w, "settings.html", map[string]any{
				"Title":     "Settings",
				"User":      user,
				"CSRFToken": csrf.Token(r),
				"Error":     "All fields are required",
				"FullWidth": true,
			})
		}
		return
	}

	if req.NewPassword != req.ConfirmPassword {
		if isJSON {
			http.Error(w, "New passwords do not match", http.StatusBadRequest)
		} else {
			render(w, "settings.html", map[string]any{
				"Title":     "Settings",
				"User":      user,
				"CSRFToken": csrf.Token(r),
				"Error":     "New passwords do not match",
				"FullWidth": true,
			})
		}
		return
	}

	if len(req.NewPassword) < 8 {
		if isJSON {
			http.Error(w, "New password must be at least 8 characters", http.StatusBadRequest)
		} else {
			render(w, "settings.html", map[string]any{
				"Title":     "Settings",
				"User":      user,
				"CSRFToken": csrf.Token(r),
				"Error":     "New password must be at least 8 characters",
				"FullWidth": true,
			})
		}
		return
	}

	if len(req.NewPassword) > 72 {
		if isJSON {
			http.Error(w, "New password must be at most 72 characters", http.StatusBadRequest)
		} else {
			render(w, "settings.html", map[string]any{
				"Title":     "Settings",
				"User":      user,
				"CSRFToken": csrf.Token(r),
				"Error":     "New password must be at most 72 characters",
				"FullWidth": true,
			})
		}
		return
	}

	// Fetch full user from database to get password hash
	var dbUser models.User
	if err := h.db.First(&dbUser, user.ID).Error; err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	// Verify current password
	if !auth.VerifyPassword(dbUser.PasswordHash, req.CurrentPassword) {
		if isJSON {
			http.Error(w, "Current password is incorrect", http.StatusUnauthorized)
		} else {
			render(w, "settings.html", map[string]any{
				"Title":     "Settings",
				"User":      user,
				"CSRFToken": csrf.Token(r),
				"Error":     "Current password is incorrect",
				"FullWidth": true,
			})
		}
		return
	}

	// Hash new password
	newPasswordHash, err := auth.HashPassword(req.NewPassword, h.cfg.BcryptCost)
	if err != nil {
		http.Error(w, "Failed to hash new password", http.StatusInternalServerError)
		return
	}

	// Update password in database
	if err := h.db.Model(&dbUser).Update("password_hash", newPasswordHash).Error; err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	if isJSON {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Password changed successfully"})
	} else {
		render(w, "settings.html", map[string]any{
			"Title":     "Settings",
			"User":      user,
			"CSRFToken": csrf.Token(r),
			"Success":   "Password changed successfully",
			"FullWidth": true,
		})
	}
}
