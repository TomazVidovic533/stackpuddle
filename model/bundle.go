package model

import (
	"fmt"
	"io/fs"
	"slices"
)

// Manifest is a project's intent: the bundle version and the packaged modules it selects. It never
// holds resolved digests.
type Manifest struct {
	bundleVersion Version
	modules       []ModuleName
}

// BundleVersion returns the selected bundle version.
func (m Manifest) BundleVersion() Version { return m.bundleVersion }

// Modules returns the selected module names, sorted.
func (m Manifest) Modules() []ModuleName { return slices.Clone(m.modules) }

// Lock pins the exact bundle and module archives a Manifest resolved to, by digest.
type Lock struct {
	bundle   LockedArtifact
	modules  []LockedModule
	document *document
}

// Bundle returns the pinned bundle archive.
func (l Lock) Bundle() LockedArtifact { return l.bundle }

// Modules returns the pinned module archives, ordered by name.
func (l Lock) Modules() []LockedModule { return slices.Clone(l.modules) }

// LockedArtifact is one pinned archive: its version, where to fetch it and the SHA-256 digest of its
// compressed bytes, which alone establishes its identity.
type LockedArtifact struct {
	Version Version
	Source  string
	Digest  string
}

// LockedModule is a pinned module archive.
type LockedModule struct {
	Name ModuleName
	LockedArtifact
}

// Bundle is a tested set: exactly one tested version for each module it contains.
type Bundle struct {
	version Version
	modules []BundleModule
}

// Version returns the bundle's version.
func (b Bundle) Version() Version { return b.version }

// Modules returns the tested module versions, ordered by name.
func (b Bundle) Modules() []BundleModule { return slices.Clone(b.modules) }

// BundleModule is one module version a bundle was tested with.
type BundleModule struct {
	Name    ModuleName
	Version Version
}

// LoadBundle reads and checks the Bundle document at path in fsys. On any diagnostic the Bundle is
// the zero value.
func LoadBundle(fsys fs.FS, path string) (Bundle, Diagnostics) {
	reader, diagnostics := newReader(fsys)
	if len(diagnostics) > 0 {
		return Bundle{}, diagnostics
	}
	doc, diagnostics := reader.loadSingle(path, kindBundle)
	if len(diagnostics) > 0 {
		return Bundle{}, diagnostics
	}
	var wire wireBundle
	if diagnostics := doc.decode(&wire); len(diagnostics) > 0 {
		return Bundle{}, diagnostics
	}
	convert := converter{}
	bundle := Bundle{version: convert.version(wire.Version)}
	for _, name := range sortedKeys(wire.Modules) {
		bundle.modules = append(bundle.modules, BundleModule{
			Name: convert.moduleName(name), Version: convert.version(wire.Modules[name].Version),
		})
	}
	if diagnostics := convert.diagnostics(doc, ""); len(diagnostics) > 0 {
		return Bundle{}, diagnostics
	}
	return bundle, nil
}

// LoadLock reads and checks the Lock document at path in fsys. On any diagnostic the Lock is the zero
// value.
func LoadLock(fsys fs.FS, path string) (Lock, Diagnostics) {
	reader, diagnostics := newReader(fsys)
	if len(diagnostics) > 0 {
		return Lock{}, diagnostics
	}
	doc, diagnostics := reader.loadSingle(path, kindLock)
	if len(diagnostics) > 0 {
		return Lock{}, diagnostics
	}
	return lockFrom(doc)
}

func lockFrom(doc *document) (Lock, Diagnostics) {
	var wire wireLock
	if diagnostics := doc.decode(&wire); len(diagnostics) > 0 {
		return Lock{}, diagnostics
	}
	convert := converter{}
	lock := Lock{
		bundle:   LockedArtifact{Version: convert.version(wire.Bundle.Version), Source: wire.Bundle.Source, Digest: wire.Bundle.Digest},
		document: doc,
	}
	for _, name := range sortedKeys(wire.Modules) {
		module := wire.Modules[name]
		lock.modules = append(lock.modules, LockedModule{
			Name:           convert.moduleName(name),
			LockedArtifact: LockedArtifact{Version: convert.version(module.Version), Source: module.Source, Digest: module.Digest},
		})
	}
	if diagnostics := convert.diagnostics(doc, ""); len(diagnostics) > 0 {
		return Lock{}, diagnostics
	}
	return lock, nil
}

func manifestFrom(doc *document) (Manifest, Diagnostics) {
	var wire wireManifest
	if diagnostics := doc.decode(&wire); len(diagnostics) > 0 {
		return Manifest{}, diagnostics
	}
	convert := converter{}
	manifest := Manifest{bundleVersion: convert.version(wire.BundleVersion)}
	for _, name := range wire.Modules {
		manifest.modules = append(manifest.modules, convert.moduleName(name))
	}
	slices.SortFunc(manifest.modules, func(a, b ModuleName) int { return compareText(a.value, b.value) })
	if diagnostics := convert.diagnostics(doc, ""); len(diagnostics) > 0 {
		return Manifest{}, diagnostics
	}
	return manifest, nil
}

// CheckLock reports where lock leaves the bundle's tested set: a bundle version other than the
// bundle's is compatibility.bundle_version_mismatch, and a module the bundle does not list at the
// locked version is compatibility.module_not_in_bundle.
func CheckLock(lock Lock, bundle Bundle) Diagnostics {
	var diagnostics Diagnostics
	if lock.bundle.Version.Compare(bundle.version) != 0 {
		diagnostics = append(diagnostics, newDiagnostic(CompatibilityBundleVersionMismatch, lock.at("/bundle/version"),
			fmt.Sprintf("the Lock pins bundle %s but the bundle is version %s", lock.bundle.Version, bundle.version)))
	}
	for _, locked := range lock.modules {
		tested := slices.ContainsFunc(bundle.modules, func(module BundleModule) bool {
			return module.Name == locked.Name && module.Version.Compare(locked.Version) == 0
		})
		if !tested {
			diagnostics = append(diagnostics, newDiagnostic(CompatibilityModuleNotInBundle,
				lock.at(joinPointer("/modules", locked.Name.value)),
				fmt.Sprintf("module %q %s is not in bundle %s's tested set", locked.Name, locked.Version, bundle.version)))
		}
	}
	return diagnostics.Sorted()
}

func (l Lock) at(pointer string) Location {
	if l.document == nil {
		return NewLocation("", pointer, 0, 0)
	}
	return l.document.at(pointer)
}
