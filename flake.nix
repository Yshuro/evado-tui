{
  description = "Build Evado from source";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      version = nixpkgs.lib.removeSuffix "\n" (builtins.readFile ./VERSION);
    in {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          evado = pkgs.callPackage ./nix/source.nix { inherit version; };
        in { inherit evado; default = evado; });
      apps = forAllSystems (system: {
        default = {
          type = "app";
          meta.description = "Run Evado";
          program = "${self.packages.${system}.evado}/bin/evado";
        };
      });
    };
}
