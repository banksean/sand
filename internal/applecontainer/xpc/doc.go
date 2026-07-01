// Package xpc implements a client interface for com.apple.container.apiserver,
// the background API server daemon for Apple's native open-source Linux container tool on macOS.
// (see https://github.com/apple/container).
//
// This package requires cgo and macOS specific header files in order to communicate with libxpc.
// However it also includes stub implementations for linux so we can run tests without talking to
// libxpc.
//
// Implementation notes:
//
// This package provides a single Client type, but the Swift client code uses separate Client
// types for various parts of the overall API surface. These are implemented in directories under
// https://github.com/apple/container/tree/main/Sources/Services
//
// Many of the message payloads that com.apple.container.apiserver returns or accepts are
// encoded as JSON data and transported as XPC_TYPE_DATA. This makes the job of (un)marshaling
// message types between Go and the Container API server much easier than if they were
// represented with nested XPC_TYPE_DICTIONARY and other primitive XPC_TYPEs.
//
// Another tailwind for this package is the fact that the Container API server uses libxpc
// instead of higher-level Swift or Objective-C libraries. This means we don't need to worry
// about a lot of complexity added by interfacing Go with those runtimes.
package xpc
