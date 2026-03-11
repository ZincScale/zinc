# typed: false
# frozen_string_literal: true

# Homebrew formula for Zinc — a modern language that transpiles to Go.
# This formula is designed for use with a Homebrew tap (victorybhg/tap).
# goreleaser will update the url, sha256, and version fields on each release.

class Zinc < Formula
  desc "A modern programming language that transpiles to Go"
  homepage "https://github.com/victorybhg/zinc"
  version "0.4.0"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_darwin_arm64.tar.gz"
      sha256 "TODO_DARWIN_ARM64_SHA256" # TODO: replace with actual sha256 after release
    end
    if Hardware::CPU.intel?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_darwin_amd64.tar.gz"
      sha256 "TODO_DARWIN_AMD64_SHA256" # TODO: replace with actual sha256 after release
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_linux_arm64.tar.gz"
      sha256 "TODO_LINUX_ARM64_SHA256" # TODO: replace with actual sha256 after release
    end
    if Hardware::CPU.intel?
      url "https://github.com/victorybhg/zinc/releases/download/v#{version}/zinc_#{version}_linux_amd64.tar.gz"
      sha256 "TODO_LINUX_AMD64_SHA256" # TODO: replace with actual sha256 after release
    end
  end

  def install
    bin.install "zinc"
  end

  test do
    assert_match "zinc version", shell_output("#{bin}/zinc --version")
  end
end
