package auth

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"gorm.io/gorm"
)

// RequireAdmin is middleware that requires the user to be authenticated and have admin privileges.
// If not authenticated, redirects to login. If not admin, returns 403 Forbidden.
func RequireAdmin(db *gorm.DB, sessionManager *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r)
			if user == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			if !user.IsAdmin {
				http.Error(w, "Forbidden - Admin access required", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
