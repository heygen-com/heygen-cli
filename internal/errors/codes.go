package errors

// CLI error-code namespace governance.
//
// A code is "CLI-originated" when the CLI synthesizes it itself, as opposed to
// relaying a code from the API response envelope. Every CLI-originated code must
// be registered in exactly one of the three sets below. New CLI-originated codes
// MUST carry the reserved cli_ prefix (cliPrefixedCodes); the two bare sets are
// CLOSED allowlists (grandfathered legacy + API-semantic) that must not grow.
//
// The cli_ prefix is reserved so a CLI code can never collide with a future API
// code, provided the API never mints a cli_-prefixed code (enforced API-side).
// codes_test.go scans the source and fails on any CLI-minted code that is either
// unregistered or bare-but-not-allowlisted, which forces the prefix on new codes.

// CLICodePrefix is reserved for CLI-originated error codes. The API must never
// emit a code beginning with this prefix.
const CLICodePrefix = "cli_"

// cliPrefixedCodes are CLI-originated codes carrying the reserved prefix.
var cliPrefixedCodes = []string{
	"cli_response_encode_error",
	"cli_response_parse_error",
	"cli_download_url_expired",
	"cli_download_failed",
	"cli_download_interrupted",
	"cli_file_io_error",
}

// grandfatheredBareCodes are CLI-originated codes that shipped bare in a stable
// release before the cli_ convention. FROZEN: do not add. Their contract is the
// exit code (buckets) or they are CLI-only names the API has no reason to mint.
var grandfatheredBareCodes = []string{
	"error", "usage_error", "auth_error", "timeout", "canceled",
	"network_error", "confirmation_required", "file_exists",
	"batch_not_supported", "wrong_install_method",
	"video_failed", "video_not_ready",
}

// bareAPISemanticCodes are bare codes that carry API semantics: CLI mirrors of
// API codes (same name, same meaning, so a same-name overlap is correct, not a
// clash) and CLI fallback labels for API faults. Not reverse-collision risk;
// unclassified_* is an accepted risk (the API will not mint an "unclassified"
// code). asset_not_available is dual-use: an API code the CLI also mints.
var bareAPISemanticCodes = []string{
	"not_found", "insufficient_credit", "forbidden", "unauthorized",
	"conflict", "rate_limit_exceeded", "validation_error", "payload_too_large",
	"asset_not_available",
	"unclassified_server_error", "unclassified_client_error",
}
