{
  pkgs ? import <nixpkgs> { },
  dockerVersion ? "0.0.0",
}:
let
  binaries = pkgs.callPackage ./binaries.nix { };
  # trick to override the tag by processing the generated hash + making it dry
  makeDummyImage = {
    fakeRootCommands = "\n      ln -s var/run run\n      ln -s bin/${binaries.pname} manager\n    ";
    name = binaries.pname;
    contents = [
      binaries
      (pkgs.dockerTools.fakeNss.override {
        extraPasswdLines = [
          "nixbld:x:${toString 1001}:${toString 0}:Build user:/home/${binaries.pname}:/noshell"
        ];
        extraGroupLines = [ "nixbld:!:${toString 1001}:" ];
      })
    ];

    config = {
      User = "1001:0";
      Entrypoint = [ "/manager" ];
    };
  };
  imageDummy = pkgs.dockerTools.streamLayeredImage {
    inherit (makeDummyImage) fakeRootCommands;
    inherit (makeDummyImage) name;
    inherit (makeDummyImage) contents;
    inherit (makeDummyImage) config;
    tag = "${dockerVersion}";
  };
in
pkgs.dockerTools.streamLayeredImage {
  inherit (makeDummyImage) fakeRootCommands;
  tag = imageDummy.imageTag;
  inherit (makeDummyImage) name;
  inherit (makeDummyImage) contents;
  inherit (makeDummyImage) config;
}
