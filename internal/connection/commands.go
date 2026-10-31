package connection

import (
	"context"

	"rest-bed-app/internal/bed"
)

// The bed's control endpoints are all PUTs of an absolute value, so applying
// one twice leaves the bed exactly as applying it once does. That is what makes
// every command below idempotent, and so safe for Do to resend after a timeout
// that may or may not have been applied. A relative or toggling command (none
// exists today) must pass idempotent=false, and is then retried only after a
// failure that proves it was never delivered.
//
//	SetMode           PUT /preferences/mode          absolute  idempotent
//	SetFirmness       PUT /preferences/firmness      absolute  idempotent
//	SetManualProfile  PUT /preferences/manualprofile absolute  idempotent
//	StartFilling      PUT /air/filling true          absolute  idempotent
//	Stop              PUT /air/flows off + filling false  absolute  idempotent
//	SetConfiguration  PUT /device/configuration      absolute  idempotent

// SetMode sets the bed's operating mode on the picked bed.
func (m *Manager) SetMode(ctx context.Context, mode string) error {
	return m.Do(ctx, true, func(ctx context.Context, c *bed.Client) error { return c.SetMode(ctx, mode) })
}

// SetFirmness sets the overall firmness on the picked bed.
func (m *Manager) SetFirmness(ctx context.Context, value int) error {
	return m.Do(ctx, true, func(ctx context.Context, c *bed.Client) error { return c.SetFirmness(ctx, value) })
}

// SetManualProfile sets the four manual-mode zone targets on the picked bed.
func (m *Manager) SetManualProfile(ctx context.Context, zones [4]int) error {
	return m.Do(ctx, true, func(ctx context.Context, c *bed.Client) error { return c.SetManualProfile(ctx, zones) })
}

// StartFilling starts the fill cycle on the picked bed.
func (m *Manager) StartFilling(ctx context.Context) error {
	return m.Do(ctx, true, func(ctx context.Context, c *bed.Client) error { return c.StartFilling(ctx) })
}

// Stop halts all zones on the picked bed.
func (m *Manager) Stop(ctx context.Context) error {
	return m.Do(ctx, true, func(ctx context.Context, c *bed.Client) error { return c.Stop(ctx) })
}

// SetConfiguration sets the configuration string on the picked bed.
func (m *Manager) SetConfiguration(ctx context.Context, config string) error {
	return m.Do(ctx, true, func(ctx context.Context, c *bed.Client) error { return c.SetConfiguration(ctx, config) })
}
