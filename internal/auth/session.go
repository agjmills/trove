package auth

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/agjmills/trove/internal/database/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func HashToken(token string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func CreateSession(db *gorm.DB, userID uint, duration time.Duration) (string, error) {
	token, err := GenerateToken()
	if err != nil {
		return "", err
	}

	tokenHash, err := HashToken(token)
	if err != nil {
		return "", err
	}

	session := models.Session{
		UserID:     userID,
		TokenHash:  tokenHash,
		ExpiresAt:  time.Now().Add(duration),
		LastUsedAt: time.Now(),
	}

	if err := db.Create(&session).Error; err != nil {
		return "", err
	}

	return token, nil
}

func ValidateSession(db *gorm.DB, token string) (*models.User, error) {
	var sessions []models.Session
	if err := db.Where("expires_at > ?", time.Now()).Find(&sessions).Error; err != nil {
		return nil, err
	}

	for _, session := range sessions {
		if bcrypt.CompareHashAndPassword([]byte(session.TokenHash), []byte(token)) == nil {
			db.Model(&session).Update("last_used_at", time.Now())

			var user models.User
			if err := db.First(&user, session.UserID).Error; err != nil {
				return nil, err
			}
			return &user, nil
		}
	}

	return nil, gorm.ErrRecordNotFound
}

func DeleteSession(db *gorm.DB, token string) error {
	var sessions []models.Session
	if err := db.Find(&sessions).Error; err != nil {
		return err
	}

	for _, session := range sessions {
		if bcrypt.CompareHashAndPassword([]byte(session.TokenHash), []byte(token)) == nil {
			return db.Delete(&session).Error
		}
	}

	return nil
}
