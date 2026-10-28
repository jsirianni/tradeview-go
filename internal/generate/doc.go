// Package generate maintains the pinned TradingView broker bindings.
// Regeneration is an explicit maintenance operation; ordinary package tests
// leave the committed source unchanged.
//
//go:generate go test -run TestRegenerateBindings -count=1 -args -write
package generate
