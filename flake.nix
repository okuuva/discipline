{
  description = "discipline, local CD ripping and reviewed inbox delivery";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/674a2028661e550a97400c1cc992b9cca9a668a1";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-linux" "x86_64-linux" ];
      eachSystem = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: import nixpkgs { inherit system; };
    in
    {
      packages = eachSystem (system:
        let pkgs = pkgsFor system;
        in rec {
          discipline = pkgs.callPackage ./nix/package.nix { };
          default = discipline;
        });

      checks = eachSystem (system: {
        package = self.packages.${system}.discipline;
      });

      devShells = eachSystem (system:
        let pkgs = pkgsFor system;
        in {
          default = pkgs.mkShell {
            inputsFrom = [ self.packages.${system}.discipline ];
            packages = [ pkgs.go pkgs.gopls ];
          };
        });
    };
}
