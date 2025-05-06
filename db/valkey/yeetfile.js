$`GOOS=wasip1 GOARCH=wasm go build -o valkey.wasm ./v1/flight`;
$`GOOS=wasip1 GOARCH=wasm go build -o valkey-airway.wasm ./v1/airway`;

$`yoke stow ./valkey.wasm oci://registry.int.xeserv.us/x-app/db/valkey/flight:${git.tag()}`;
$`yoke stow ./valkey-airway.wasm oci://registry.int.xeserv.us/x-app/db/valkey/airway:${git.tag()}`;

$`gzip -f9 *.wasm`;

file.install("valkey.wasm.gz", `../../var/valkey-${git.tag()}.wasm.gz`);
file.install(
  "valkey-airway.wasm.gz",
  `../../var/valkey-airway-${git.tag()}.wasm.gz`,
);
