package ipc

import (
	"fmt"
	"net"
	"os/user"
	"strconv"

	"elepn/daemon/internal/derr"
)

// AuthGroup is the system group name peers must belong to. Members of this
// group have IPC access; non-members are rejected at accept time.
const AuthGroup = "xrayd"

// AuthAccept performs the SO_PEERCRED + group membership check described in
// §8.6 of the spec. Returns nil on success or derr.ErrUnauthorized on any
// failure path (denied uid, NSS error, missing group).
//
// The check is performed once per connection at accept time. Group membership
// is queried via NSS (os/user.GroupIds), so LDAP/SSSD setups work without
// parsing /etc/group directly.
func AuthAccept(c *net.UnixConn) error {
	uid, err := readPeerUID(c)
	if err != nil {
		return derr.ErrUnauthorized.With(err)
	}
	if uid == 0 {
		// Root is always allowed. Pragmatic: makes systemctl-driven testing easy
		// and matches Mullvad's behavior. The daemon runs as xrayd, not root —
		// this only matters for `sudo socat ...` style debugging.
		return nil
	}
	u, err := user.LookupId(strconv.Itoa(int(uid)))
	if err != nil {
		return derr.ErrUnauthorized.With(fmt.Errorf("LookupId(%d): %w", uid, err))
	}
	gids, err := u.GroupIds()
	if err != nil {
		return derr.ErrUnauthorized.With(fmt.Errorf("GroupIds(%s): %w", u.Username, err))
	}
	xrayGroup, err := user.LookupGroup(AuthGroup)
	if err != nil {
		return derr.ErrUnauthorized.With(fmt.Errorf("LookupGroup(%s): %w", AuthGroup, err))
	}
	for _, g := range gids {
		if g == xrayGroup.Gid {
			return nil
		}
	}
	return derr.ErrUnauthorized.WithMessage(
		fmt.Sprintf("uid %d not in group %s", uid, AuthGroup),
	)
}
