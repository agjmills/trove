package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"gorm.io/gorm"
)

type AuthHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewAuthHandler(db *gorm.DB, cfg *config.Config) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg}
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

	contentType := r.Header.Get("Content-Type")
	if contentType == "application/json" {
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
		if contentType == "application/json" {
			http.Error(w, "All fields are required", http.StatusBadRequest)
		} else {
			registerTemplate.ExecuteTemplate(w, "layout.html", map[string]any{
				"Title": "Register",
				"Error": "All fields are required",
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

	duration, _ := time.ParseDuration(h.cfg.SessionDuration)
	token, err := auth.CreateSession(h.db, user.ID, duration)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Env == "production",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(duration.Seconds()),
	})

	if contentType == "application/json" {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		})
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	contentType := r.Header.Get("Content-Type")
	if contentType == "application/json" {
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
		if contentType == "application/json" {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		} else {
			loginTemplate.ExecuteTemplate(w, "layout.html", map[string]any{
				"Title": "Login",
				"Error": "Invalid credentials",
			})
		}
		return
	}

	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		if contentType == "application/json" {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		} else {
			loginTemplate.ExecuteTemplate(w, "layout.html", map[string]any{
				"Title": "Login",
				"Error": "Invalid credentials",
			})
		}
		return
	}

	duration, _ := time.ParseDuration(h.cfg.SessionDuration)
	token, err := auth.CreateSession(h.db, user.ID, duration)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Env == "production",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(duration.Seconds()),
	})

	if contentType == "application/json" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
		})
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil {
		auth.DeleteSession(h.db, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	if r.Header.Get("Content-Type") == "application/json" {
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
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	render(w, "login.html", map[string]any{
		"Title": "Login",
	})
}

func (h *AuthHandler) ShowRegister(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	render(w, "register.html", map[string]any{
		"Title": "Register",
	})
}
