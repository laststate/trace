package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/laststate/trace/internal/store"
)

// aclMiddleware checks if a user has access to the requested resource based on project and organization permissions.
// This is for more advanced permission checking beyond simple roles.
type aclMiddleware struct {
	store *store.Store
}

func (a *aclMiddleware) checkUserHasProjectAccess(ctx context.Context, userID, projectID uuid.UUID) (bool, string, error) {
	return a.store.UserCanAccessProject(ctx, userID, projectID)
}

// checkProjectAccess validates that the project belongs to the organization in the session.
func (s *Server) checkProjectAccess(r *http.Request, projectID uuid.UUID) (bool, error) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		return false, store.ErrNotFound
	}

	// Check if it's the user's project
	ok, _, err := s.Store.UserCanAccessProject(r.Context(), sess.UserID, projectID)
	if err != nil {
		return false, err
	}

	return ok, nil
}

// checkOrgAccess validates that a user has access to an organization.
func (s *Server) checkOrgAccess(r *http.Request, orgID uuid.UUID) error {
	sess, ok := sessionFrom(s, r)
	if !ok {
		return store.ErrNotFound
	}

	// Future: expand to check if the user has access to the org via memberships.
	// For now, ensure the session org matches.
	if sess.OrganizationID != orgID {
		return store.ErrNotFound
	}

	return nil
}