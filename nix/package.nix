{ lib, buildGoModule }:

buildGoModule {
  pname = "discipline";
  version = "0.1.0";
  src = lib.cleanSource ../.;
  vendorHash = "sha256-Swi56SaPh4AN7LZ2a+j3p/jNf/InnbmE6AEErjqLg0g=";

  env.CGO_ENABLED = "1";
  subPackages = [ "cmd/discipline" ];

  checkPhase = ''
    runHook preCheck
    test -z "$(gofmt -l cmd internal)"
    go vet ./...
    go test -race ./...
    runHook postCheck
  '';

  meta = {
    description = "CD ripping with durable review and inbox delivery";
    homepage = "https://github.com/okuuva/discipline";
    mainProgram = "discipline";
    platforms = lib.platforms.linux;
  };
}
