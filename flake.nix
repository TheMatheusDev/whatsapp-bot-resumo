{
  description = "Devshell for whatsapp-bot-resumo (WhatsApp summarizer bot in Go)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }: let
    systems = [ "x86_64-linux" "aarch64-linux" ];
    forAllSystems = nixpkgs.lib.genAttrs systems;
  in {
    devShells = forAllSystems (system: let
      pkgs = import nixpkgs { inherit system; };
    in {
      default = pkgs.mkShell {
        packages = [
          # Go 1.25.8 - matches go.mod
          pkgs.go_1_25
          # C toolchain for CGO (github.com/mattn/go-sqlite3)
          pkgs.gcc
          pkgs.pkg-config
          # Clang for Android ARM64 cross-compile (optional)
          pkgs.clang
          # SQLite CLI for inspecting work.db
          pkgs.sqlite
          # Misc
          pkgs.git
        ];

        shellHook = ''
          echo "🤖 whatsapp-bot-resumo devshell"
          go version
        '';
      };
    });
  };
}
