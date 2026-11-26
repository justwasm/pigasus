// Package codec owns pure conversions between the proxy's OpenAI/Anthropic
// request and response shapes and the CloudCode turn model. Media effects are
// supplied explicitly through Media; HTTP, credentials, retry policy, and stream
// I/O and lifecycle remain in agyproxy.
package codec
