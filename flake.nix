{
  description = "elport: a git-like sync CLI for eLabFTW";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = self.shortRev or "dev";
    in
    {
      packages = forAllSystems (
        pkgs:
        let
          elport = pkgs.buildGoModule {
            pname = "elport";
            inherit version;
            src = self;
            subPackages = [ "cmd/elport" ];
            vendorHash = "sha256-UQTsHNSM3lJW0674GC8j7csrI9Ap91VE/wl3QniI9LY=";
            env.CGO_ENABLED = 0;
            ldflags = [
              "-s"
              "-w"
              "-X github.com/youthesame/elport/internal/cli.Version=${version}"
            ];
            doCheck = false;
            meta = {
              description = "Git-like sync CLI for eLabFTW";
              homepage = "https://github.com/youthesame/elport";
              license = pkgs.lib.licenses.mit;
              mainProgram = "elport";
            };
          };
        in
        {
          inherit elport;
          default = elport;
        }
      );
    };
}
