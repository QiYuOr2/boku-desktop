// Package deepseek holds the identity of the DeepSeek service: the name it is
// registered under, the credential key it is stored with, and where a user
// gets that credential.
//
// It exists because two features talk to the same service: the balance panel
// (internal/panel/deepseek) and the translator (internal/translate/deepseek).
// They are separate features with separate lifetimes, but they must agree on
// the provider id and on the credential key — "deepseek.apiKey" is one entry
// in the credential store and one name in the cross-end contract
// (docs/providers.md §7), and two copies of a string that must match is two
// chances to get it wrong.
//
// Nothing here does anything: it is the service's name and address, and the
// features that use them keep their own code.
package deepseek

// ProviderID is the service's id: the provider id the balance panel is
// registered under, and the translator id the translation tool offers.
const ProviderID = "deepseek"

// APIKeyCredential is the credential both of them need, without the provider
// prefix: the store keeps it as CredentialKey(ProviderID, APIKeyCredential).
const APIKeyCredential = "apiKey"

// DocsURL is where the user creates the key. Both features show it, because
// both of them use the one credential.
const DocsURL = "https://platform.deepseek.com/api_keys"
