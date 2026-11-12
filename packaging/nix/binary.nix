{ lib, stdenvNoCC, fetchurl, release }:

let asset = release.assets.${stdenvNoCC.hostPlatform.system};
in stdenvNoCC.mkDerivation {
  pname = "evado";
  inherit (release) version;
  src = fetchurl { inherit (asset) url sha256; };

  sourceRoot = ".";
  dontConfigure = true;
  dontBuild = true;
  # Preserve the release executable on both macOS and Linux.
  dontStrip = true;
  dontPatchELF = true;
  dontPatchShebangs = true;
  installPhase = ''
    runHook preInstall
    install -Dm755 evado "$out/bin/evado"
    install -Dm644 THIRD_PARTY_NOTICES.txt "$out/share/doc/evado/THIRD_PARTY_NOTICES.txt"
    if [ -f LICENSE ]; then
      install -Dm644 LICENSE "$out/share/doc/evado/LICENSE"
    fi
    runHook postInstall
  '';

  meta = {
    description = "Evado terminal todo client";
    homepage = "https://github.com/nerdlabz/evado-tui";
    license = lib.licenses.mit;
    mainProgram = "evado";
    platforms = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
  };
}
