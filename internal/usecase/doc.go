// Package usecase holds one complete action per type: reading a panel,
// refreshing it, storing a credential. It depends on internal/domain and on
// nothing else, so it can be tested without a window, a network or a file.
//
// Presentation calls these; providers and stores implement the interfaces
// they are given.
package usecase
