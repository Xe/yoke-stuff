$`GOOS=wasip1 GOARCH=wasm go build -o valkey.wasm ./v1/flight`;
$`GOOS=wasip1 GOARCH=wasm go build -o valkey-airway.wasm ./v1/airway`;

$`yoke stow ./valkey.wasm oci://registry.int.xeserv.us/x-app/db/valkey/flight:v1`;
$`yoke stow ./valkey-airway.wasm oci://registry.int.xeserv.us/x-app/db/valkey/airway:v1`;

$`yoke takeoff valkeyairway oci://reg.xeiaso.net/x-app/db/valkey/airway:v1 -- --flight-url=oci://reg.xeiaso.net/x-app/db/valkey/flight:v1`;