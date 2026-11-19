// Package desktop is the typed MiniMax DesktopService client.
//
// Calls travel through an app-server desktopService/transport tunnel. Method
// names, paths, and request bindings are generated from the @minimax-ai/code
// 0.7.8 IDL. Response objects use IDL jsonName fields. A field whose schema
// kind is scalar without a TypeScript type is a Scalar: the JSON literal is
// preserved because the primitive was not declared.
//
// RawCall is only the forward-compatible escape hatch for methods that are
// not in this baseline. It does not replace MethodCatalog.
//
// Drive paths under /minimax-desktop/api/v1/drive are rewritten by the
// app-server dispatcher to the website gateway before the IDL registrar runs.
// Those responses keep the original body in GatewayRaw.
package desktop
