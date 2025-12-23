package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/gorilla/csrf"
	"github.com/maruel/natural"
	"gorm.io/gorm"
)

type PageHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

// NewPageHandler returns a PageHandler initialized with the given GORM database handle and application configuration.
func NewPageHandler(db *gorm.DB, cfg *config.Config) *PageHandler {
	return &PageHandler{db: db, cfg: cfg}
}

func (h *PageHandler) ShowFiles(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	// Get current folder from query param, default to root
	currentFolder := sanitizeFolderPath(r.URL.Query().Get("folder"))

	// Validate folder exists (root folder "/" is always valid)
	if currentFolder != "/" {
		// Check if folder exists in folders table
		var folderCount int64
		h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ?", user.ID, currentFolder).Count(&folderCount)

		// Also check if any files exist in this folder path (implicit folders)
		var fileCount int64
		if folderCount == 0 {
			h.db.Model(&models.File{}).Where("user_id = ? AND logical_path = ?", user.ID, currentFolder).Count(&fileCount)
		}

		// If folder doesn't exist in either table, return 404
		if folderCount == 0 && fileCount == 0 {
			http.NotFound(w, r)
			return
		}
	}

	// Pagination parameters
	page := 1
	pageSize := 15
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	offset := (page - 1) * pageSize

	// Get direct subfolders from Folders table (exclude deleted)
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
			AND trashed_at IS NULL
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
			AND trashed_at IS NULL
			ORDER BY folder_path
		`, user.ID, currentFolder+"/%", currentFolder+"/%/%").Scan(&folders)
	}

	// Also check for implicit folders (folders that only exist because files are in them)
	// Exclude deleted files
	type implicitFolderPath struct {
		LogicalPath string
	}
	var implicitFolders []implicitFolderPath
	h.db.Model(&models.File{}).
		Select("DISTINCT logical_path").
		Where("user_id = ? AND logical_path LIKE ? AND logical_path != ? AND trashed_at IS NULL",
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
		relativePath := sf.LogicalPath
		if currentFolder != "/" {
			relativePath = strings.TrimPrefix(sf.LogicalPath, currentFolder+"/")
		} else {
			relativePath = strings.TrimPrefix(sf.LogicalPath, "/")
		}

		// Only include direct children (no further slashes)
		if !strings.Contains(relativePath, "/") && relativePath != "" {
			folderMap[relativePath] = true
		}
	}

	// FolderInfo holds folder name and sanitized ID for safe HTML rendering
	type FolderInfo struct {
		Name string
		ID   string
	}

	// Convert to slice and sort naturally (case-insensitive)
	// All folders are shown (no pagination for folders)
	folderNames := make([]string, 0, len(folderMap))
	for name := range folderMap {
		folderNames = append(folderNames, name)
	}
	sort.Slice(folderNames, func(i, j int) bool {
		return natural.Less(strings.ToLower(folderNames[i]), strings.ToLower(folderNames[j]))
	})

	// Build folder info with sanitized IDs
	folderInfos := make([]FolderInfo, 0, len(folderNames))
	for i, name := range folderNames {
		// Use index-based ID to guarantee uniqueness even if sanitized names collide
		folderInfos = append(folderInfos, FolderInfo{
			Name: name,
			ID:   "folder-" + strconv.Itoa(i),
		})
	}

	// Get all files in current folder for natural sorting
	// Exclude failed uploads - they are shown as toast notifications instead
	// Exclude deleted files
	var allFiles []models.File
	h.db.Where("user_id = ? AND logical_path = ? AND upload_status != ? AND trashed_at IS NULL", user.ID, currentFolder, "failed").Find(&allFiles)

	// Sort files naturally (handles "file2" before "file10" correctly)
	sort.Slice(allFiles, func(i, j int) bool {
		return natural.Less(strings.ToLower(allFiles[i].OriginalFilename), strings.ToLower(allFiles[j].OriginalFilename))
	})

	// Pagination applies only to files (folders are always shown)
	totalFiles := len(allFiles)
	var files []models.File
	if offset < totalFiles {
		end := offset + pageSize
		if end > totalFiles {
			end = totalFiles
		}
		files = allFiles[offset:end]
	}

	// Calculate pagination info (based on files only)
	totalPages := (totalFiles + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
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

	// Get flash message if any
	flashMsg := flash.Get(w, r)

	// Get any failed uploads for this user to show as toast notifications
	// These will be auto-dismissed after being shown to the user
	// Exclude deleted files
	var failedUploads []models.File
	h.db.Where("user_id = ? AND upload_status = ? AND trashed_at IS NULL", user.ID, "failed").Find(&failedUploads)

	// Count deleted items for nav badge (single query for both files and folders)
	var deletedCount int64
	h.db.Raw(`
		SELECT 
			(SELECT COUNT(*) FROM files WHERE user_id = ? AND trashed_at IS NOT NULL AND deleted_at IS NULL) +
			(SELECT COUNT(*) FROM folders WHERE user_id = ? AND trashed_at IS NOT NULL AND deleted_at IS NULL) AS total
	`, user.ID, user.ID).Scan(&deletedCount)

	render(w, "files.html", map[string]any{
		"Title":         "Files",
		"User":          user,
		"Files":         files,
		"Folders":       folderInfos,
		"CurrentFolder": currentFolder,
		"ParentFolder":  parentFolder,
		"Breadcrumbs":   breadcrumbs,
		"Flash":         flashMsg,
		"CSRFToken":     csrf.Token(r),
		"Page":          page,
		"TotalPages":    totalPages,
		"TotalFiles":    totalFiles,
		"FullWidth":     true,
		"MaxUploadSize": h.cfg.MaxUploadSize,
		"FailedUploads": failedUploads,
		"DeletedCount":  deletedCount,
	})
}
