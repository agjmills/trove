package flash

import (
	"net/http/httptest"
	"testing"
)

func TestSetAndGet(t *testing.T) {
	tests := []struct {
		name        string
		msgType     string
		content     string
		wantType    string
		wantContent string
	}{
		{
			name:        "error message",
			msgType:     "error",
			content:     "Something went wrong",
			wantType:    "error",
			wantContent: "Something went wrong",
		},
		{
			name:        "success message",
			msgType:     "success",
			content:     "Operation completed successfully",
			wantType:    "success",
			wantContent: "Operation completed successfully",
		},
		{
			name:        "info message",
			msgType:     "info",
			content:     "FYI: Important information",
			wantType:    "info",
			wantContent: "FYI: Important information",
		},
		{
			name:        "warning message",
			msgType:     "warning",
			content:     "Please be careful",
			wantType:    "warning",
			wantContent: "Please be careful",
		},
		{
			name:        "empty content",
			msgType:     "error",
			content:     "",
			wantType:    "error",
			wantContent: "",
		},
		{
			name:        "content with colon",
			msgType:     "info",
			content:     "Username: john",
			wantType:    "info",
			wantContent: "Username: john",
		},
		{
			name:        "special characters",
			msgType:     "error",
			content:     "Error: File 'test.txt' not found!",
			wantType:    "error",
			wantContent: "Error: File 'test.txt' not found!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create response recorder for setting flash
			w := httptest.NewRecorder()
			Set(w, tt.msgType, tt.content)

			// Get the cookie from the response
			cookies := w.Result().Cookies()
			if len(cookies) == 0 {
				t.Fatal("No cookie set")
			}

			// Create a new request with the cookie for retrieval
			r := httptest.NewRequest("GET", "/", nil)
			r.AddCookie(cookies[0])

			// Create new recorder for getting flash
			w2 := httptest.NewRecorder()
			msg := Get(w2, r)

			if msg == nil {
				t.Fatal("Get() returned nil")
			}

			if msg.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", msg.Type, tt.wantType)
			}

			if msg.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", msg.Content, tt.wantContent)
			}

			// Verify cookie was cleared
			cookies2 := w2.Result().Cookies()
			if len(cookies2) == 0 {
				t.Fatal("No cookie set after Get()")
			}

			// The cookie should have MaxAge -1 (to delete it)
			if cookies2[0].MaxAge != -1 {
				t.Errorf("Cookie MaxAge = %d, want -1 (deleted)", cookies2[0].MaxAge)
			}
		})
	}
}

func TestGet_NoCookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	msg := Get(w, r)
	if msg != nil {
		t.Errorf("Get() = %v, want nil when no cookie present", msg)
	}
}

func TestError(t *testing.T) {
	w := httptest.NewRecorder()
	Error(w, "Test error")

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Error() did not set cookie")
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])

	w2 := httptest.NewRecorder()
	msg := Get(w2, r)

	if msg == nil {
		t.Fatal("Get() returned nil")
	}

	if msg.Type != "error" {
		t.Errorf("Type = %q, want \"error\"", msg.Type)
	}

	if msg.Content != "Test error" {
		t.Errorf("Content = %q, want \"Test error\"", msg.Content)
	}
}

func TestSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	Success(w, "Test success")

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Success() did not set cookie")
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])

	w2 := httptest.NewRecorder()
	msg := Get(w2, r)

	if msg == nil {
		t.Fatal("Get() returned nil")
	}

	if msg.Type != "success" {
		t.Errorf("Type = %q, want \"success\"", msg.Type)
	}

	if msg.Content != "Test success" {
		t.Errorf("Content = %q, want \"Test success\"", msg.Content)
	}
}

func TestInfo(t *testing.T) {
	w := httptest.NewRecorder()
	Info(w, "Test info")

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Info() did not set cookie")
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])

	w2 := httptest.NewRecorder()
	msg := Get(w2, r)

	if msg == nil {
		t.Fatal("Get() returned nil")
	}

	if msg.Type != "info" {
		t.Errorf("Type = %q, want \"info\"", msg.Type)
	}

	if msg.Content != "Test info" {
		t.Errorf("Content = %q, want \"Test info\"", msg.Content)
	}
}

func TestWarning(t *testing.T) {
	w := httptest.NewRecorder()
	Warning(w, "Test warning")

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Warning() did not set cookie")
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])

	w2 := httptest.NewRecorder()
	msg := Get(w2, r)

	if msg == nil {
		t.Fatal("Get() returned nil")
	}

	if msg.Type != "warning" {
		t.Errorf("Type = %q, want \"warning\"", msg.Type)
	}

	if msg.Content != "Test warning" {
		t.Errorf("Content = %q, want \"Test warning\"", msg.Content)
	}
}

func TestGet_ClearsMessage(t *testing.T) {
	// Set a flash message
	w := httptest.NewRecorder()
	Error(w, "First message")

	cookies := w.Result().Cookies()
	
	// Get the message (should clear it)
	r1 := httptest.NewRequest("GET", "/", nil)
	r1.AddCookie(cookies[0])
	w2 := httptest.NewRecorder()
	msg1 := Get(w2, r1)
	if msg1 == nil {
		t.Fatal("First Get() returned nil")
	}

	// Create a new request without the cookie (simulating next request after clear)
	r2 := httptest.NewRequest("GET", "/", nil)
	w3 := httptest.NewRecorder()
	msg2 := Get(w3, r2)
	if msg2 != nil {
		t.Error("Second Get() should return nil after message was cleared")
	}
}

func TestMessage_MultipleColons(t *testing.T) {
	w := httptest.NewRecorder()
	Set(w, "info", "URL: http://example.com:8080/path")

	cookies := w.Result().Cookies()
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])

	w2 := httptest.NewRecorder()
	msg := Get(w2, r)

	if msg == nil {
		t.Fatal("Get() returned nil")
	}

	if msg.Type != "info" {
		t.Errorf("Type = %q, want \"info\"", msg.Type)
	}

	expected := "URL: http://example.com:8080/path"
	if msg.Content != expected {
		t.Errorf("Content = %q, want %q", msg.Content, expected)
	}
}
