package state

import (
	"context"
	"errors"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/supervisor"
	"elepn/daemon/internal/xrayconfig"
)

// doConnect is the long-running worker that does the heavy lifting outside
// the actor goroutine. Posts cmdConnectProgress mid-flight via the `progress`
// callback; returns a connectResult that the caller dispatches as cmdConnectDone.
// Spec §3.5.
func doConnect(
	ctx context.Context,
	d deps,
	progress func(command) bool,
	gen int64,
	id xrayconfig.ULID,
) (result connectResult) {
	result.id = id
	cu := newCleanupStack()
	defer func() {
		if result.err != nil {
			cu.run()
			result.cleanup = nil
		} else {
			result.cleanup = cu
		}
	}()

	// Guard against cancellation in the window between goroutine spawn and first use of ctx.
	if err := ctx.Err(); err != nil {
		result.err = err
		return
	}

	cfgPath, err := d.cfgs.PathFor(id)
	if err != nil {
		result.err = err
		return
	}
	if vr, err := d.cfgs.Validate(ctx, id); err != nil {
		result.err = err
		return
	} else if !vr.OK {
		result.err = derr.ErrConfigInvalid.WithMessage(vr.Error)
		return
	}

	// Validation OK — tell the actor to flip Validating → Connecting.
	if !progress(cmdConnectProgress{gen: gen, newState: StateConnecting}) {
		result.err = ctx.Err()
		return
	}

	child, err := d.sup.Start(ctx, cfgPath)
	if err != nil {
		result.err = derr.WrapSpawn(err)
		return
	}
	cu.push("stop-xray", func() {
		stopCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = d.sup.Stop(stopCtx, child, 5*time.Second)
	})
	result.child = child

	if err := supervisor.AwaitProcessAlive(ctx, child, 1*time.Second); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.err = derr.ErrConnectTimeout.With(err)
			return
		}
		result.err = derr.WrapDiedEarly(err)
		return
	}
	// 10s budget covers xray's cold-cache geodata indexing on HDD (3 MB geosite.dat
	// with 60k+ entries) before the SOCKS inbound binds.
	if err := supervisor.AwaitSocksReady(ctx, d.cfg.SocksAddr, 10*time.Second); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.err = derr.ErrConnectTimeout.With(err)
			return
		}
		result.err = derr.WrapInbound(err)
		return
	}
	return
}
