package auth

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/agjmills/trove/internal/database/models"
	"gorm.io/gorm"
)

type contextKey string

const UserContextKey contextKey = "user"

func RequireAuth(db *gorm.DB, sessionManager *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user ID from session
			userID := sessionManager.GetInt(r.Context(), "user_id")
			if userID == 0 {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			// Load user from database
			var user models.User
			if err := db.First(&user, userID).Error; err != nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, &user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(r *http.Request) *models.User {
	user, _ := r.Context().Value(UserContextKey).(*models.User)
	return user
}

func OptionalAuth(db *gorm.DB, sessionManager *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user ID from session
			userID := sessionManager.GetInt(r.Context(), "user_id")
			if userID != 0 {
				// Load user from database
				var user models.User
				if err := db.First(&user, userID).Error; err == nil {
					ctx := context.WithValue(r.Context(), UserContextKey, &user)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
