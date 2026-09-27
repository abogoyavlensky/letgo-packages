module github.com/abogoyavlensky/letgo-packages/livekit/shim

go 1.26

require (
	github.com/livekit/livekit-server v1.13.7
	github.com/livekit/protocol v1.51.1-0.20260905133529-a4f4b5c0c23f
	github.com/nooga/let-go v0.0.0
)

// Copied from livekit-server v1.13.7's go.mod: it does not compile without
// these forks. Go ignores a dependency's replace directives, so they are
// inert for anyone requiring this module; livekit/lgx.edn carries the same
// three as :go/replace, which is what puts them into the runtime's go.mod.
// Bump both together whenever the livekit-server pin moves.
replace github.com/pion/webrtc/v4 => github.com/livekit/webrtc-pion/v4 v4.2.18-warp.1

replace github.com/pion/dtls/v3 => github.com/livekit/dtls/v3 v3.1.5-warp.1

replace github.com/pion/ice/v4 => github.com/livekit/ice/v4 v4.4.0-warp.2
