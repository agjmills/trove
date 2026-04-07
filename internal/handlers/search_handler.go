package handlers

import (
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
)

type SearchHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewSearchHandler(db *gorm.DB, cfg *config.Config) *SearchHandler {
	return &SearchHandler{db: db, cfg: cfg}
}

// Search handles GET /search?q= — full-text filename search for the authenticated user.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))

	var results []models.File
	if q != "" {
		pattern := "%" + strings.ToLower(q) + "%"
		h.db.Where(
			`user_id = ? AND trashed_at IS NULL AND upload_status = 'completed'
			AND (LOWER(filename) LIKE ? OR LOWER(original_filename) LIKE ? OR LOWER(logical_path) LIKE ?
			     OR LOWER(CAST(tags AS TEXT)) LIKE ?)`,
			user.ID, pattern, pattern, pattern, pattern,
		).Order("logical_path, filename").Find(&results)
	}

	if err := render(w, "search.html", map[string]any{
		"Title":   "Search",
		"User":    user,
		"Query":   q,
		"Results": results,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
