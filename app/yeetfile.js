$`GOOS=wasip1 GOARCH=wasm go build -o x-app.wasm ./v1/flight`;
$`GOOS=wasip1 GOARCH=wasm go build -o x-app-airway.wasm ./v1/airway`;

$`yoke stow ./x-app.wasm oci://registry.int.xeserv.us/x-app/flight:${git.tag()}`;
$`yoke stow ./x-app-airway.wasm oci://registry.int.xeserv.us/x-app/airway:${git.tag()}`;

$`gzip -f9 *.wasm`;

file.install("x-app.wasm.gz", `../var/x-app-${git.tag()}.wasm.gz`);
file.install(
  "x-app-airway.wasm.gz",
  `../var/x-app-airway-${git.tag()}.wasm.gz`,
);
