# typed: false
# frozen_string_literal: true

# Homebrew formula for Zinc — convention over configuration for native apps.
# This formula is designed for use with a Homebrew tap (victorybhg/tap).
# goreleaser will update the url, sha256, and version fields on each release.

class Zinc < Formula
  desc "Convention over configuration language that compiles to native binaries via C# AOT or Go"
  homepage "https://github.com/victorybhg/zinc"
  version "0.5.0"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_darwin_arm64.tar.gz"
      sha256 "TODO_DARWIN_ARM64_SHA256" # updated by goreleaser
    end
    if Hardware::CPU.intel?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_darwin_amd64.tar.gz"
      sha256 "TODO_DARWIN_AMD64_SHA256" # updated by goreleaser
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_linux_arm64.tar.gz"
      sha256 "TODO_LINUX_ARM64_SHA256" # updated by goreleaser
    end
    if Hardware::CPU.intel?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_linux_amd64.tar.gz"
      sha256 "TODO_LINUX_AMD64_SHA256" # updated by goreleaser
    end
  end

  depends_on "dotnet@10" => :recommended

  def install
    bin.install "zinc"
  end

  def caveats
    <<~EOS
      Zinc compiles to native binaries via C# AOT (default) or Go.

      For C# AOT builds (default):
        Install .NET 10+ SDK: https://dotnet.microsoft.com/download

      For Go builds (--target go):
        Install Go 1.26+: https://go.dev/dl/

      Quick start:
        mkdir myapp && cd myapp
        zinc init myapp
        zinc build
    EOS
  end

  test do
    assert_match "zinc version", shell_output("#{bin}/zinc --version")
  end
end
