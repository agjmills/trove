package handlers

import (
	"net/http"

	"github.com/agjmills/trove/internal/auth"
	"gorm.io/gorm"
)

type PageHandler struct {
	db *gorm.DB
}

func NewPageHandler(db *gorm.DB) *PageHandler {
	return &PageHandler{db: db}
}

func (h *PageHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	render(w, "dashboard.html", map[string]any{
		"Title": "Dashboard",
		"User":  user,
		"Files": []any{},
	})
}
