# Protocols

Elephant services speak two RPC protocols over the same service definitions:
Connect and Twirp. A service that serves both mounts them on paths that never
overlap, so a caller picks one by the URL it posts to and nothing else. The
protocol toggle in the site header decides which one the endpoints and
examples on the API pages are written for.

Both protocols are generated from the same `.proto` files, so the request and
response messages are the same messages with the same fields. What differs is
the path, the content type, how the JSON spells a field name, the shape of an
error body and, for three codes, the HTTP status.

## Paths and content types

| | Connect | Twirp |
|---|---|---|
| Path | `POST /<package>.<Service>/<Method>` | `POST /twirp/<package>.<Service>/<Method>` |
| JSON | `application/json` | `application/json` |
| Protobuf | `application/proto` | `application/protobuf` |
| Authorization | `Authorization: Bearer <token>` | `Authorization: Bearer <token>` |

The Connect paths are mounted at the server root with no prefix, which is what
every Connect client and proxy assumes and what the generated `Procedure`
constants say. An ingress rule that routes on `/twirp/` needs a sibling rule
for the unprefixed paths.

The two protocols answer a `Content-Type` the server does not recognise
differently. Connect answers a bare `415 Unsupported Media Type` with no body at
all, naming the types it accepts in an `Accept-Post` header; there is no error
code to read, so a client that always parses a Connect error body has to handle
an empty one. Twirp answers `404` with an ordinary Twirp error body, code
`bad_route`, and the method and path in `meta.twirp_invalid_route`.

## JSON field names

**The two protocols spell field names differently in responses.** Connect
encodes with the protobuf JSON mapping and its defaults, so a field declared
`document_uuid` comes back as `documentUuid`. Twirp encodes with the names from
the `.proto` file, so the same field comes back as `document_uuid`. Everything
else about the encoding is the same on both: 64 bit integers are strings,
`bytes` is base64, and fields left at their default value are omitted from the
response.

Requests are accepted either way on both protocols, so a body written for one
is a valid body for the other. The generated request bodies on the method pages
use the `lowerCamelCase` spelling.

The generated clients — Go, `@protobuf-ts`, connect-es — decode into the
message type and are unaffected. Code that reads a raw `fetch` or `curl`
response by field name is the code that has to change when it moves from
`/twirp/` to the Connect path.

### Optional Connect headers

Connect clients send two headers that the servers do not require:

- `Connect-Protocol-Version` identifies the protocol version. A plain `curl` or
  `fetch` with `Content-Type: application/json` works without it, but when it is
  sent its value has to be exactly `1`; anything else is rejected.
- `Connect-Timeout-Ms` sets a deadline, which becomes the handler's context
  deadline.

## Errors

The error codes are the same 16 on both stacks, spelled identically
(`not_found`, `invalid_argument`, `failed_precondition`, and so on). Read the
code from the body rather than keying on the HTTP status.

Twirp puts the message and the metadata in the body:

```json
{
  "code": "not_found",
  "msg": "no such document",
  "meta": {"uuid": "abc..."}
}
```

Connect has no free-form metadata map, so the metadata travels as a typed
error detail, `elephantine.rpc.ErrorMeta`:

```json
{
  "code": "not_found",
  "message": "no such document",
  "details": [
    {
      "type": "elephantine.rpc.ErrorMeta",
      "value": "<base64, unpadded raw std>",
      "debug": {"meta": {"uuid": "abc..."}}
    }
  ]
}
```

The `type` is the message's full name with the `type.googleapis.com/` prefix
stripped. `debug` is a best effort rendering that the Connect runtime emits
when it can resolve the message, which makes the metadata readable from a raw
`fetch` or `curl` — but the authoritative decode is the base64 `value`, so a
client that depends on the metadata should decode that. The metadata itself is
byte for byte the same on both stacks, and a Twirp caller sees it flattened
back into Twirp's `meta` map.

### HTTP status differences

The HTTP status for a code is identical on both stacks except for three:

| Code | Connect | Twirp |
|---|---|---|
| `failed_precondition` | 400 | 412 |
| `canceled` | 499 | 408 |
| `deadline_exceeded` | 504 | 408 |

`failed_precondition` is the one that matters in practice: document locks,
system locks and workflow rule violations all return it, so a client that
treats 412 as "lock conflict" and everything else as a hard failure will
misread a Connect response. Key on the code in the body.

## Which protocol does a service serve?

Connect is served from the version of the API declarations that the deployed
service was built against, and the version pages say which versions those are.
Each API page carries a deployed versions table, listing the version each
tenant runs in production and the protocols that version serves. Staging runs
the latest version.

Tenants move at their own pace, so a version that is dual-stack in the
declarations is not dual-stack everywhere. A method page writes a Connect
example only for a tenant whose deployed version actually serves Connect, and
says so in one line for the tenants that do not. An API served by more than one
deployment — a few services are split that way — gets a row per deployment, and
a method's example uses the host that answers for its service.

An API that has not been migrated yet serves Twirp only, and its pages say so
by showing no protocol choice.
