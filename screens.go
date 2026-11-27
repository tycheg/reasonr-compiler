package reasonr

import "github.com/tycheg/reasonr-compiler/internal/uiview"

// Screen descriptions and app manifests for platform runtimes (RFC §22): one
// screen of one app in one language and for one platform. Their JSON is
// the contract between the engine and the runtimes.

// Manifest is an app manifest: the menu, the screens with the hashes of
// their descriptions, the places. The engine adds the caller's state
// (Test, Features, States) before serving it.
type Manifest = uiview.Manifest

// ScreenView is a screen description.
type ScreenView = uiview.Screen

// Manifest is the manifest of app in language lang for platform.
func (m *Model) Manifest(app, lang, platform string) *Manifest {
	return uiview.New(m.UI(), app).Manifest(lang, platform)
}

// Screen is the description of screen of app in language lang for
// platform; nil — the app has no such screen.
func (m *Model) Screen(app, screen, lang, platform string) *ScreenView {
	return uiview.New(m.UI(), app).Screen(screen, lang, platform)
}

// ViewHash is the content hash of a description: its address in the cache.
func ViewHash(v any) string { return uiview.Hash(v) }
