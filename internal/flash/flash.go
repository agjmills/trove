package flash

import (
	"encoding/base64"
	"net/http"
	"time"
)

const (
	flashCookieName = "flash_message"
)

type Message struct {
	Type    string // "error", "success", "info", "warning"
	Content string
}

// Set creates a flash message that will be available on the next request
func Set(w http.ResponseWriter, msgType, content string) {
	msg := msgType + ":" + content
	encoded := base64.StdEncoding.EncodeToString([]byte(msg))

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60, // 60 seconds
	})
}

// Get retrieves and clears the flash message
func Get(w http.ResponseWriter, r *http.Request) *Message {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return nil
	}

	// Clear the cookie immediately
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	// Decode the message
	decoded, err := base64.StdEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil
	}

	msg := string(decoded)
	// Split on first colon
	for i := 0; i < len(msg); i++ {
		if msg[i] == ':' {
			return &Message{
				Type:    msg[:i],
				Content: msg[i+1:],
			}
		}
	}

	return nil
}

// Error sets an error flash message
func Error(w http.ResponseWriter, content string) {
	Set(w, "error", content)
}

// Success sets a success flash message
func Success(w http.ResponseWriter, content string) {
	Set(w, "success", content)
}

// Info sets an info flash message
func Info(w http.ResponseWriter, content string) {
	Set(w, "info", content)
}

// Warning sets a warning flash message
func Warning(w http.ResponseWriter, content string) {
	Set(w, "warning", content)
}
