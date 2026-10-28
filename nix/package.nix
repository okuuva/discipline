{ lib, buildGoModule, pkg-config, systemd, abcde, makeWrapper, writeShellScriptBin
, coreutils, bash, gnused, gnugrep, gawk, findutils, which, cddiscid, cdparanoia
, lame, rsgain, python3Packages, util-linux, inetutils, curl, imagemagick, ffmpeg-headless }:

let
  patchedAbcde = abcde.overrideAttrs (old: {
    patches = (old.patches or [ ]) ++ [
      ./abcde-musicbrainz-single-track.patch
      ./abcde-metadata-quoting.patch
    ];
  });
  musicbrainz = writeShellScriptBin "discipline-musicbrainz" ''
    # Failed lookup is missing metadata, not failed audio extraction.
    ${coreutils}/bin/timeout --foreground --kill-after=5s 30s ${patchedAbcde}/bin/abcde-musicbrainz-tool "$@"
    result=$?
    if [ "$result" -ne 0 ] && [ "''${1-}" = --command ] && [ "''${2-}" = data ]; then
      exit 0
    fi
    exit "$result"
  '';
  runtime = [ patchedAbcde musicbrainz coreutils bash gnused gnugrep gawk findutils
    which cddiscid cdparanoia lame rsgain python3Packages.eyed3 util-linux inetutils
    curl imagemagick ffmpeg-headless ];
in
buildGoModule {
  pname = "discipline";
  version = "0.1.0";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-4tWuELZbgW6PbkBj4bkyT/LI4R+EYr5+Gr7DKVZ1uMM=";
  nativeBuildInputs = [ pkg-config makeWrapper ];
  buildInputs = [ systemd ];
  nativeCheckInputs = runtime;
  env.CGO_ENABLED = "1";
  subPackages = [ "cmd/discipline" ];

  checkPhase = ''
    runHook preCheck
    export MAGICK_CONFIGURE_PATH=${../data/image-policy}
    export DISCIPLINE_TEST_ABCDE=${patchedAbcde}/bin/abcde
    export DISCIPLINE_TEST_CDDB=${patchedAbcde}/bin/cddb-tool
    export DISCIPLINE_TEST_MUSICBRAINZ=${patchedAbcde}/bin/abcde-musicbrainz-tool
    test -z "$(gofmt -l cmd internal)"
    go vet ./...
    go test -race -timeout 2m ./...
    runHook postCheck
  '';

  postInstall = ''
    wrapProgram $out/bin/discipline \
      --prefix PATH : ${lib.makeBinPath runtime} \
      --set DISCIPLINE_ABCDE_CONFIG ${../data/abcde.conf} \
      --set MAGICK_CONFIGURE_PATH ${../data/image-policy}
  '';

  meta = {
    description = "CD ripping with durable review and inbox delivery";
    homepage = "https://github.com/okuuva/discipline";
    mainProgram = "discipline";
    platforms = lib.platforms.linux;
  };
}
