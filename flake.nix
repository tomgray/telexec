{
  description = "telexec: Execution engine for remote and local commands";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-26.05";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = fn: nixpkgs.lib.genAttrs systems (system: fn nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs:
        let
          dateVersion = builtins.substring 0 8 self.lastModifiedDate;
          version = if (self ? rev) then self.rev else "${dateVersion}+dirty";
        in
        {
          default = pkgs.buildGo127Module {
            pname = "telexec";
            inherit version;
            src = ./.;
            vendorHash = "sha256-57m2Dlnx31G3BAoFd7+J59KL6dqn5vjq+Y2HdNiv/7M=";
          };

          mock = pkgs.writeShellScriptBin "mock" "${pkgs.mockgen}/bin/mockgen -source api.go -package telexec -destination mock.go";

          test = pkgs.writeShellScriptBin "test" ''
            exec ${pkgs.go_1_27}/bin/go test -v ./... "$@"
          '';
        });

      checks = forAllSystems (pkgs: {
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      });

      apps = forAllSystems (pkgs: {
        mock = {
          type = "app";
          program = "${self.packages.${pkgs.stdenv.hostPlatform.system}.mock}/bin/mock";
        };
        test = {
          type = "app";
          program = "${self.packages.${pkgs.stdenv.hostPlatform.system}.test}/bin/test";
        };
      });

      formatter = forAllSystems (pkgs:
        pkgs.writeShellScriptBin "formatter" ''
          ${pkgs.nixpkgs-fmt}/bin/nixpkgs-fmt .
          ${pkgs.go_1_27}/bin/gofmt -w .
        '');

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go_1_27
            pkgs.mockgen
            pkgs.nixpkgs-fmt
          ];
        };
      });
    };
}
