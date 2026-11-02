//go:build darwin && cgo && integration

package ble

import "zwiftboard/internal/keys"

// NewIntegrationHolds exposes the production session hold path only to tagged
// integration tests. InstallIntegrationKeyboard replaces OS output separately.
func NewIntegrationHolds() *Holds {
	return holdKeys(keys.Down, keys.Up)
}
