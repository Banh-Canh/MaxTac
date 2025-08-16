{
  pkgs ? import <nixpkgs> { },
  imageName ? "maxtac",
  imageTag ? "v0.0.0",
  src ? ../config,
}:
let
  runCommand =
    pkgs.runCommand "manifests"
      {
        name = "kustomize";
        nativeBuildInputs = [ pkgs.kustomize ];
        src = pkgs.lib.cleanSource src;
      }
      ''
        set -e # Exit immediately if a command exits with a non-zero status.

        echo "--> Copying source to ./config"
        cp -r ${src} ./config
        chmod -R u+w ./config # Ensure all files are writable

        echo "--> Setting image to ${imageName}:${imageTag}"
        pushd ./config/manager
        kustomize edit set image controller=${imageName}:${imageTag}
        popd

        echo "--> Building kustomize output into $out/bundle.yaml"
        mkdir -p $out # Ensure the output directory exists
        kustomize build ./config/default > $out/bundle.yaml

        echo "--> Build complete."
      '';
in
runCommand
