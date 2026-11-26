package testenv

// Fixed, nonsecret test vector. The all-zero source claims are deliberately
// synthetic, not a reviewed revision. The expected manifest digest is pinned
// independently of the received stream; only transport integrity is tested.
const syntheticGuestPayload = `{"schema_version":1,"source_revision":"0000000000000000000000000000000000000000","source_dirty":false,"flake_lock_sha256":"0000000000000000000000000000000000000000000000000000000000000000","segments":[{"sha256":"9ae815b7ff1c6a1d2be4fb9e2d3dd0fdc4daccde05a6d1ae3d014d3f9e596cef","bytes":27},{"sha256":"48687dbf20d42188423de63503415b955b1d3e9c411fe51645a947a13fa9d0d0","bytes":30}]}
Aginctus synthetic payload
no credentials or source data
`
const syntheticGuestPayloadManifest = "5db8687ff89336443c9ffdcc287736b9a8b3d3612d4af49c0701b4edc9f7bdab"
