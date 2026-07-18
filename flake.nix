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
          version = "0.2.0";
          src = pkgs.lib.cleanSource ./.;
          vendorHash = "sha256-/j4b/XP0qz8qOoI7fWhEcdyqFVmGq0ffS7pQJONyT9Y=";
        };
      };
    };
}
