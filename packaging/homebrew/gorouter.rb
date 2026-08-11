class Gorouter < Formula
  desc "Gorouter native binary"
  homepage "https://github.com/gorouter/gorouter"
  url "https://github.com/gorouter/gorouter/releases/download/v1.2.3/gorouter-darwin-arm64"
  sha256 "sha256-published-at-release"
  version "1.2.3"
  def install
    bin.install "gorouter-darwin-arm64" => "gorouter"
  end
end
