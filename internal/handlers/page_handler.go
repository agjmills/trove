package handlers

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/gorilla/csrf"
	"gorm.io/gorm"
)

type PageHandler struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewPageHandler(db *gorm.DB, cfg *config.Config) *PageHandler {
	return &PageHandler{db: db, cfg: cfg}
}

func (h *PageHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)

	// Get current folder from query param, default to root
	currentFolder := sanitizeFolderPath(r.URL.Query().Get("folder"))

	// Pagination parameters
	page := 1
	pageSize := 50
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	offset := (page - 1) * pageSize

	// Get total count of files in current folder
	var totalFiles int64
	h.db.Model(&models.File{}).Where("user_id = ? AND folder_path = ?", user.ID, currentFolder).Count(&totalFiles)

	// Get files in current folder with pagination
	var files []models.File
	h.db.Where("user_id = ? AND folder_path = ?", user.ID, currentFolder).
		Offset(offset).
		Limit(pageSize).
		Find(&files)

	// Sort files with natural ordering (handles numbered files correctly)
	sort.Slice(files, func(i, j int) bool {
		return naturalLess(files[i].OriginalFilename, files[j].OriginalFilename)
	})

	// Calculate pagination info
	totalPages := int((totalFiles + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}

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

	// Get flash message if any
	flashMsg := flash.Get(w, r)

	render(w, "dashboard.html", map[string]any{
		"Title":         "Dashboard",
		"User":          user,
		"Files":         files,
		"Folders":       folderNames,
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
	})
}

// naturalLess compares two strings using natural ordering (numbers are compared numerically)
func naturalLess(a, b string) bool {
	// Extract numbers from strings like "file (1).txt" and "file (10).txt"
	re := regexp.MustCompile(`\s*\((\d+)\)`)

	aMatches := re.FindStringSubmatch(a)
	bMatches := re.FindStringSubmatch(b)

	// Remove " (n)" pattern to get base name
	aBase := re.ReplaceAllString(a, "")
	bBase := re.ReplaceAllString(b, "")

	// If base names are different, compare alphabetically
	if aBase != bBase {
		return aBase < bBase
	}

	// Same base name - file without number comes first
	aHasNum := len(aMatches) > 1
	bHasNum := len(bMatches) > 1

	if !aHasNum && bHasNum {
		return true // a (no number) comes before b (has number)
	}
	if aHasNum && !bHasNum {
		return false // b (no number) comes before a (has number)
	}

	// Both have numbers, compare numerically
	if aHasNum && bHasNum {
		aNum, _ := strconv.Atoi(aMatches[1])
		bNum, _ := strconv.Atoi(bMatches[1])
		return aNum < bNum
	}

	// Default to string comparison
	return a < b
}
