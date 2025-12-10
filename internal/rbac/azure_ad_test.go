package rbac

import (
	"context"
	"testing"
	"time"
)

func TestNewRBACManager(t *testing.T) {
	manager := NewRBACManager()
	if manager == nil {
		t.Fatal("NewRBACManager returned nil")
	}
	if manager.cache == nil {
		t.Fatal("Cache map not initialized")
	}
	if manager.cacheTTL != 1*time.Hour {
		t.Errorf("Expected cache TTL of 1 hour, got %v", manager.cacheTTL)
	}
}

func TestGetCurrentUserEmail(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	email, err := manager.GetCurrentUserEmail(ctx)
	if err != nil {
		t.Fatalf("Failed to get current user: %v", err)
	}

	if email == "" {
		t.Fatal("Email is empty")
	}

	t.Logf("Current user email: %s", email)
}

func TestGetUserGroups(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	email, err := manager.GetCurrentUserEmail(ctx)
	if err != nil {
		t.Fatalf("Failed to get current user: %v", err)
	}

	groups, err := manager.GetUserGroups(ctx, email)
	if err != nil {
		t.Fatalf("Failed to get user groups: %v", err)
	}

	if len(groups) == 0 {
		t.Log("Warning: User has no groups")
	}

	t.Logf("User %s has %d groups", email, len(groups))
	for i, group := range groups {
		t.Logf("  [%d] %s (ID: %s)", i+1, group.DisplayName, group.ID)
	}
}

func TestCheckCurrentUserIsSRE(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	isSRE, err := manager.CheckCurrentUserIsSRE(ctx)
	if err != nil {
		t.Fatalf("Failed to check SRE status: %v", err)
	}

	t.Logf("User is SRE: %v", isSRE)
}

func TestGetUserPermissions(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	email, err := manager.GetCurrentUserEmail(ctx)
	if err != nil {
		t.Fatalf("Failed to get current user: %v", err)
	}

	perms, err := manager.GetUserPermissions(ctx, email)
	if err != nil {
		t.Fatalf("Failed to get user permissions: %v", err)
	}

	if perms.Email != email {
		t.Errorf("Expected email %s, got %s", email, perms.Email)
	}

	t.Logf("Permissions for %s:", perms.Email)
	t.Logf("  - Is SRE: %v", perms.IsSRE)
	t.Logf("  - Groups: %d", len(perms.Groups))
	t.Logf("  - Cached at: %s", perms.CachedAt.Format(time.RFC3339))
}

func TestCacheInvalidation(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	email, err := manager.GetCurrentUserEmail(ctx)
	if err != nil {
		t.Fatalf("Failed to get current user: %v", err)
	}

	// First call - should fetch from Azure CLI
	_, err = manager.GetUserPermissions(ctx, email)
	if err != nil {
		t.Fatalf("Failed to get user permissions: %v", err)
	}

	// Second call - should use cache
	startTime := time.Now()
	_, err = manager.GetUserPermissions(ctx, email)
	if err != nil {
		t.Fatalf("Failed to get cached user permissions: %v", err)
	}
	duration := time.Since(startTime)

	if duration > 100*time.Millisecond {
		t.Errorf("Cache not working, call took %v", duration)
	}

	t.Logf("Cached call took: %v", duration)

	// Invalidate cache
	manager.InvalidateUserCache(email)

	// Third call - should fetch again
	_, err = manager.GetUserPermissions(ctx, email)
	if err != nil {
		t.Fatalf("Failed to get user permissions after cache invalidation: %v", err)
	}

	t.Log("Cache invalidation successful")
}

func TestCheckUserInGroup(t *testing.T) {
	manager := NewRBACManager()
	ctx := context.Background()

	email, err := manager.GetCurrentUserEmail(ctx)
	if err != nil {
		t.Fatalf("Failed to get current user: %v", err)
	}

	// Test existing group (case-insensitive)
	isMember, err := manager.CheckUserInGroup(ctx, email, "vv_cloud_sre")
	if err != nil {
		t.Fatalf("Failed to check group membership: %v", err)
	}
	t.Logf("User is member of vv_cloud_sre: %v", isMember)

	// Test non-existing group
	isMember, err = manager.CheckUserInGroup(ctx, email, "NonExistentGroup12345")
	if err != nil {
		t.Fatalf("Failed to check group membership: %v", err)
	}
	if isMember {
		t.Error("User should not be member of non-existent group")
	}
}
