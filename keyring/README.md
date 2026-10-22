# keyring

Passwords in the system keyring, over
[go-keyring](https://github.com/zalando/go-keyring): the macOS Keychain,
the Secret Service on Linux, the Credential Manager on Windows. Pure Go,
no cgo.

```clojure
{:deps {abogoyavlensky/letgo-keyring
        {:git/url "https://github.com/abogoyavlensky/letgo-packages"
         :git/tag "keyring-v0.1.0"
         :deps/root "keyring"}}}
```

```clojure
(require '[keyring.core :as kr])

(kr/put! "MyApp" "profile-42" "s3cret")   ; => nil
(kr/get "MyApp" "profile-42")             ; => "s3cret"
(kr/delete! "MyApp" "profile-42")         ; => nil
(kr/get "MyApp" "profile-42")             ; => nil
```

## API

| Fn | Does |
|---|---|
| `(put! service user password)` | Store the password, replacing any stored one. Throws when the keyring refuses. |
| `(get service user)` | The stored password, or nil when there is none. |
| `(delete! service user)` | Remove it. A missing entry is not an error. |
| `(use-memory!)` | Swap the system keyring for an empty in-memory one. |

The service is your app's name and the user is any account name or id of
yours: together they name one entry. Passwords come back exactly as
stored, whitespace and all.

## Platforms

- **macOS**: the login Keychain, through `/usr/bin/security`. The password
  goes to it on stdin, so it never shows in `ps`. Items appear in Keychain
  Access under the service name. Items the app created read back without
  a prompt.
- **Linux**: the Secret Service over D-Bus (GNOME Keyring, KWallet). It
  needs a running secret service and an unlocked collection; a headless
  box usually has neither, so calls there throw. Use `use-memory!` in
  tests and CI.
- **Windows**: the Credential Manager. Untested: let-go does not build for
  Windows yet.

## Things to know

- **`use-memory!` is process-wide.** go-keyring keeps its provider in a
  package variable, so once called, every caller in the process uses the
  memory store until it exits. Call it at the top of a test namespace, or
  behind a flag in an app's config, never conditionally mid-run.
- **Calls are serialised.** The shim holds one lock around each call:
  go-keyring's memory store is an unlocked map, and a let-go program calls
  from many goroutines (a Wails app's bridge calls run concurrently).

## Layout

```
keyring/
├── lgx.edn               the shim as {:go/local "shim"}
├── shim/                 Set, Get, Delete, UseMemory over go-keyring
├── src/keyring/core.lg   put!, get, delete!, use-memory!
├── test/keyring/         on the memory store, concurrency included
└── example/              an asserting walkthrough on the memory store
```

## Development

```
cd keyring && lgx test
cd keyring/example && lgx run
```

The shim ships in the package tag (`keyring-vX.Y.Z`); there is no Go
module tag. See "Releasing" in the [repo README](../README.md).
