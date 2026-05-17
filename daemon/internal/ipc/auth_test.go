package ipc

import (
	"os/user"
	"testing"
)

func TestCheckGroupExistsRejectsUnknown(t *testing.T) {
	// A group name no sane sysadmin would create. If this ever exists on a
	// test machine, that machine has bigger problems than a flaky test.
	const bogus = "xrayd-bogus-nonexistent-group-7df3"
	if err := CheckGroupExists(bogus); err == nil {
		t.Fatalf("CheckGroupExists(%q) = nil, want lookup error", bogus)
	}
}

func TestCheckGroupExistsAcceptsRealGroup(t *testing.T) {
	// Pick whatever group the current user is in — there must be at least
	// one. This proves the positive path without depending on a specific
	// installed group like xrayd.
	u, err := user.Current()
	if err != nil {
		t.Skipf("user.Current: %v", err)
	}
	gids, err := u.GroupIds()
	if err != nil || len(gids) == 0 {
		t.Skipf("no groups available for current user: %v", err)
	}
	g, err := user.LookupGroupId(gids[0])
	if err != nil {
		t.Skipf("LookupGroupId(%s): %v", gids[0], err)
	}
	if err := CheckGroupExists(g.Name); err != nil {
		t.Fatalf("CheckGroupExists(%q) = %v, want nil for the user's own group", g.Name, err)
	}
}
