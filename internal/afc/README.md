# AFC transport

`afc.go`, `client.go` and `errors.go` are adapted from the MIT-licensed
go-ios v1.3.2 `ios/afc` package:
https://github.com/danielpaulus/go-ios/tree/v1.3.2/ios/afc

The adapter accepts an existing USB service connection, uses the standard
library's slices package, validates response magic/sequence/lengths and file
handles, and checks complete writes. The upstream license is in `LICENSE` and
is copied beside the distributed executable by `scripts/build.ps1`.

The PocketFiles directory/transfer implementation is in `internal/phone`.
Apple USBMux pairing and Lockdown service setup use the application's existing
transport and plist dependency, so no separate iOS helper executable is needed.
