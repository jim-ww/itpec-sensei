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
          version = "1.0";
          src = pkgs.lib.cleanSource ./.;
          vendorHash = "sha256-/j4b/XP0qz8qOoI7fWhEcdyqFVmGq0ffS7pQJONyT9Y=";

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
