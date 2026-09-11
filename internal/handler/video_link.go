package handler

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// errInvalidVideoLink is answered for anything that is not an address a browser
// can be sent to. The field is free text a coach types, and until it was joined
// onto training items it only ever came back to the coach who typed it; it now
// reaches their athlete and, through a frozen session prescription, a coach who
// took that athlete over. A "javascript:" value stored here would run in
// whichever of them clicked it.
var errInvalidVideoLink = errors.New("video_link must be an http or https address")

// hostAndPort is a dotted name ending in a letters-only suffix, or a dotted
// quad, each with an optional port. It is what tells an address written without
// a scheme from a sentence that happens to contain a full stop.
var hostAndPort = regexp.MustCompile(
	`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?` +
		`(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*` +
		`\.[A-Za-z]{2,}` +
		`|(?:\d{1,3}\.){3}\d{1,3})(?::\d{1,5})?$`)

// normalizeVideoLink returns the value to store for a demo video link: the empty
// string when the coach cleared the field, the address itself when they typed
// one, and an error otherwise.
//
// A link written without a scheme is stored as https rather than refused, since
// that is what a coach types and the field they type it into does not make them
// add one. Normalizing here means every client reads one canonical form, and
// matches how crimpy-app and crimpy-frontend already read the field.
func normalizeVideoLink(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	// An address has no whitespace in it. A sentence does, and url.Parse would
	// take one rather than refuse it.
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return "", errInvalidVideoLink
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			return "", errInvalidVideoLink
		}
		return trimmed, nil
	}

	// Anything else is only an address if what stands where the host would is
	// host shaped, which is also what refuses "javascript:alert(1)": url.Parse
	// reads that as a scheme, so the scheme is not what can be tested here.
	authority := strings.SplitN(trimmed, "/", 2)[0]
	authority = strings.SplitN(authority, "?", 2)[0]
	authority = strings.SplitN(authority, "#", 2)[0]
	if !hostAndPort.MatchString(authority) {
		return "", errInvalidVideoLink
	}
	return "https://" + trimmed, nil
}
