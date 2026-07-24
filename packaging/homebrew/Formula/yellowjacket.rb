# typed: false
# frozen_string_literal: true

# YellowJacket — cross-platform desktop music player built with Wails (Go + Lit).
#
# This formula builds from source. The Wails toolchain (`go tool wails`) resolves
# from the tool directives in go.mod, and Wails drives the frontend install/build
# itself (pnpm), so only the Go toolchain, Node, and pnpm are needed at build time.
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
  # WebKitGTK/GTK stack, which must be present (webkit2gtk-4.1, gtk3, alsa-lib).
  on_linux do
    depends_on "pkg-config" => :build
  end

  def install
    ENV["CGO_ENABLED"] = "1"
    # Keep Go resolving modules from the network into its sandboxed cache.
    ENV["GOFLAGS"] = "-mod=mod"

    commit = build.head? ? "HEAD" : "v#{version}"
    ldflags = "-s -w -X 'main.version=v#{version}' -X 'main.commit=#{commit}'"

    system "go", "generate", "./..."
    system "go", "tool", "wails", "build",
           "-tags", "webkit2_41",
           "-clean", "-trimpath",
           "-ldflags", ldflags

    # Wails emits a .app bundle on macOS and a bare ELF binary on Linux.
    if OS.mac?
      prefix.install "build/bin/YellowJacket.app"
      bin.write_exec_script "#{prefix}/YellowJacket.app/Contents/MacOS/YellowJacket"
    else
      bin.install "build/bin/yellowjacket"
    end
  end

  test do
    # The GUI binary has no headless mode; assert it was built and is runnable.
    if OS.mac?
      assert_predicate prefix/"YellowJacket.app/Contents/MacOS/YellowJacket", :executable?
    else
      assert_predicate bin/"yellowjacket", :executable?
    end
  end
end
