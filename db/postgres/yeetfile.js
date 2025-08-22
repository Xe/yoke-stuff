$`GOOS=wasip1 GOARCH=wasm go build -o postgres.wasm ./v1/flight`;
$`GOOS=wasip1 GOARCH=wasm go build -o postgres-airway.wasm ./v1/airway`;

$`yoke stow ./postgres.wasm oci://registry.int.xeserv.us/x-app/db/postgres/flight:${git.tag()}`;
$`yoke stow ./postgres-airway.wasm oci://registry.int.xeserv.us/x-app/db/postgres/airway:${git.tag()}`;

$`gzip -f9 *.wasm`;

file.install("postgres.wasm.gz", `../../var/postgres-${git.tag()}.wasm.gz`);
file.install(
  "postgres-airway.wasm.gz",
  `../../var/postgres-airway-${git.tag()}.wasm.gz`,
);
