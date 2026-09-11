package handler

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
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
// a scheme from a sentence that happens to contain a full stop, and what refuses
// "javascript:alert(1)", which url.Parse reads as having a scheme.
var hostAndPort = regexp.MustCompile(
	`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?` +
		`(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*` +
		`\.[A-Za-z]{2,}` +
		`|(?:\d{1,3}\.){3}\d{1,3})(?::\d{1,5})?$`)

// blank reports whether r is whitespace or an invisible the clients treat as
// one. crimpy-app and crimpy-frontend both test with \s, which covers a
// non-breaking space; a value they refuse to render must not be one this
// accepts, or the same stored link works on one surface and is inert on the
// other with nothing said anywhere.
func blank(r rune) bool {
	return unicode.IsSpace(r) ||
		r == '\ufeff' || // byte order mark
		r == '\u200b' || // zero width space
		r == '\u2060' // word joiner
}

// addressable reports whether s is a url a client can actually be handed. It is
// what stops a value this function accepts from being one the database or a
// browser then refuses: url.Parse rejects control bytes, and a port outside the
// range throws in the URL constructors the clients use.
func addressable(s string) bool {
	parsed, err := url.Parse(s)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false
		}
	}
	return true
}

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
	if strings.IndexFunc(trimmed, blank) >= 0 {
		return "", errInvalidVideoLink
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if !addressable(trimmed) {
			return "", errInvalidVideoLink
		}
		return trimmed, nil
	}

	// Anything else is only an address if what stands where the host would is
	// host shaped. Tested before the scheme is added and checked again after, so
	// what comes back is both an address and storable.
	authority := strings.SplitN(trimmed, "/", 2)[0]
	authority = strings.SplitN(authority, "?", 2)[0]
	authority = strings.SplitN(authority, "#", 2)[0]
	if !hostAndPort.MatchString(authority) {
		return "", errInvalidVideoLink
	}
	candidate := "https://" + trimmed
	if !addressable(candidate) {
		return "", errInvalidVideoLink
	}
	return candidate, nil
}
