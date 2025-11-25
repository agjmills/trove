package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type User struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Username     string         `gorm:"uniqueIndex;not null;size:50" json:"username"`
	Email        string         `gorm:"uniqueIndex;not null;size:255" json:"email"`
	PasswordHash string         `gorm:"not null;size:255" json:"-"`
	StorageQuota int64          `gorm:"not null;default:10737418240" json:"storage_quota"`
	StorageUsed  int64          `gorm:"not null;default:0" json:"storage_used"`
	IsAdmin      bool           `gorm:"not null;default:false" json:"is_admin"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`

	Files   []File   `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Folders []Folder `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

type Folder struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	UserID     uint           `gorm:"not null;index:idx_user_folder_path" json:"user_id"`
	FolderPath string         `gorm:"not null;size:1024;index:idx_user_folder_path" json:"folder_path"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

type File struct {
	ID               uint                                  `gorm:"primaryKey" json:"id"`
	UserID           uint                                  `gorm:"not null;index" json:"user_id"`
	StoragePath      string                                `gorm:"not null;size:1024;index" json:"storage_path"`             // UUID-based path for storage operations (not unique - deduplication)
	LogicalPath      string                                `gorm:"not null;size:1024;default:'/';index" json:"logical_path"` // Logical folder path for UI navigation
	Filename         string                                `gorm:"not null;size:255" json:"filename"`                        // Display name (editable)
	OriginalFilename string                                `gorm:"not null;size:255" json:"original_filename"`               // Original name (immutable)
	FileSize         int64                                 `gorm:"not null" json:"file_size"`
	MimeType         string                                `gorm:"size:100" json:"mime_type"`
	Hash             string                                `gorm:"index;size:64" json:"hash"`
	Metadata         datatypes.JSONType[map[string]string] `json:"metadata"` // Arbitrary key-value metadata
	Tags             datatypes.JSONType[[]string]          `json:"tags"`     // Simple string tags for filtering
	CreatedAt        time.Time                             `json:"created_at"`
	UpdatedAt        time.Time                             `json:"updated_at"`
	DeletedAt        gorm.DeletedAt                        `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}
