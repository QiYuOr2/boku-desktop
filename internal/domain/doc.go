// Package domain is the contract of the application: the entities, the
// interfaces the layers around it implement, and the errors they return.
//
// It depends on nothing — no user interface, no SQLite, no HTTP, no MyGo —
// which is what keeps the other layers replaceable and their tests offline.
// Vendor names appear nowhere here: a provider's shape is turned into these
// entities by an adapter.
package domain
