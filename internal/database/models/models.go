package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type User struct {
	ID                   uint           `gorm:"primaryKey" json:"id"`
	Username             string         `gorm:"uniqueIndex;not null;size:50" json:"username"`
	Email                string         `gorm:"uniqueIndex;not null;size:255" json:"email"`
	PasswordHash         string         `gorm:"not null;size:255" json:"-"`
	StorageQuota         int64          `gorm:"not null;default:10737418240" json:"storage_quota"`
	StorageUsed          int64          `gorm:"not null;default:0" json:"storage_used"`
	IsAdmin              bool           `gorm:"not null;default:false" json:"is_admin"`
	DeletedRetentionDays *int           `gorm:"column:trash_retention_days;default:null" json:"deleted_retention_days,omitempty"` // Per-user deleted items retention (nil = use system default)
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`

	Files   []File   `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Folders []Folder `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

type Folder struct {
	ID                 uint           `gorm:"primaryKey" json:"id"`
	UserID             uint           `gorm:"not null;index:idx_user_folder_path" json:"user_id"`
	FolderPath         string         `gorm:"not null;size:1024;index:idx_user_folder_path" json:"folder_path"`
	SoftDeletedAt      *time.Time     `gorm:"column:trashed_at;index" json:"soft_deleted_at,omitempty"` // When folder was soft-deleted (nil = not deleted)
	OriginalFolderPath string         `gorm:"size:1024" json:"original_folder_path,omitempty"`          // Original path before deletion (for restore)
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

type File struct {
	ID                  uint                                  `gorm:"primaryKey" json:"id"`
	UserID              uint                                  `gorm:"not null;index" json:"user_id"`
	StoragePath         string                                `gorm:"not null;size:1024;index" json:"storage_path"`             // UUID-based path for storage operations (not unique - deduplication)
	LogicalPath         string                                `gorm:"not null;size:1024;default:'/';index" json:"logical_path"` // Logical folder path for UI navigation
	Filename            string                                `gorm:"not null;size:255" json:"filename"`                        // Display name (editable)
	OriginalFilename    string                                `gorm:"not null;size:255" json:"original_filename"`               // Original name (immutable)
	FileSize            int64                                 `gorm:"not null" json:"file_size"`
	MimeType            string                                `gorm:"size:100" json:"mime_type"`
	Hash                string                                `gorm:"index;size:64" json:"hash"`
	UploadStatus        string                                `gorm:"size:20;default:'completed';index" json:"upload_status"`   // Upload status: pending, uploading, completed, failed
	ErrorMessage        string                                `gorm:"size:500" json:"error_message,omitempty"`                  // Error message for failed uploads
	TempPath            string                                `gorm:"size:1024" json:"-"`                                       // Temporary local path (used during async upload, not shown to user)
	Metadata            datatypes.JSONType[map[string]string] `json:"metadata"`                                                 // Arbitrary key-value metadata
	Tags                datatypes.JSONType[[]string]          `json:"tags"`                                                     // Simple string tags for filtering
	SoftDeletedAt       *time.Time                            `gorm:"column:trashed_at;index" json:"soft_deleted_at,omitempty"` // When file was soft-deleted (nil = not deleted)
	OriginalLogicalPath string                                `gorm:"size:1024" json:"original_logical_path,omitempty"`         // Original path before deletion (for restore)
	CreatedAt           time.Time                             `json:"created_at"`
	UpdatedAt           time.Time                             `json:"updated_at"`
	DeletedAt           gorm.DeletedAt                        `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

// UploadSession tracks state for resumable chunked uploads
type UploadSession struct {
	ID             string         `gorm:"primaryKey;size:36" json:"id"` // UUID
	UserID         uint           `gorm:"not null;index" json:"user_id"`
	Filename       string         `gorm:"not null;size:255" json:"filename"`
	LogicalPath    string         `gorm:"not null;size:1024;default:'/'" json:"logical_path"`
	TotalSize      int64          `gorm:"not null" json:"total_size"`
	TotalChunks    int            `gorm:"not null" json:"total_chunks"`
	ChunkSize      int64          `gorm:"not null" json:"chunk_size"`
	ReceivedChunks int            `gorm:"not null;default:0" json:"received_chunks"`
	ChunksReceived datatypes.JSON `gorm:"type:json" json:"chunks_received"`             // Array of chunk numbers received
	Status         string         `gorm:"size:20;default:'active';index" json:"status"` // active, completed, cancelled, expired
	Hash           string         `gorm:"size:64" json:"hash,omitempty"`                // Expected hash for verification (optional)
	MimeType       string         `gorm:"size:100" json:"mime_type"`
	TempDir        string         `gorm:"size:1024" json:"temp_dir"` // Temporary directory for chunks
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	ExpiresAt      time.Time      `gorm:"index" json:"expires_at"` // When this session should be cleaned up
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}
