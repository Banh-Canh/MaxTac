{
  pkgs,
  lib,
}:
let
  inherit (lib) cleanSource cleanSourceWith;
in
pkgs.buildGoModule {
  pname = "maxtac";
  version = "nix";

  src = cleanSourceWith {
    filter =
      name: _:
      !(
        (baseNameOf name) == "Dockerfile"
        || (baseNameOf name) == "Makefile"
        || (baseNameOf name) == "README.md"
        || (baseNameOf name) == "PROJECT"
        || (baseNameOf name) == "config"
        || (baseNameOf name) == "conf"
        || (baseNameOf name) == "nix"
      );
    src = cleanSource ../.;
  };

  CGO_ENABLED = 0;

  subPackages = [ "cmd" ];

  vendorHash = "sha256-tKf1DkA4RAfmQA+4SSPi80BCV5bdFZCWbXuBzU4Ogdk=";

  postInstall = "mv $out/bin/cmd $out/bin/$pname";

  doCheck = true;

  meta = with lib; {
    description = "$pname; version: $version";
    homepage = "http://github.com/banh-canh/$pname";
    license = licenses.asl20;
    platforms = platforms.linux;
    mainProgram = "$pname";
  };
}
