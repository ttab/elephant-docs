# Protocols

Elephant services speak two RPC protocols over the same service definitions:
Connect and Twirp. A service that serves both mounts them on paths that never
overlap, so a caller picks one by the URL it posts to and nothing else. The
protocol toggle in the site header decides which one the endpoints and
examples on the API pages are written for.

Both protocols are generated from the same `.proto` files, so the request and
response messages, the field names and the JSON encoding are identical. What
differs is the path, the content type, the shape of an error body and, for
three codes, the HTTP status.

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

JSON fields use the protobuf JSON mapping on both stacks: `lowerCamelCase`
names, 64 bit integers as strings, `bytes` as base64. Fields at their default
value are omitted from responses.

### Optional Connect headers

Connect clients send two headers that the servers do not require:

- `Connect-Protocol-Version: 1` identifies the protocol version. A plain
  `curl` or `fetch` with `Content-Type: application/json` works without it.
- `Connect-Timeout-Ms` sets a deadline, which becomes the handler's context
  deadline.

### gRPC and gRPC-Web

The Connect mount also serves gRPC and gRPC-Web on the same paths, selected by
the request's content type. Nothing in the fleet calls a service that way yet,
and the API pages document Connect and Twirp only, but a gRPC client generated
from the same `.proto` file will reach a service that has the Connect mount. An
ingress in front of the service has to allow HTTP/2 to the backend before an
external gRPC caller can get through.

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

An API that has not been migrated yet serves Twirp only, and its pages say so
by showing no protocol choice.
