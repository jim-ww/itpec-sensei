{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs = inputs @ {
    nixpkgs,
    flake-parts,
    flake-utils,
    ...
  }:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = flake-utils.lib.defaultSystems;
      perSystem = {pkgs, ...}: {
        packages.default = pkgs.buildGoModule {
          pname = "itpec-sensei";
          version = "0.4.0";
          src = pkgs.lib.cleanSource ./.;
          vendorHash = "sha256-pFWc1rUGPL0KQyeshK79B7CkuzpbSRhHzt854JTfRb4=";

          nativeBuildInputs = [pkgs.installShellFiles];

          postInstall = ''
            installShellCompletion --cmd itpec-sensei \
              --bash <($out/bin/itpec-sensei completion bash) \
              --zsh <($out/bin/itpec-sensei completion zsh) \
              --fish <($out/bin/itpec-sensei completion fish)
          '';
        };
      };
    };
}
