//go:build indexbuild

package events

// emitRuntime is the index tools' half of the split described in
// runtime_wails.go: under the indexbuild tag there is no Wails
// application to emit through, and importing the one that would be
// there costs cgo and a GTK/WebKit toolchain the index build container
// deliberately does not have.
//
// Returning ErrNoRuntime is the same answer the app gives before Run
// and after shutdown, so Emit's callers need no second code path: an
// event emitted by cmd/indexbuild is logged and dropped.  A test that
// wants to observe one installs a Sink with WithSink, which Deliver
// consults first and which works under either tag.
func emitRuntime(_ string, _ ...any) error {
	return ErrNoRuntime
}
