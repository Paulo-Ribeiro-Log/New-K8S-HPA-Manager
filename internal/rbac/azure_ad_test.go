package rbac

import (
	"context"
	"testing"
	"time"
)

func TestNewRBACManager(t *testing.T) {
	manager := NewRBACManager(false)
	if manager == nil {
		t.Fatal("NewRBACManager returned nil")
	}
	if manager.cache == nil {
		t.Fatal("Cache map not initialized")
	}
	if manager.cacheTTL != 1*time.Hour {
		t.Errorf("Expected cache TTL of 1 hour, got %v", manager.cacheTTL)
	}
	if manager.disableADCheck != false {
		t.Error("Expected disableADCheck to be false")
	}
}

func TestNewRBACManagerWithBypass(t *testing.T) {
	manager := NewRBACManager(true)
	if manager == nil {
		t.Fatal("NewRBACManager returned nil")
	}
	if manager.disableADCheck != true {
		t.Error("Expected disableADCheck to be true")
	}
}

func TestGetCurrentUserEmail(t *testing.T) {
	manager := NewRBACManager(false)
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
	manager := NewRBACManager(false)
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
	manager := NewRBACManager(false)
	ctx := context.Background()

	isSRE, err := manager.CheckCurrentUserIsSRE(ctx)
	if err != nil {
		t.Fatalf("Failed to check SRE status: %v", err)
	}

	t.Logf("User is SRE: %v", isSRE)
}

func TestGetUserPermissions(t *testing.T) {
	manager := NewRBACManager(false)
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
	manager := NewRBACManager(false)
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
	manager := NewRBACManager(false)
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

func TestBypassMode(t *testing.T) {
	manager := NewRBACManager(true) // Bypass enabled
	ctx := context.Background()

	// CheckCurrentUserIsSRE should always return true in bypass mode
	isSRE, err := manager.CheckCurrentUserIsSRE(ctx)
	if err != nil {
		t.Fatalf("Failed to check SRE status in bypass mode: %v", err)
	}
	if !isSRE {
		t.Error("Expected isSRE to be true in bypass mode")
	}

	// GetCurrentUserPermissions should return bypass permissions
	perms, err := manager.GetCurrentUserPermissions(ctx)
	if err != nil {
		t.Fatalf("Failed to get permissions in bypass mode: %v", err)
	}
	if perms.Email != "bypass@emergency.mode" {
		t.Errorf("Expected bypass email, got %s", perms.Email)
	}
	if !perms.IsSRE {
		t.Error("Expected isSRE to be true in bypass mode")
	}
	if len(perms.Groups) != 1 || perms.Groups[0].DisplayName != "EMERGENCY_MODE" {
		t.Error("Expected EMERGENCY_MODE group in bypass mode")
	}

	t.Logf("Bypass mode test passed - Email: %s, IsSRE: %v", perms.Email, perms.IsSRE)
}
