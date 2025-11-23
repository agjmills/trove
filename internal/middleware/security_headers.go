package middleware

import "net/http"

// SecurityHeaders adds security-related HTTP headers to all responses
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking attacks
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Enable XSS protection (legacy browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy - only send referrer for same origin
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy
		// Restricts resource loading to prevent XSS attacks
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline'; " + // Allow inline scripts for upload progress
			"style-src 'self' 'unsafe-inline'; " + // Allow inline styles
			"img-src 'self' data:; " +
			"font-src 'self'; " +
			"connect-src 'self'; " +
			"frame-ancestors 'none'"
		w.Header().Set("Content-Security-Policy", csp)

		// Permissions Policy (formerly Feature-Policy)
		// Disable unnecessary browser features
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// Only set HSTS in production with HTTPS
		// Uncomment when using HTTPS in production:
		// if r.TLS != nil {
		// 	w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		// }

		next.ServeHTTP(w, r)
	})
}
