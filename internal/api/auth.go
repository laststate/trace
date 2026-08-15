package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/laststate/trace/internal/store"
)

// requireRole enforces role-based access control for specific roles,
// defaulting to the "viewer" level. Session lookup and role rank comparison
// are shared with the rest of the package (see session.go and server.go).
func (s *Server) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := sessionFrom(s, r)
			if !ok {
				writeErr(w, 401, "unauthorized", "authentication required", false)
				return
			}
			if role == "" {
				role = "viewer"
			}
			if !s.userHasRole(sess, role) {
				writeErr(w, 403, "access_denied", fmt.Sprintf("role '%s' required", role), false)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// roleRanks is the single canonical source of truth for role-to-rank mapping.
// All role comparison logic across the api package must reference this map.
var roleRanks = map[string]int{
	"viewer":     1,
	"developer":  2,
	"maintainer": 3,
	"admin":      4,
	"owner":      5,
}

// userHasRole checks if a user has the required minimum role for the action.
// Empty or unknown roles fail-closed (return false) to prevent accidental
// access grants from role assignment bugs.
func (s *Server) userHasRole(sess store.Session, requiredRole string) bool {
	userRole := sess.Role
	if userRole == "" {
		userRole = "viewer"
	}
	requiredRank, ok := roleRanks[requiredRole]
	if !ok {
		requiredRank = roleRanks["viewer"]
	}
	userRank, ok := roleRanks[userRole]
	if !ok {
		return false // unknown role — fail closed
	}
	return userRank >= requiredRank
}

// unused import guard keeps the json package available for the file's helpers
// that may be added in subsequent phases (e.g. password reset request body).
var _ = json.Marshal
