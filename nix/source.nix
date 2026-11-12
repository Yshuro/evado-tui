{ lib, buildGoModule, version ? "dev" }:

buildGoModule {
  pname = "evado";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [ ../go.mod ../go.sum ../cmd ../internal ];
  };
  vendorHash = "sha256-PYwqD0iw0NEeza/v6f6+cLc/ddzG2qhvyQe+huDLoAo=";
  subPackages = [ "cmd/evado" ];
  env.CGO_ENABLED = "0";
  ldflags = [ "-s" "-w" "-X main.version=${version}" ];
  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';

  meta = {
    description = "Evado terminal todo client";
    homepage = "https://github.com/nerdlabz/evado-tui";
    license = lib.licenses.mit;
    mainProgram = "evado";
    platforms = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
  };
}
