package models

import (
	"time"

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

	Files    []File    `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Folders  []Folder  `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Sessions []Session `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
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
	ID               uint           `gorm:"primaryKey" json:"id"`
	UserID           uint           `gorm:"not null;index" json:"user_id"`
	Filename         string         `gorm:"not null;size:255" json:"filename"`
	OriginalFilename string         `gorm:"not null;size:255" json:"original_filename"`
	FilePath         string         `gorm:"not null;size:1024" json:"file_path"`
	FileSize         int64          `gorm:"not null" json:"file_size"`
	MimeType         string         `gorm:"size:100" json:"mime_type"`
	Hash             string         `gorm:"index;size:64" json:"hash"`
	FolderPath       string         `gorm:"size:1024;default:/" json:"folder_path"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

type Session struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"not null;index" json:"user_id"`
	TokenHash  string    `gorm:"uniqueIndex;not null;size:255" json:"-"`
	ExpiresAt  time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}
