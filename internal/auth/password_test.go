package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		cost     int
		wantErr  bool
	}{
		{
			name:     "valid password with default cost",
			password: "mysecretpassword123",
			cost:     bcrypt.DefaultCost,
			wantErr:  false,
		},
		{
			name:     "valid password with min cost",
			password: "password",
			cost:     bcrypt.MinCost,
			wantErr:  false,
		},
		{
			name:     "valid password with cost 12",
			password: "password",
			cost:     12,
			wantErr:  false,
		},
		{
			name:     "empty password",
			password: "",
			cost:     bcrypt.DefaultCost,
			wantErr:  false, // bcrypt allows empty passwords
		},
		{
			name:     "long password",
			password: strings.Repeat("a", 72), // bcrypt max is 72 bytes
			cost:     bcrypt.DefaultCost,
			wantErr:  false,
		},
		{
			name:     "password exceeding bcrypt limit",
			password: strings.Repeat("a", 73),
			cost:     bcrypt.DefaultCost,
			wantErr:  true, // bcrypt rejects passwords > 72 bytes
		},
		{
			name:     "special characters",
			password: "p@$$w0rd!#%&*()[]{}",
			cost:     bcrypt.DefaultCost,
			wantErr:  false,
		},
		{
			name:     "unicode characters",
			password: "пароль密码🔐",
			cost:     bcrypt.DefaultCost,
			wantErr:  false,
		},
		{
			name:     "invalid cost too low",
			password: "password",
			cost:     bcrypt.MinCost - 1,
			wantErr:  false, // bcrypt will use MinCost instead of erroring
		},
		{
			name:     "invalid cost too high",
			password: "password",
			cost:     bcrypt.MaxCost + 1,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password, tt.cost)
			if (err != nil) != tt.wantErr {
				t.Errorf("HashPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify hash is not empty
				if hash == "" {
					t.Error("HashPassword() returned empty hash")
				}

				// Verify hash format (bcrypt hashes start with $2a$, $2b$, or $2y$)
				if !strings.HasPrefix(hash, "$2") {
					t.Errorf("HashPassword() returned invalid bcrypt hash format: %s", hash)
				}

				// Verify hash is different from original password
				if hash == tt.password {
					t.Error("HashPassword() returned unhashed password")
				}

				// Verify we can use the hash with VerifyPassword
				if !VerifyPassword(hash, tt.password) {
					t.Error("HashPassword() produced hash that doesn't verify")
				}
			}
		})
	}
}

func TestVerifyPassword(t *testing.T) {
	correctPassword := "mypassword123"
	hash, err := HashPassword(correctPassword, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to hash password for test: %v", err)
	}

	tests := []struct {
		name           string
		hashedPassword string
		password       string
		want           bool
	}{
		{
			name:           "correct password",
			hashedPassword: hash,
			password:       correctPassword,
			want:           true,
		},
		{
			name:           "wrong password",
			hashedPassword: hash,
			password:       "wrongpassword",
			want:           false,
		},
		{
			name:           "empty password when non-empty expected",
			hashedPassword: hash,
			password:       "",
			want:           false,
		},
		{
			name:           "case sensitive check",
			hashedPassword: hash,
			password:       "MYPASSWORD123",
			want:           false,
		},
		{
			name:           "password with extra character",
			hashedPassword: hash,
			password:       correctPassword + "x",
			want:           false,
		},
		{
			name:           "password missing character",
			hashedPassword: hash,
			password:       correctPassword[:len(correctPassword)-1],
			want:           false,
		},
		{
			name:           "invalid hash format",
			hashedPassword: "notavalidhash",
			password:       correctPassword,
			want:           false,
		},
		{
			name:           "empty hash",
			hashedPassword: "",
			password:       correctPassword,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifyPassword(tt.hashedPassword, tt.password); got != tt.want {
				t.Errorf("VerifyPassword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerifyPassword_EmptyPasswordHash(t *testing.T) {
	// Test with a password that was hashed as empty
	emptyHash, err := HashPassword("", bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to hash empty password: %v", err)
	}

	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{
			name:     "empty password matches empty hash",
			password: "",
			want:     true,
		},
		{
			name:     "non-empty password doesn't match empty hash",
			password: "something",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VerifyPassword(emptyHash, tt.password); got != tt.want {
				t.Errorf("VerifyPassword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHashPassword_Consistency(t *testing.T) {
	password := "testpassword"

	// Hash same password multiple times
	hash1, err := HashPassword(password, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("First hash failed: %v", err)
	}

	hash2, err := HashPassword(password, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Second hash failed: %v", err)
	}

	// Hashes should be different (bcrypt uses random salt)
	if hash1 == hash2 {
		t.Error("HashPassword() produced identical hashes (salt not random)")
	}

	// But both should verify correctly
	if !VerifyPassword(hash1, password) {
		t.Error("First hash doesn't verify")
	}

	if !VerifyPassword(hash2, password) {
		t.Error("Second hash doesn't verify")
	}
}

func TestHashPassword_DifferentCosts(t *testing.T) {
	password := "testpassword"

	// Hash with different costs
	hashMin, err := HashPassword(password, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("MinCost hash failed: %v", err)
	}

	hashDefault, err := HashPassword(password, bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("DefaultCost hash failed: %v", err)
	}

	// Both should verify
	if !VerifyPassword(hashMin, password) {
		t.Error("MinCost hash doesn't verify")
	}

	if !VerifyPassword(hashDefault, password) {
		t.Error("DefaultCost hash doesn't verify")
	}

	// Hashes should be different
	if hashMin == hashDefault {
		t.Error("Different costs produced identical hashes")
	}
}
