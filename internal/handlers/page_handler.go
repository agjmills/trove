package handlers

import (
	"net/http"
	"strings"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
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

	// Get current folder from query param, default to root
	currentFolder := sanitizeFolderPath(r.URL.Query().Get("folder"))

	// Get files in current folder
	var files []models.File
	h.db.Where("user_id = ? AND folder_path = ?", user.ID, currentFolder).
		Order("created_at DESC").Find(&files)

	// Get direct subfolders from Folders table
	var folders []models.Folder
	if currentFolder == "/" {
		// Root level: get folders that don't contain additional slashes after the first one
		h.db.Raw(`
			SELECT * FROM folders 
			WHERE user_id = ? 
			AND folder_path LIKE '/%'
			AND folder_path NOT LIKE '%/%/%'
			AND LENGTH(folder_path) - LENGTH(REPLACE(folder_path, '/', '')) = 1
			AND deleted_at IS NULL
			ORDER BY folder_path
		`, user.ID).Scan(&folders)
	} else {
		// Subdirectory: get direct children only
		h.db.Raw(`
			SELECT * FROM folders 
			WHERE user_id = ? 
			AND folder_path LIKE ?
			AND folder_path NOT LIKE ?
			AND deleted_at IS NULL
			ORDER BY folder_path
		`, user.ID, currentFolder+"/%", currentFolder+"/%/%").Scan(&folders)
	}

	// Also check for implicit folders (folders that only exist because files are in them)
	type FolderInfo struct {
		FolderPath string
	}
	var implicitFolders []FolderInfo
	h.db.Model(&models.File{}).
		Select("DISTINCT folder_path").
		Where("user_id = ? AND folder_path LIKE ? AND folder_path != ?",
			user.ID, currentFolder+"/%", currentFolder).
		Scan(&implicitFolders)

	// Extract direct subfolder names
	folderMap := make(map[string]bool)

	// Add explicit folders
	for _, f := range folders {
		relativePath := f.FolderPath
		if currentFolder != "/" {
			relativePath = strings.TrimPrefix(f.FolderPath, currentFolder+"/")
		} else {
			relativePath = strings.TrimPrefix(f.FolderPath, "/")
		}
		if relativePath != "" {
			folderMap[relativePath] = true
		}
	}

	// Add implicit folders (only immediate children)
	for _, sf := range implicitFolders {
		relativePath := sf.FolderPath
		if currentFolder != "/" {
			relativePath = strings.TrimPrefix(sf.FolderPath, currentFolder+"/")
		} else {
			relativePath = strings.TrimPrefix(sf.FolderPath, "/")
		}

		// Only include direct children (no further slashes)
		if !strings.Contains(relativePath, "/") && relativePath != "" {
			folderMap[relativePath] = true
		}
	}

	// Convert to slice for template
	folderNames := make([]string, 0, len(folderMap))
	for name := range folderMap {
		folderNames = append(folderNames, name)
	}

	// Build breadcrumb trail
	type Breadcrumb struct {
		Name string
		Path string
	}
	breadcrumbs := []Breadcrumb{{Name: "Home", Path: "/"}}
	if currentFolder != "/" {
		parts := strings.Split(strings.Trim(currentFolder, "/"), "/")
		currentPath := ""
		for _, part := range parts {
			currentPath += "/" + part
			breadcrumbs = append(breadcrumbs, Breadcrumb{
				Name: part,
				Path: currentPath,
			})
		}
	}

	// Calculate parent folder
	parentFolder := "/"
	if currentFolder != "/" {
		lastSlash := strings.LastIndex(currentFolder, "/")
		if lastSlash == 0 {
			parentFolder = "/"
		} else {
			parentFolder = currentFolder[:lastSlash]
		}
	}

	render(w, "dashboard.html", map[string]any{
		"Title":         "Dashboard",
		"User":          user,
		"Files":         files,
		"Folders":       folderNames,
		"CurrentFolder": currentFolder,
		"ParentFolder":  parentFolder,
		"Breadcrumbs":   breadcrumbs,
	})
}
