# typed: false
# frozen_string_literal: true

# YellowJacket — cross-platform desktop music player built with Wails (Go + Lit).
#
# This formula builds from source. The Wails toolchain (`go tool wails3`)
# resolves from the tool directives in go.mod, and Wails drives the frontend
# install/build itself (pnpm), so only the Go toolchain, Node, and pnpm are
# needed at build time.
#
# This file is the canonical source. On each tagged release, CI computes the
# tarball checksum and syncs an updated copy into the homebrew-yellowjacket tap
# repo (see .gitea/workflows/homebrew-formula.yml). The `version`/`sha256` lines
# below are what CI rewrites — keep them on their own lines.
class Yellowjacket < Formula
  desc "Cross-platform desktop music player — local library, MusicBrainz explore & auto-tag"
  homepage "https://git.ljones.me/yonlu/yellowjacket"
  version "1.3.0"
  url "https://git.ljones.me/yonlu/yellowjacket/archive/v#{version}.tar.gz"
  sha256 "11929d9a7a32839f86213502b698a02376b38f0838fa20610408f062423899e5"
  license :cannot_represent # custom license — see repository

  head "https://git.ljones.me/yonlu/yellowjacket.git", branch: "main"

  depends_on "go" => :build
  depends_on "node" => :build
  depends_on "pnpm" => :build

  # Wails targets macOS and Linux. On Linux, Homebrew builds against the system
  # WebKitGTK/GTK stack, which must be present. Wails v3 resolves GTK4 +
  # WebKitGTK 6.0 by default (webkitgtk-6.0, gtk4, alsa-lib); v2's
  # webkit2gtk-4.1 + gtk3 is now only an opt-in `-tags gtk3` escape hatch and is
  # not what this formula builds.
  on_linux do
    depends_on "pkg-config" => :build
  end

  def install
    ENV["CGO_ENABLED"] = "1"
    # Keep Go resolving modules from the network into its sandboxed cache.
    ENV["GOFLAGS"] = "-mod=mod"

    commit = build.head? ? "HEAD" : "v#{version}"

    # v3's build is a Taskfile tree whose tasks invoke `wails3` by bare name, so
    # the vendored tool has to be on PATH under that name — scripts/toolbin is
    # the shim the Makefile uses for the same reason. And `wails3 build` has no
    # -ldflags flag of its own (that was v2), so the version stamp goes through
    # the LDFLAGS_EXTRA task variable this repo added to the platform Taskfiles.
    # -trimpath and -w -s are already in the production task's own flags.
    ENV.prepend_path "PATH", buildpath/"scripts/toolbin"
    ldflags_extra = "-X 'main.version=v#{version}' -X 'main.commit=#{commit}'"

    system "go", "generate", "./..."

    # `task build` produces a bare binary in bin/ on both platforms; the .app
    # bundle is `task package`, which is a separate step in v3.
    if OS.mac?
      system "go", "tool", "wails3", "task", "package",
             "LDFLAGS_EXTRA=#{ldflags_extra}"
      # The bundle is named after `productName` in build/config.yml; glob for it
      # so a rename (or casing difference) can't silently break the install.
      app = Dir["bin/*.app"].first
      odie "wails3 task package produced no .app bundle in bin/" if app.nil?
      prefix.install app
      exe = Dir[prefix/"*.app/Contents/MacOS/*"].find { |f| File.executable?(f) }
      bin.write_exec_script exe
    else
      system "go", "tool", "wails3", "task", "build",
             "LDFLAGS_EXTRA=#{ldflags_extra}"
      bin.install "bin/yellowjacket"
    end
  end

  test do
    # The GUI binary has no headless mode; assert it was built and is runnable.
    if OS.mac?
      assert_predicate Dir[prefix/"*.app/Contents/MacOS/*"].first, :executable?
    else
      assert_predicate bin/"yellowjacket", :executable?
    end
  end
end
